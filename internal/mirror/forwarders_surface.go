package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// forwarders as code, on the reconcile engine. A forwarder is an on-prem
// ingestion agent; this surface manages the forwarder objects themselves (its
// collectors are a separate, nested resource). The diff basis is the
// operator-editable config: a display name plus the freeform config block
// (uploadCompression, metadata, serverSettings, gracefulTimeout, ...). Server
// identity (name) and server-stamped time fields are carried separately or
// stripped, so a pulled snapshot pushes back in sync.
//
// On disk: one `<slug>.yaml` per forwarder, holding the display name, the server
// name (identity), and the config block. Delete is a clean by-id delete, so the
// surface is prune-eligible. Forwarders carry no etag.

// fwdServerStripKeys are runtime/server-managed keys dropped from the diff basis
// at any depth — they ride on a live forwarder but are never operator config.
var fwdServerStripKeys = []string{"state"}

// fwdSpec is the diff basis: the meaningful, operator-editable forwarder config.
type fwdSpec struct {
	DisplayName string         `json:"display_name"`
	Config      map[string]any `json:"config,omitempty"`
}

// fwdOnDisk is the `<slug>.yaml` shape LoadDir reads back.
type fwdOnDisk struct {
	DisplayName string         `yaml:"display_name"`
	Name        string         `yaml:"name"`
	Config      map[string]any `yaml:"config"`
}

func forwardersSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "forwarders",
		Dir:     DirForwarders,
		Product: reconcile.ProductSIEM,
		// No etag; config is replaced wholesale on PATCH so Update overlays local
		// edits onto the live body. Delete is a clean by-id delete → prune-eligible.
		Caps: reconcile.Capabilities{NoEtag: true, WholeBodyWrite: true, PruneEligible: true},

		List:    forwardersList(c),
		LoadDir: loadForwarders,
		Write:   writeForwarderObject,
		Create:  forwardersCreate(c),
		Update:  forwardersUpdate(c),
		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteForwarder(ctx, lastSegment(live.ServerID))
		},
	}
}

func forwardersList(c *chronicle.Client) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		fwds, err := c.ListForwarders(ctx)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		for _, f := range fwds {
			o, berr := forwarderLiveObject(f)
			if berr != nil {
				warnf("forwarders: build %s: %v", forwarderDisplay(f), berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// forwarderLiveObject builds the engine object: the canonical diff basis plus the
// full live forwarder in Raw for the Update overlay.
func forwarderLiveObject(f chronicle.Forwarder) (reconcile.Object, error) {
	cfg, err := forwarderConfigMap(f)
	if err != nil {
		return reconcile.Object{}, err
	}
	canon, err := canonicalForwarder(fwdSpec{DisplayName: forwarderDisplay(f), Config: cfg})
	if err != nil {
		return reconcile.Object{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(forwarderDisplay(f)), ServerID: f.Name, Canonical: canon, Raw: raw}, nil
}

func loadForwarders(dir string) ([]reconcile.Object, error) {
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
		var od fwdOnDisk
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &od); rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalForwarder(fwdSpec{DisplayName: od.DisplayName, Config: od.Config})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".yaml"),
			ServerID:  od.Name,
			Canonical: canon,
		})
	}
	return objs, nil
}

// writeForwarderObject renders a LIVE/echo object back to `<slug>.yaml`.
func writeForwarderObject(dir string, o reconcile.Object) error {
	if len(o.Raw) == 0 {
		return fmt.Errorf("forwarders: cannot write %q without a live model", o.Slug)
	}
	var f chronicle.Forwarder
	if err := json.Unmarshal(o.Raw, &f); err != nil {
		return err
	}
	cfg, err := forwarderConfigMap(f)
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	rec := map[string]any{
		"display_name": forwarderDisplay(f),
		"name":         f.Name,
		"config":       cfg,
	}
	for k, v := range rec {
		if isEmptyValue(v) {
			delete(rec, k)
		}
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), rec)
}

// forwardersCreate creates a forwarder from the local spec.
func forwardersCreate(c *chronicle.Client) func(context.Context, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
		spec, err := decodeForwarderSpec(local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		created, err := c.CreateForwarder(ctx, forwarderBody(spec))
		if err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetForwarder(ctx, created.Name)
		if err != nil {
			return reconcile.Object{}, err
		}
		return forwarderLiveObject(*full)
	}
}

// forwardersUpdate overlays local edits onto the live config, then sends the
// full body — config is replaced wholesale on PATCH.
func forwardersUpdate(c *chronicle.Client) func(context.Context, reconcile.Object, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
		var liveFwd chronicle.Forwarder
		if err := json.Unmarshal(live.Raw, &liveFwd); err != nil {
			return reconcile.Object{}, err
		}
		liveCfg, err := forwarderConfigMap(liveFwd)
		if err != nil {
			return reconcile.Object{}, err
		}
		liveSpecJSON, err := json.Marshal(fwdSpec{DisplayName: forwarderDisplay(liveFwd), Config: liveCfg})
		if err != nil {
			return reconcile.Object{}, err
		}
		merged, err := reconcile.DeepMerge(liveSpecJSON, local.Canonical, nil)
		if err != nil {
			return reconcile.Object{}, err
		}
		spec, err := decodeForwarderSpec(merged)
		if err != nil {
			return reconcile.Object{}, err
		}
		if _, err := c.UpdateForwarder(ctx, lastSegment(live.ServerID), forwarderBody(spec), "display_name", "config"); err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetForwarder(ctx, lastSegment(live.ServerID))
		if err != nil {
			return reconcile.Object{}, err
		}
		return forwarderLiveObject(*full)
	}
}

// --- helpers ----------------------------------------------------------------

// forwarderConfigMap decodes a forwarder's freeform config blob into a map (nil
// when the forwarder carries no config).
func forwarderConfigMap(f chronicle.Forwarder) (map[string]any, error) {
	if len(f.Config) == 0 {
		return nil, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(f.Config, &cfg); err != nil {
		return nil, err
	}
	if len(cfg) == 0 {
		return nil, nil
	}
	return cfg, nil
}

// forwarderBody builds the create/update request body from a spec.
func forwarderBody(spec fwdSpec) map[string]any {
	body := map[string]any{"displayName": spec.DisplayName}
	if len(spec.Config) > 0 {
		body["config"] = spec.Config
	}
	return body
}

// forwarderDisplay is the display name with a fallback to the server id.
func forwarderDisplay(f chronicle.Forwarder) string {
	if f.DisplayName != "" {
		return f.DisplayName
	}
	if id := lastSegment(f.Name); id != "" {
		return id
	}
	return "unnamed"
}

// canonicalForwarder canonicalizes the spec, stripping runtime/server keys (and
// the time keys Canonicalize drops by default) at any depth so a live forwarder
// and a pulled file diff equal.
func canonicalForwarder(spec fwdSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw, fwdServerStripKeys...)
}

func decodeForwarderSpec(canonical []byte) (fwdSpec, error) {
	var spec fwdSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}
