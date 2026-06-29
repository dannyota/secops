package mirror

// redactedMarker replaces any sensitive value before it is written to disk.
const redactedMarker = "***REDACTED***"

// sensitiveKeys are scalar field names that may carry credentials on feeds and
// similar entities. Their values are redacted before anything touches disk so a
// pulled snapshot is safe to commit.
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

// redact recursively replaces the value of any sensitive key with a marker.
func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKeys[k] {
				out[k] = redactedMarker
			} else {
				out[k] = redact(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val)
		}
		return out
	default:
		return v
	}
}
