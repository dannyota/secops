// Package httpretry holds the shared HTTP retry/backoff and client-side pacing
// policy used by both transports — the chronicle (ADC) and SOAR (AppKey) clients
// mirror the same behavior, so the logic lives here once and is unit-tested once.
//
// It does three things:
//   - honors a server-supplied retry hint (the `Retry-After` header and/or a
//     google.rpc.RetryInfo in the JSON error body) — authoritative for a 429
//     quota wait, where a fixed local backoff is otherwise far too short;
//   - computes a jittered, capped exponential backoff when there is no hint
//     (full jitter de-syncs concurrent callers, avoiding a thundering herd);
//   - exposes a token-bucket limiter so bursty multi-call operations pace
//     themselves under the API quota instead of firing all at once.
package httpretry

import (
	"encoding/json"
	// nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- backoff jitter, not security-sensitive
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Jitter returns a uniform random value in [0,1) for backoff jitter. It uses a
// non-cryptographic PRNG on purpose — de-syncing retries needs no cryptographic
// strength — so callers pass it as the rnd argument to Policy.Backoff.
func Jitter() float64 {
	// nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- backoff jitter, not security-sensitive
	return rand.Float64() //nolint:gosec // jitter is not security-sensitive
}

// Policy parameterizes the retry backoff. The zero value is unusable; use
// Default* or construct explicitly.
type Policy struct {
	// MaxAttempts is the total number of attempts including the first.
	MaxAttempts int
	// Base is the first backoff delay; it doubles each attempt (before jitter).
	Base time.Duration
	// Max caps a single computed (no-hint) backoff.
	Max time.Duration
	// Budget caps a single honored server hint, so a pathological Retry-After
	// can't hang a command indefinitely. 0 means "use Max".
	Budget time.Duration
}

// DefaultPolicy is the shared retry policy. 429 is safe to retry on any method
// and is quota-driven, so the budget is generous enough to ride out a typical
// per-minute quota window when the server tells us how long to wait.
func DefaultPolicy() Policy {
	return Policy{MaxAttempts: 5, Base: 300 * time.Millisecond, Max: 8 * time.Second, Budget: 60 * time.Second}
}

// Backoff returns how long to wait before the given 1-based attempt (attempt 1 is
// the first retry, i.e. after the initial try failed). When hint > 0 it is the
// server's suggested delay (use it only for a 429, where it's authoritative) and
// is honored plus a little jitter, capped at Budget. Otherwise an EQUAL-jitter
// exponential backoff is used (half fixed + half random, so retries keep a minimum
// spacing rather than possibly firing back-to-back), capped at Max. rnd is a value
// in [0,1) (the caller passes Jitter()), kept a parameter so the math is
// deterministic under test.
func (p Policy) Backoff(attempt int, hint time.Duration, rnd float64) time.Duration {
	budget := p.Budget
	if budget <= 0 {
		budget = p.Max
	}
	if hint > 0 {
		w := hint + time.Duration(rnd*float64(p.Base)) // honor the server + small jitter
		return min(w, budget)
	}
	if p.Base <= 0 {
		return 0 // no local backoff configured (e.g. tests)
	}
	if attempt < 1 {
		attempt = 1
	}
	exp := p.Base << uint(attempt-1) // Base * 2^(attempt-1)
	if exp <= 0 || exp > p.Max {     // overflow or over cap
		exp = p.Max
	}
	half := exp / 2
	return half + time.Duration(rnd*float64(half)) // equal jitter: [exp/2, exp)
}

// ParseHint extracts a server-suggested retry delay from a response: the
// `Retry-After` header (delta-seconds or an HTTP-date) and/or a
// google.rpc.RetryInfo entry in the JSON error body's `error.details`. It returns
// 0 when neither is present or usable.
func ParseHint(h http.Header, body []byte) time.Duration {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			if secs > 0 {
				return time.Duration(secs) * time.Second
			}
		} else if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	if len(body) == 0 {
		return 0
	}
	var env struct {
		Error struct {
			Details []struct {
				Type       string          `json:"@type"`
				RetryDelay json.RawMessage `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil {
		return 0
	}
	for _, d := range env.Error.Details {
		if strings.Contains(d.Type, "RetryInfo") && len(d.RetryDelay) > 0 {
			if dur := parseProtoDuration(d.RetryDelay); dur > 0 {
				return dur
			}
		}
	}
	return 0
}

// parseProtoDuration decodes a protobuf Duration in its JSON forms: the canonical
// string ("37s", "1.500s") or the {"seconds":..,"nanos":..} object.
func parseProtoDuration(raw json.RawMessage) time.Duration {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			return d
		}
		// Bare seconds with a trailing 's' that ParseDuration rejects (rare) —
		// fall through to 0.
		return 0
	}
	var obj struct {
		Seconds json.Number `json:"seconds"`
		Nanos   int64       `json:"nanos"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		secs, _ := obj.Seconds.Int64()
		return time.Duration(secs)*time.Second + time.Duration(obj.Nanos)*time.Nanosecond
	}
	return 0
}

// NewLimiter builds a token-bucket limiter at perSec requests/second with the
// given burst, for callers that opt into proactive pacing (clients default to no
// limiter). A nil limiter (perSec <= 0) means "no client-side pacing".
func NewLimiter(perSec float64, burst int) *rate.Limiter {
	if perSec <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(perSec), burst)
}
