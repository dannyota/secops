package mirror

import "strings"

const redactedMarker = "***REDACTED***"

// sensitiveKeys is matched case-insensitively (lowercase entries): server
// payloads carry the same credential under Password/password/PASSWORD
// depending on the surface, and a missed casing writes a secret to disk.
var sensitiveKeys = map[string]bool{
	"password":            true,
	"secret":              true,
	"apikey":              true,
	"api_key":             true,
	"token":               true,
	"privatekey":          true,
	"private_key":         true,
	"clientsecret":        true,
	"client_secret":       true,
	"authorizationheader": true,
	"secretaccesskey":     true,
	"access_key":          true,
	"accesskey":           true,
	"authtoken":           true,
	"auth_token":          true,
	"sharedkey":           true,
	"shared_key":          true,
}

// stripSecrets recursively replaces the value of any sensitive key with a
// redaction marker so credentials are never written to disk during pull.
func stripSecrets(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKeys[strings.ToLower(k)] {
				out[k] = redactedMarker
			} else {
				out[k] = stripSecrets(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = stripSecrets(val)
		}
		return out
	default:
		return v
	}
}
