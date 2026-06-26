package httpretry

import (
	"net/http"
	"testing"
	"time"
)

func TestParseHintRetryAfterSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "37")
	if got := ParseHint(h, nil); got != 37*time.Second {
		t.Errorf("Retry-After: 37s, got %v", got)
	}
}

func TestParseHintRetryAfterDate(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", time.Now().Add(20*time.Second).UTC().Format(http.TimeFormat))
	got := ParseHint(h, nil)
	if got < 15*time.Second || got > 21*time.Second {
		t.Errorf("Retry-After date ~20s, got %v", got)
	}
}

func TestParseHintRetryInfoString(t *testing.T) {
	body := []byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[
		{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"42s"}]}}`)
	if got := ParseHint(http.Header{}, body); got != 42*time.Second {
		t.Errorf("RetryInfo string: 42s, got %v", got)
	}
}

func TestParseHintRetryInfoObject(t *testing.T) {
	body := []byte(`{"error":{"details":[
		{"@type":"google.rpc.RetryInfo","retryDelay":{"seconds":15,"nanos":500000000}}]}}`)
	if got := ParseHint(http.Header{}, body); got != 15500*time.Millisecond {
		t.Errorf("RetryInfo object: 15.5s, got %v", got)
	}
}

func TestParseHintNone(t *testing.T) {
	if got := ParseHint(http.Header{}, []byte(`{"error":{"code":500}}`)); got != 0 {
		t.Errorf("no hint should be 0, got %v", got)
	}
	// Header takes precedence over an absent body hint, and a malformed body is safe.
	if got := ParseHint(http.Header{}, []byte("not json")); got != 0 {
		t.Errorf("malformed body should be 0, got %v", got)
	}
}

func TestBackoffHonorsHintCappedAtBudget(t *testing.T) {
	p := Policy{MaxAttempts: 5, Base: 300 * time.Millisecond, Max: 8 * time.Second, Budget: 60 * time.Second}
	// A hint within budget is honored (plus <= Base jitter).
	w := p.Backoff(1, 40*time.Second, 0.5)
	if w < 40*time.Second || w > 40*time.Second+p.Base {
		t.Errorf("hint 40s honored ~40s, got %v", w)
	}
	// A hint beyond budget is clamped to budget.
	if w := p.Backoff(1, 5*time.Minute, 0.9); w != p.Budget {
		t.Errorf("oversize hint clamped to budget %v, got %v", p.Budget, w)
	}
}

func TestBackoffEqualJitterExponentialCapped(t *testing.T) {
	p := Policy{MaxAttempts: 5, Base: 300 * time.Millisecond, Max: 8 * time.Second}
	// Equal jitter: the wait floor is exp/2 (so retries keep minimum spacing —
	// never near-0), and the ceiling is exp. Attempt 1: exp=Base=300ms.
	if lo := p.Backoff(1, 0, 0); lo != 150*time.Millisecond {
		t.Errorf("attempt 1 floor should be exp/2=150ms, got %v", lo)
	}
	hi := p.Backoff(3, 0, 0.999) // exp = Base*2^2 = 1.2s; range [600ms, 1.2s)
	if hi < 600*time.Millisecond || hi > 1200*time.Millisecond {
		t.Errorf("attempt 3 range [600ms,1.2s), got %v", hi)
	}
	// A high attempt is capped at Max, never unbounded.
	if w := p.Backoff(20, 0, 1.0); w > p.Max {
		t.Errorf("backoff must cap at Max %v, got %v", p.Max, w)
	}
}

func TestBackoffZeroBaseIsInstant(t *testing.T) {
	// Tests zero the base for instant retries; no-hint backoff must be 0, not the cap.
	p := Policy{MaxAttempts: 5, Base: 0, Max: 8 * time.Second, Budget: 60 * time.Second}
	if w := p.Backoff(3, 0, 0.99); w != 0 {
		t.Errorf("zero-base no-hint backoff should be 0, got %v", w)
	}
	// A server hint is still honored even with a zero base.
	if w := p.Backoff(1, 5*time.Second, 0); w != 5*time.Second {
		t.Errorf("zero-base should still honor a 5s hint, got %v", w)
	}
}

func TestNewLimiter(t *testing.T) {
	if NewLimiter(0, 5) != nil {
		t.Error("perSec<=0 should give a nil limiter (no pacing)")
	}
	if l := NewLimiter(10, 20); l == nil || l.Burst() != 20 {
		t.Errorf("limiter should have burst 20, got %v", l)
	}
}
