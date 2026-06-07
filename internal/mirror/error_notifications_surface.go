package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// errorNotificationConfigs (ingestion-health alerts) as code, on the reconcile
// engine. A config delivers a zero-ingest / size-threshold / normalization-delay
// alert to Cloud Monitoring notification channels. Full CRUD — create
// (server-assigns the id), update (PATCH), delete (prune-eligible), NoEtag.
//
// On disk each config is one `<slug>.json`: its body (displayName, enabled,
// notificationChannels, and exactly one notification_type block — kept raw, as the
// oneof shape varies) plus a reserved `_server` id block. The notification channel
// refs (projects/{project}/notificationChannels/{id}) and Pub/Sub-style condition
// blocks are passed through verbatim.

// errorNotifWritableMaskKeys maps each operator-editable JSON key to its
// snake_case updateMask path. The notification_type oneof variants map to the
// proto field `notification_type` (a oneof is masked by the containing field).
var errorNotifWritableMaskKeys = map[string]string{
	"displayName":                              "display_name",
	"enabled":                                  "enabled",
	"notificationChannels":                     "notification_channels",
	"ingestionCountZeroNotifications":          "notification_type",
	"ingestionSizeThresholdNotifications":      "notification_type",
	"normalizationDelayThresholdNotifications": "notification_type",
}

func errorNotificationsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "error_notifications",
		Dir:     DirErrorNotifs,
		Product: reconcile.ProductSIEM,
		// Clean delete-by-id (a notification config is benign/recreatable) →
		// prune-eligible. No etag on the resource.
		Caps: reconcile.Capabilities{PruneEligible: true, NoEtag: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			configs, err := c.ListErrorNotificationConfigs(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for i := range configs {
				o, berr := errorNotifObject(configs[i])
				if berr != nil {
					warnf("error_notifications: build %s: %v", configs[i].ID(), berr)
					res.Incomplete = true
					continue
				}
				res.Objects = append(res.Objects, o)
			}
			return res, nil
		},

		LoadDir: loadErrorNotifs,
		Write:   writeErrorNotifObject,

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			created, err := c.CreateErrorNotificationConfig(ctx, local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			if created.ID() == "" {
				return reconcile.Object{}, fmt.Errorf("error_notifications: create %q returned no resource name", local.Slug)
			}
			full, err := c.GetErrorNotificationConfig(ctx, created.ID())
			if err != nil {
				return reconcile.Object{}, err
			}
			return errorNotifObject(*full)
		},

		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			mask, err := errorNotifUpdateMask(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			id := lastSegment(live.ServerID)
			if _, err := c.UpdateErrorNotificationConfig(ctx, id, local.Canonical, mask); err != nil {
				return reconcile.Object{}, err
			}
			full, err := c.GetErrorNotificationConfig(ctx, id)
			if err != nil {
				return reconcile.Object{}, err
			}
			return errorNotifObject(*full)
		},

		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteErrorNotificationConfig(ctx, lastSegment(live.ServerID))
		},
	}
}

// errorNotifObject builds the engine object (canonical diff basis + id) for a
// live config.
func errorNotifObject(e chronicle.ErrorNotificationConfig) (reconcile.Object, error) {
	canon, err := errorNotifCanonical(e.Raw)
	if err != nil {
		return reconcile.Object{}, err
	}
	display := e.DisplayName
	if display == "" {
		display = e.ID()
	}
	if e.Name == "" {
		return reconcile.Object{}, fmt.Errorf("error notification config has no resource name")
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: e.Name, Canonical: canon}, nil
}

// errorNotifCanonical drops the root resource name (identity → ServerID) and
// canonicalizes (Canonicalize also strips etag + time fields).
func errorNotifCanonical(raw json.RawMessage) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "name")
	// `enabled` is a proto3 bool: the server omits it from a GET when false, so
	// normalize it to always-present (default false) — otherwise an operator file
	// with `enabled: false` would diff forever against a live config that omits it.
	if _, ok := m["enabled"]; !ok {
		m["enabled"] = false
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(b)
}

// errorNotifUpdateMask returns the updateMask paths for exactly the writable keys
// present in the body, so a PATCH never clears a field the operator didn't include.
func errorNotifUpdateMask(canonical []byte) ([]string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &m); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for key, path := range errorNotifWritableMaskKeys {
		if _, ok := m[key]; ok {
			seen[path] = true
		}
	}
	mask := make([]string, 0, len(seen))
	for p := range seen {
		mask = append(mask, p)
	}
	sort.Strings(mask)
	return mask, nil
}

func loadErrorNotifs(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		id, _ := serverBlock(b)
		canon, cerr := errorNotifCanonical(b)
		if cerr != nil {
			return nil, fmt.Errorf("error_notifications: canonicalize %s: %w", e.Name(), cerr)
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".json"),
			ServerID:  id,
			Canonical: canon,
		})
	}
	return objs, nil
}

// writeErrorNotifObject renders the canonical body plus the `_server` id block to
// `<slug>.json`.
func writeErrorNotifObject(dir string, o reconcile.Object) error {
	fields := map[string]any{}
	if len(o.Canonical) > 0 {
		if err := json.Unmarshal(o.Canonical, &fields); err != nil {
			return err
		}
	}
	fields["_server"] = map[string]any{"id": o.ServerID}
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
}
