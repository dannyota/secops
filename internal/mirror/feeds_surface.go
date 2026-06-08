package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// feeds as code, on the reconcile engine. Feeds carry SECRETS in their
// source-specific settings, so this surface uses the same redaction round-trip as
// the JSON surfaces: secrets are masked on pull, and on update the operator's
// edits are overlaid onto the live (unredacted) body so the real secret is
// preserved and the mask is never sent back. A create that still carries a mask
// is refused.
//
// On the wire a feed's config lives under details{} (feedSourceType, logType,
// assetNamespace, labels, and source-specific *Settings siblings). The canonical
// splits those into stable fields and drops server-managed/runtime keys. Feed
// STATE (enabled/disabled) is a runtime toggle via :enable/:disable verbs, NOT
// config — it is deliberately excluded from desired state.

// feedSplitKeys are the details keys the canonical promotes to their own fields.
var feedSplitKeys = map[string]bool{
	"feedSourceType": true, "logType": true, "assetNamespace": true, "labels": true,
}

// feedServerKeys are server-managed/migration details that must never enter the
// diff basis (verified present on live feeds).
var feedServerKeys = map[string]bool{
	"lastV2MigrationAttemptTime": true,
	"stsMigrationReadiness":      true,
}

// feedSpec is the diff basis: the meaningful, operator-editable feed config.
// Settings carries the source-specific block (httpSettings/gcsSettings/...) with
// secrets redacted before canonicalization.
type feedSpec struct {
	DisplayName    string         `json:"display_name"`
	SourceType     string         `json:"source_type,omitempty"`
	LogType        string         `json:"log_type,omitempty"`
	AssetNamespace string         `json:"asset_namespace,omitempty"`
	Labels         any            `json:"labels,omitempty"`
	Settings       map[string]any `json:"settings,omitempty"`
}

// feedOnDisk is the subset of `<slug>.yaml` LoadDir reads back (the puller writes
// more, but only these feed the canonical + identity).
type feedOnDisk struct {
	DisplayName    string         `yaml:"display_name"`
	Name           string         `yaml:"name"`
	SourceType     string         `yaml:"source_type"`
	LogType        string         `yaml:"log_type"`
	AssetNamespace string         `yaml:"asset_namespace"`
	Labels         any            `yaml:"labels"`
	Settings       map[string]any `yaml:"settings"`
}

func feedsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "feeds",
		Dir:     DirFeeds,
		Product: reconcile.ProductSIEM,
		// No etag; details is replaced wholesale on PATCH so Update overlays onto
		// the live body. Delete stops ingestion (high-blast) → not prune-eligible.
		Caps: reconcile.Capabilities{NoEtag: true, WholeBodyWrite: true},

		List:    feedsList(c),
		LoadDir: loadFeeds,
		Write:   writeFeedObject,
		Create:  feedsCreate(c),
		Update:  feedsUpdate(c),
		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteFeed(ctx, lastSegment(live.ServerID))
		},
	}
}

func feedsList(c *chronicle.Client) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		feeds, err := c.ListFeeds(ctx)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		for _, f := range feeds {
			o, berr := feedLiveObject(f)
			if berr != nil {
				warnf("feeds: build %s: %v", feedDisplay(f), berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// feedLiveObject builds the engine object: redacted canonical for the diff basis,
// the full UNREDACTED feed in Raw for the Update overlay.
func feedLiveObject(f chronicle.Feed) (reconcile.Object, error) {
	canon, err := canonicalFeed(feedSpecFromFeed(f))
	if err != nil {
		return reconcile.Object{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(feedDisplay(f)), ServerID: f.Name, Canonical: canon, Raw: raw}, nil
}

func loadFeeds(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		var od feedOnDisk
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &od); rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalFeed(feedSpec{
			DisplayName:    od.DisplayName,
			SourceType:     od.SourceType,
			LogType:        od.LogType,
			AssetNamespace: od.AssetNamespace,
			Labels:         od.Labels,
			Settings:       od.Settings,
		})
		if cerr != nil {
			return nil, cerr
		}
		raw, rerr := json.Marshal(feedSpec{
			DisplayName:    od.DisplayName,
			SourceType:     od.SourceType,
			LogType:        od.LogType,
			AssetNamespace: od.AssetNamespace,
			Labels:         od.Labels,
			Settings:       od.Settings,
		})
		if rerr != nil {
			return nil, rerr
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".yaml"),
			ServerID:  od.Name,
			Canonical: canon,
			Raw:       raw,
		})
	}
	return objs, nil
}

// writeFeedObject renders a LIVE/echo object back to `<slug>.yaml`, reusing the
// existing pull writer so pull and reconcile-Write produce identical files.
func writeFeedObject(dir string, o reconcile.Object) error {
	if len(o.Raw) == 0 {
		return fmt.Errorf("feeds: cannot write %q without a live model", o.Slug)
	}
	var f chronicle.Feed
	if err := json.Unmarshal(o.Raw, &f); err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), feedRecord(f, feedDisplay(f)))
}

// feedsCreate creates a feed from the local spec, refusing a body that still
// carries a redaction marker (a real secret can't be invented from a mask).
func feedsCreate(c *chronicle.Client) func(context.Context, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
		spec, err := decodeLocalFeedSpec(local)
		if err != nil {
			return reconcile.Object{}, err
		}
		spec, err = resolveFeedSpecSecrets(ctx, c, spec)
		if err != nil {
			return reconcile.Object{}, err
		}
		body, err := json.Marshal(spec)
		if err != nil {
			return reconcile.Object{}, err
		}
		if reconcile.ContainsValue(body, redactedMarker) {
			return reconcile.Object{}, fmt.Errorf(
				"refusing to create %q: body still contains a redaction marker (%s); supply the real secret first",
				local.Slug, redactedMarker)
		}
		created, err := c.CreateFeed(ctx, spec.DisplayName, spec.SourceType, spec.LogType, spec.AssetNamespace, feedWriteSettings(spec))
		if err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetFeed(ctx, created.Name)
		if err != nil {
			return reconcile.Object{}, err
		}
		return feedLiveObject(*full)
	}
}

// feedsUpdate overlays local edits onto the live details (preserving redacted
// secrets), then sends the full body — details is replaced wholesale on PATCH.
func feedsUpdate(c *chronicle.Client) func(context.Context, reconcile.Object, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
		var liveFeed chronicle.Feed
		if err := json.Unmarshal(live.Raw, &liveFeed); err != nil {
			return reconcile.Object{}, err
		}
		localSpec, err := decodeLocalFeedSpec(local)
		if err != nil {
			return reconcile.Object{}, err
		}
		localSpec, err = resolveFeedSpecSecrets(ctx, c, localSpec)
		if err != nil {
			return reconcile.Object{}, err
		}
		// Overlay local edits onto the live (unredacted) spec: where local still
		// holds the mask, keep the live secret; otherwise local wins.
		liveSpecJSON, err := json.Marshal(feedSpecFromFeed(liveFeed))
		if err != nil {
			return reconcile.Object{}, err
		}
		localSpecJSON, err := json.Marshal(localSpec)
		if err != nil {
			return reconcile.Object{}, err
		}
		merged, err := reconcile.DeepMerge(liveSpecJSON, localSpecJSON, func(_ string, v any) bool {
			s, ok := v.(string)
			return ok && s == redactedMarker
		})
		if err != nil {
			return reconcile.Object{}, err
		}
		// Scalar markers are kept-from-live by the skip above, but a marker nested
		// in an array survives the wholesale array replace — refuse rather than
		// overwrite the live secret with the mask.
		if reconcile.ContainsValue(merged, redactedMarker) {
			return reconcile.Object{}, fmt.Errorf(
				"refusing to update %q: merged body still contains a redaction marker (%s); supply the real secret before pushing",
				local.Slug, redactedMarker)
		}
		spec, err := decodeFeedSpec(merged)
		if err != nil {
			return reconcile.Object{}, err
		}
		if _, err := c.UpdateFeed(ctx, lastSegment(live.ServerID), spec.DisplayName, spec.SourceType, spec.LogType, spec.AssetNamespace, feedWriteSettings(spec)); err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetFeed(ctx, lastSegment(live.ServerID))
		if err != nil {
			return reconcile.Object{}, err
		}
		return feedLiveObject(*full)
	}
}

// --- helpers ----------------------------------------------------------------

// feedSpecFromFeed reduces a live feed to the diff-basis spec.
func feedSpecFromFeed(f chronicle.Feed) feedSpec {
	d := f.Details
	return feedSpec{
		DisplayName:    feedDisplay(f),
		SourceType:     asString(d["feedSourceType"]),
		LogType:        lastSegment(asString(d["logType"])),
		AssetNamespace: asString(d["assetNamespace"]),
		Labels:         d["labels"],
		Settings:       feedSettings(d),
	}
}

// feedSettings is the source-specific settings block: every details key that is
// not a promoted field and not a server-managed key.
func feedSettings(details map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range details {
		if feedSplitKeys[k] || feedServerKeys[k] {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// feedWriteSettings rebuilds the settings map the SDK merges into details on
// write, folding labels back in (CreateFeed/UpdateFeed carry labels via settings).
func feedWriteSettings(spec feedSpec) map[string]any {
	out := map[string]any{}
	maps.Copy(out, spec.Settings)
	if spec.Labels != nil {
		out["labels"] = spec.Labels
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// feedDisplay is the display name with the puller's fallbacks.
func feedDisplay(f chronicle.Feed) string {
	if f.DisplayName != "" {
		return f.DisplayName
	}
	if f.UID != "" {
		return f.UID
	}
	return "unnamed"
}

// canonicalFeed redacts then canonicalizes the spec (redaction MUST run on both
// the live and on-disk sides so a masked secret never differs from itself).
func canonicalFeed(spec feedSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	rb, err := json.Marshal(redact(v))
	if err != nil {
		return nil, err
	}
	// Strip server-managed/migration keys at any depth so a live feed and a
	// pulled file (whose settings block still carries them) canonicalize equal.
	return reconcile.Canonicalize(rb, feedServerStripKeys...)
}

// feedServerStripKeys are dropped from the canonical at any depth (they can ride
// inside the on-disk settings block but are never operator config).
var feedServerStripKeys = []string{"lastV2MigrationAttemptTime", "stsMigrationReadiness"}

func decodeFeedSpec(canonical []byte) (feedSpec, error) {
	var spec feedSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}

func decodeLocalFeedSpec(local reconcile.Object) (feedSpec, error) {
	if len(local.Raw) > 0 {
		return decodeFeedSpec(local.Raw)
	}
	return decodeFeedSpec(local.Canonical)
}
