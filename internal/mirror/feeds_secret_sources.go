package mirror

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
)

const feedSecretRefKey = "secret_ref"

// resolveFeedSpecSecrets replaces feed secret_ref objects with their runtime
// values immediately before a live create/update. The redacted canonical used
// for planning never stores those values.
func resolveFeedSpecSecrets(ctx context.Context, c *chronicle.Client, spec feedSpec) (feedSpec, error) {
	settings, err := resolveFeedSecretRefs(ctx, c, spec.Settings)
	if err != nil {
		return feedSpec{}, err
	}
	out := spec
	if settings == nil {
		out.Settings = nil
	} else if m, ok := settings.(map[string]any); ok {
		out.Settings = m
	} else {
		return feedSpec{}, fmt.Errorf("feeds: settings resolved to %T, want object", settings)
	}
	return out, nil
}

func resolveFeedSecretRefs(ctx context.Context, c *chronicle.Client, v any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		if ref, ok := t[feedSecretRefKey]; ok {
			if len(t) != 1 {
				return nil, fmt.Errorf("feeds: %s object must not contain sibling keys", feedSecretRefKey)
			}
			s, ok := ref.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("feeds: %s must be a non-empty string", feedSecretRefKey)
			}
			return resolveFeedSecretRef(ctx, c, s)
		}
		out := make(map[string]any, len(t))
		for k, val := range t {
			resolved, err := resolveFeedSecretRefs(ctx, c, val)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			resolved, err := resolveFeedSecretRefs(ctx, c, val)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}

func resolveFeedSecretRef(ctx context.Context, c *chronicle.Client, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if name, ok := strings.CutPrefix(ref, "env:"); ok {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("feeds: env secret_ref has an empty variable name")
		}
		val, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("feeds: env secret_ref variable is not set")
		}
		return val, nil
	}
	if name, ok := strings.CutPrefix(ref, "secretmanager:"); ok {
		return accessSecretManager(ctx, c, name)
	}
	if name, ok := strings.CutPrefix(ref, "sm://"); ok {
		return accessSecretManager(ctx, c, name)
	}
	return "", fmt.Errorf("feeds: unsupported secret_ref scheme (want env: or secretmanager:)")
}

func accessSecretManager(ctx context.Context, c *chronicle.Client, ref string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("feeds: Secret Manager secret_ref requires a Chronicle client")
	}
	resource, err := secretManagerResource(c.Settings(), ref)
	if err != nil {
		return "", err
	}
	endpoint := "https://secretmanager.googleapis.com/v1/" + strings.TrimLeft(resource, "/") + ":access"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("feeds: build Secret Manager request: %w", err)
	}

	force := c.Settings().ForceIPv4
	hc := &http.Client{
		Timeout:   60 * time.Second,
		Transport: auth.RoundTripper(auth.OAuth(auth.WithForceIPv4(force)), auth.HTTPTransport(force)),
	}

	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("feeds: access Secret Manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("feeds: read Secret Manager response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("feeds: Secret Manager access failed with HTTP %d", resp.StatusCode)
	}
	var out struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("feeds: decode Secret Manager response: %w", err)
	}
	secret, err := base64.StdEncoding.DecodeString(out.Payload.Data)
	if err != nil {
		return "", fmt.Errorf("feeds: decode Secret Manager payload: %w", err)
	}
	return string(secret), nil
}

func secretManagerResource(s chronicle.Settings, ref string) (string, error) {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	if ref == "" {
		return "", fmt.Errorf("feeds: Secret Manager secret_ref is empty")
	}
	if strings.HasPrefix(ref, "projects/") {
		if strings.Contains(ref, "/versions/") {
			return ref, nil
		}
		if strings.Contains(ref, "/secrets/") {
			return ref + "/versions/latest", nil
		}
		return "", fmt.Errorf("feeds: Secret Manager resource must include /secrets/")
	}
	if strings.Contains(ref, "/") {
		return "", fmt.Errorf("feeds: Secret Manager resource must start with projects/")
	}
	if strings.TrimSpace(s.ProjectID) == "" {
		return "", fmt.Errorf("feeds: project id is required for short Secret Manager secret_ref")
	}
	return "projects/" + url.PathEscape(s.ProjectID) + "/secrets/" + url.PathEscape(ref) + "/versions/latest", nil
}
