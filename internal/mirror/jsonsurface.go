package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/internal/mirror/reconcile"
)

// jsonSurface adapts a RawJSON per-object SOAR endpoint to a reconcile.Surface.
// It is the cheap fan-out: a new Lane-1 SOAR surface = one jsonSurfaceSpec wired
// to the legacy SDK methods, no bespoke puller. ONLY clean per-object surfaces
// (stable id, read-shape round-trips to write-shape, delete-by-id) belong here;
// batch upserts, export/import bundles, and selector-only reads do not.
//
// On disk each object is `<slug>.json`: the redacted, volatile-stripped config
// fields plus a reserved `_server` identity block (id/etag). Canonicalize strips
// `_server`, so the block never pollutes a git diff. Pull redacts secrets;
// Update overlays the operator's edits onto the live (unredacted) body and drops
// any field still masked, so a real secret is never overwritten by its mask.

// Function shapes the registries bind to specific legacy SDK methods. RawJSON is
// json.RawMessage, so e.g. lc.ListWebhookCards fits rawListFn directly.
type (
	rawListFn = func(ctx context.Context) (json.RawMessage, error)
	rawGetFn  = func(ctx context.Context, id string) (json.RawMessage, error)
	rawBodyFn = func(ctx context.Context, body any) (json.RawMessage, error)
	rawDelFn  = func(ctx context.Context, id string) (json.RawMessage, error)
)

type jsonSurfaceSpec struct {
	name, dir  string
	product    reconcile.Product
	caps       reconcile.Capabilities
	idField    string   // JSON key of the server identity (e.g. "identifier")
	nameField  string   // JSON key of the display name (e.g. "name")
	extraStrip []string // surface-specific volatile keys to drop from the diff

	list   rawListFn // required
	getOne rawGetFn  // optional: full read by id (else the list item is used)
	create rawBodyFn // optional
	update rawBodyFn // optional
	del    rawDelFn  // optional
}

func jsonSurface(spec jsonSurfaceSpec) reconcile.Surface {
	s := reconcile.Surface{
		Name:    spec.name,
		Dir:     spec.dir,
		Product: spec.product,
		Caps:    spec.caps,
		List:    spec.listObjects,
		LoadDir: spec.loadDir,
		Write:   spec.write,
	}
	if spec.create != nil {
		s.Create = spec.createObject
	}
	if spec.update != nil {
		s.Update = spec.updateObject
	}
	if spec.del != nil {
		s.Delete = spec.deleteObject
	}
	return s
}

// listObjects lists live objects, fetching the full body per item when getOne is
// set. A per-item failure is warned and marks the listing incomplete (which
// disables --prune) rather than aborting the whole surface.
func (spec jsonSurfaceSpec) listObjects(ctx context.Context) (reconcile.ListResult, error) {
	raw, err := spec.list(ctx)
	if err != nil {
		return reconcile.ListResult{}, err
	}
	items, err := decodeRawList(raw)
	if err != nil {
		return reconcile.ListResult{}, fmt.Errorf("%s: %w", spec.name, err)
	}
	var objs []reconcile.Object
	incomplete := false
	for _, it := range items {
		full := it
		if id := jsonField(it, spec.idField); spec.getOne != nil && id != "" {
			g, gerr := spec.getOne(ctx, id)
			if gerr != nil {
				warnf("%s: get %q: %v", spec.name, id, gerr)
				incomplete = true
				continue
			}
			full = g
		}
		o, berr := spec.buildObject(full)
		if berr != nil {
			warnf("%s: build object: %v", spec.name, berr)
			incomplete = true
			continue
		}
		objs = append(objs, o)
	}
	return reconcile.ListResult{Objects: objs, Incomplete: incomplete}, nil
}

// buildObject redacts a live body, canonicalizes it for the diff basis, and
// keeps the UNREDACTED body in Raw for Update overlay.
func (spec jsonSurfaceSpec) buildObject(full json.RawMessage) (reconcile.Object, error) {
	var v any
	if err := json.Unmarshal(full, &v); err != nil {
		return reconcile.Object{}, err
	}
	rb, err := json.Marshal(redact(v))
	if err != nil {
		return reconcile.Object{}, err
	}
	canon, err := reconcile.Canonicalize(rb, spec.extraStrip...)
	if err != nil {
		return reconcile.Object{}, err
	}
	name := jsonField(full, spec.nameField)
	id := jsonField(full, spec.idField)
	if id == "" && name == "" {
		// Neither identity field resolved — the surface is misconfigured for this
		// payload (e.g. a nested/grouped list shape). Fail loudly rather than write
		// an "_unnamed" file with an empty server id (which collides + re-creates).
		return reconcile.Object{}, fmt.Errorf(
			"%s: object has neither %q (id) nor %q (name) at top level", spec.name, spec.idField, spec.nameField)
	}
	if name == "" {
		name = id
	}
	return reconcile.Object{
		Slug:      Slugify(name),
		ServerID:  id,
		Canonical: canon,
		Raw:       full,
	}, nil
}

func (spec jsonSurfaceSpec) loadDir(dir string) ([]reconcile.Object, error) {
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
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		id, etag := serverBlock(b)
		canon, err := reconcile.Canonicalize(b, spec.extraStrip...)
		if err != nil {
			return nil, fmt.Errorf("%s: canonicalize %s: %w", spec.name, e.Name(), err)
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".json"),
			ServerID:  id,
			Etag:      etag,
			Canonical: canon,
		})
	}
	return objs, nil
}

// write renders one object as `<slug>.json`: canonical config fields plus the
// reserved `_server` identity block.
func (spec jsonSurfaceSpec) write(dir string, o reconcile.Object) error {
	fields := map[string]any{}
	if len(o.Canonical) > 0 {
		if err := json.Unmarshal(o.Canonical, &fields); err != nil {
			return err
		}
	}
	server := map[string]any{"id": o.ServerID}
	if o.Etag != "" {
		server["etag"] = o.Etag
	}
	fields["_server"] = server
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
}

func (spec jsonSurfaceSpec) createObject(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
	if reconcile.ContainsValue(local.Canonical, redactedMarker) {
		return reconcile.Object{}, fmt.Errorf(
			"refusing to create %q: body still contains a redaction marker (%s); supply the real value first",
			local.Slug, redactedMarker)
	}
	// Guard against an upsert-keyed-by-id surface (e.g. connectors' SaveConnector):
	// a file that still carries an existing live id is an UPDATE, not a create —
	// creating it would overwrite that live object. Refuse and tell the operator.
	if id := jsonField(local.Canonical, spec.idField); id != "" {
		if exists, err := spec.idExistsLive(ctx, id); err == nil && exists {
			return reconcile.Object{}, fmt.Errorf(
				"refusing to create %q: %s %q already exists live — keep its _server block to update it, or assign a new %s",
				local.Slug, spec.idField, id, spec.idField)
		}
	}
	var body any
	if err := json.Unmarshal(local.Canonical, &body); err != nil {
		return reconcile.Object{}, err
	}
	if _, err := spec.create(ctx, body); err != nil {
		return reconcile.Object{}, err
	}
	// The create API may not echo a usable id; re-resolve by display name.
	return spec.resolveByName(ctx, jsonField(local.Canonical, spec.nameField))
}

func (spec jsonSurfaceSpec) updateObject(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
	merged, err := reconcile.DeepMerge(live.Raw, local.Canonical, func(_ string, v any) bool {
		s, ok := v.(string)
		return ok && s == redactedMarker
	})
	if err != nil {
		return reconcile.Object{}, err
	}
	var body any
	if err := json.Unmarshal(merged, &body); err != nil {
		return reconcile.Object{}, err
	}
	if _, err := spec.update(ctx, body); err != nil {
		return reconcile.Object{}, err
	}
	full := merged
	if spec.getOne != nil && live.ServerID != "" {
		if g, gerr := spec.getOne(ctx, live.ServerID); gerr == nil {
			full = g
		}
	}
	return spec.buildObject(full)
}

func (spec jsonSurfaceSpec) deleteObject(ctx context.Context, live reconcile.Object) error {
	_, err := spec.del(ctx, live.ServerID)
	return err
}

// idExistsLive reports whether any live object already has the given id.
func (spec jsonSurfaceSpec) idExistsLive(ctx context.Context, id string) (bool, error) {
	raw, err := spec.list(ctx)
	if err != nil {
		return false, err
	}
	items, err := decodeRawList(raw)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if jsonField(it, spec.idField) == id {
			return true, nil
		}
	}
	return false, nil
}

// resolveByName lists and returns the object whose nameField matches name,
// fetching the full body when getOne is set. Used after a create to capture the
// server-assigned id.
func (spec jsonSurfaceSpec) resolveByName(ctx context.Context, name string) (reconcile.Object, error) {
	raw, err := spec.list(ctx)
	if err != nil {
		return reconcile.Object{}, err
	}
	items, err := decodeRawList(raw)
	if err != nil {
		return reconcile.Object{}, err
	}
	for _, it := range items {
		if jsonField(it, spec.nameField) != name {
			continue
		}
		full := it
		if id := jsonField(it, spec.idField); spec.getOne != nil && id != "" {
			if g, gerr := spec.getOne(ctx, id); gerr == nil {
				full = g
			}
		}
		return spec.buildObject(full)
	}
	return reconcile.Object{}, fmt.Errorf("%s: created %q not found on re-list", spec.name, name)
}

// decodeRawList accepts either a bare JSON array or an object wrapping the
// records in its first array-valued field (keys tried in sorted order for
// determinism), matching the legacy smoke harness's objects().
func decodeRawList(raw json.RawMessage) ([]json.RawMessage, error) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return arr, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var a []json.RawMessage
			if json.Unmarshal(obj[k], &a) == nil {
				return a, nil
			}
		}
	}
	return nil, fmt.Errorf("response is neither a JSON array nor an object wrapping one")
}

// jsonField returns the named top-level field as a string (coercing a numeric id
// to its integer form), or "" if absent.
func jsonField(raw json.RawMessage, key string) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case json.Number:
		return v.String()
	}
	return ""
}

// serverBlock extracts the reserved `_server` identity from an on-disk object.
func serverBlock(raw json.RawMessage) (id, etag string) {
	var m struct {
		Server struct {
			ID   any    `json:"id"`
			Etag string `json:"etag"`
		} `json:"_server"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return "", ""
	}
	switch v := m.Server.ID.(type) {
	case string:
		id = v
	case float64:
		id = fmt.Sprintf("%.0f", v)
	}
	return id, m.Server.Etag
}
