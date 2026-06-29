package mirror

const redactedMarker = "***REDACTED***"

var sensitiveKeys = map[string]bool{
	"password":            true,
	"secret":              true,
	"apiKey":              true,
	"api_key":             true,
	"token":               true,
	"privateKey":          true,
	"private_key":         true,
	"clientSecret":        true,
	"client_secret":       true,
	"authorizationHeader": true,
	"secretAccessKey":     true,
	"access_key":          true,
	"accessKey":           true,
	"authToken":           true,
	"auth_token":          true,
	"sharedKey":           true,
	"shared_key":          true,
}

// stripSecrets recursively replaces the value of any sensitive key with a
// redaction marker so credentials are never written to disk during pull.
func stripSecrets(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKeys[k] {
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
