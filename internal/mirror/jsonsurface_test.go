package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// fakeAPI is an in-memory per-object-CUD endpoint backing a jsonSurfaceSpec, so
// the adapter (redaction, _server round-trip, create/update/delete, the refresh
// and collision guards) is testable with no network.
type fakeAPI struct {
	objs map[string]map[string]any
	seq  int
}

func newFakeAPI() *fakeAPI { return &fakeAPI{objs: map[string]map[string]any{}} }

func (f *fakeAPI) list(_ context.Context) (json.RawMessage, error) {
	arr := make([]map[string]any, 0, len(f.objs))
	for _, o := range f.objs {
		arr = append(arr, o)
	}
	return json.Marshal(arr)
}

func (f *fakeAPI) get(_ context.Context, id string) (json.RawMessage, error) {
	o, ok := f.objs[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return json.Marshal(o)
}

func (f *fakeAPI) create(_ context.Context, body any) (json.RawMessage, error) {
	m := asMap(body)
	id, _ := m["identifier"].(string)
	if id == "" {
		f.seq++
		id = fmt.Sprintf("srv-%d", f.seq)
		m["identifier"] = id
	}
	f.objs[id] = m
	return json.Marshal(m)
}

func (f *fakeAPI) update(_ context.Context, body any) (json.RawMessage, error) {
	m := asMap(body)
	id, _ := m["identifier"].(string)
	if id == "" {
		return nil, fmt.Errorf("update: no identifier")
	}
	f.objs[id] = m
	return json.Marshal(m)
}

func (f *fakeAPI) del(_ context.Context, id string) (json.RawMessage, error) {
	delete(f.objs, id)
	return nil, nil
}

func asMap(body any) map[string]any {
	b, _ := json.Marshal(body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func fakeWebhookSurface(f *fakeAPI) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name:      "fakehooks",
		dir:       "fakehooks",
		product:   reconcile.ProductSOAR,
		idField:   "identifier",
		nameField: "name",
		caps:      reconcile.Capabilities{WholeBodyWrite: true, PruneEligible: true},
		list:      f.list,
		getOne:    f.get,
		create:    f.create,
		update:    f.update,
		del:       f.del,
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestJSONSurfacePullRedactsAndRoundTrips: pull masks the secret on disk, embeds
// the _server id, and a freshly pulled snapshot diffs as Unchanged (the canonical
// of local == live despite the mask).
func TestJSONSurfacePullRedactsAndRoundTrips(t *testing.T) {
	f := newFakeAPI()
	f.objs["w1"] = map[string]any{"identifier": "w1", "name": "hook1", "apiKey": "realsecret", "url": "http://a"}
	s := fakeWebhookSurface(f)
	dir := t.TempDir()
	ctx := context.Background()

	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, filepath.Join(dir, "hook1.json"))
	if strings.Contains(body, "realsecret") {
		t.Errorf("pulled snapshot leaked the secret:\n%s", body)
	}
	if !strings.Contains(body, redactedMarker) || !strings.Contains(body, `"_server"`) || !strings.Contains(body, "w1") {
		t.Errorf("snapshot missing redaction marker or _server id:\n%s", body)
	}

	plan, _, err := reconcile.BuildPlan(ctx, s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Errorf("a fresh pull should diff clean, got creates=%d updates=%d deletes=%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}
}

// TestJSONSurfaceUpdatePreservesSecret: editing a non-secret field and pushing
// overlays the edit onto the live body and never writes the mask over the real
// secret.
func TestJSONSurfaceUpdatePreservesSecret(t *testing.T) {
	f := newFakeAPI()
	f.objs["w1"] = map[string]any{"identifier": "w1", "name": "hook1", "apiKey": "realsecret", "url": "http://a"}
	s := fakeWebhookSurface(f)
	dir := t.TempDir()
	ctx := context.Background()
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Edit the url in the on-disk file (apiKey stays masked).
	path := filepath.Join(dir, "hook1.json")
	var m map[string]any
	_ = json.Unmarshal([]byte(readFile(t, path)), &m)
	m["url"] = "http://b"
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := f.objs["w1"]["url"]; got != "http://b" {
		t.Errorf("update not applied: url=%v", got)
	}
	if got := f.objs["w1"]["apiKey"]; got != "realsecret" {
		t.Errorf("the masked secret overwrote the real one: apiKey=%v", got)
	}
}

// TestJSONSurfaceCreateRefreshesOriginalFile: a create writes the server id back
// into the OPERATOR'S file (not a slugified copy), so a second push is a no-op
// rather than a duplicate create. Guards the refreshLocal fix.
func TestJSONSurfaceCreateRefreshesOriginalFile(t *testing.T) {
	f := newFakeAPI()
	s := fakeWebhookSurface(f)
	dir := t.TempDir()
	ctx := context.Background()

	// Operator hand-writes a new file with an arbitrary name and a display name
	// that slugifies DIFFERENTLY from the filename.
	newPath := filepath.Join(dir, "my-new-hook.json")
	if err := os.WriteFile(newPath, []byte(`{"name":"Fresh Hook","url":"http://x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(f.objs) != 1 {
		t.Fatalf("expected exactly one created object, got %d", len(f.objs))
	}
	// The server id must have been written back into the ORIGINAL file.
	if !strings.Contains(readFile(t, newPath), `"_server"`) {
		t.Errorf("create did not refresh the operator's file with the server id:\n%s", readFile(t, newPath))
	}
	// No slugified duplicate ("Fresh_Hook.json") should have appeared.
	if _, err := os.Stat(filepath.Join(dir, "Fresh_Hook.json")); err == nil {
		t.Error("create wrote a slugified duplicate file instead of refreshing the original")
	}
	// A second push must be a no-op (not another create).
	plan, _, err := reconcile.BuildPlan(ctx, s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Errorf("second push should be clean, got creates=%d", len(plan.Creates()))
	}
}

// TestJSONSurfaceWrap: the wrapKey option nests the send body under an envelope
// key (e.g. ontology AddOrUpdateVisualFamily wants {visualFamilyDataModel: record}).
func TestJSONSurfaceWrap(t *testing.T) {
	plain := jsonSurfaceSpec{}
	if got := plain.wrap(map[string]any{"a": 1}); got.(map[string]any)["a"] != 1 {
		t.Error("no wrapKey: body should pass through unchanged")
	}
	wrapped := jsonSurfaceSpec{wrapKey: "env"}.wrap(map[string]any{"a": 1}).(map[string]any)
	inner, ok := wrapped["env"].(map[string]any)
	if !ok || inner["a"] != 1 {
		t.Errorf("wrapKey should nest the body under the key, got %v", wrapped)
	}
}

// TestDecodeRawListUnwrapsObjectsList: the paged settings reads
// (GetEnvironments/GetNetworkDetails) wrap records in {metadata, objectsList};
// decodeRawList must return objectsList, not the metadata object. (Validated
// live: environments pull picked objectsList correctly.)
func TestDecodeRawListUnwrapsObjectsList(t *testing.T) {
	raw := json.RawMessage(`{"metadata":{"requestedPage":0,"totalCount":2},"objectsList":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	items, err := decodeRawList(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 records from objectsList, got %d", len(items))
	}
	if jsonField(items[0], "name") != "a" {
		t.Errorf("first record wrong: %s", items[0])
	}
}

// TestJSONSurfaceListIncompleteOnNoIdentity: an object with neither id nor name
// (e.g. a grouped/nested list shape, like connectors' grouped-by-integration
// cards) is skipped and marks the listing incomplete, instead of writing an
// "_unnamed" file. Guards against the misconfigured-surface failure mode.
func TestJSONSurfaceListIncompleteOnNoIdentity(t *testing.T) {
	f := newFakeAPI()
	f.objs["w1"] = map[string]any{"identifier": "w1", "name": "good", "url": "x"}
	f.objs["bad"] = map[string]any{"group": "g", "items": []any{}} // no identifier/name
	s := fakeWebhookSurface(f)

	res, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Incomplete {
		t.Error("expected Incomplete=true when an object has no identity")
	}
	if len(res.Objects) != 1 || res.Objects[0].Slug != Slugify("good") {
		t.Errorf("the well-formed object should still list, got %d objects", len(res.Objects))
	}
}

// TestJSONSurfaceCreateRefusesExistingID: a file that reuses an existing live id
// (e.g. a careless clone) is refused, never overwriting the live object.
func TestJSONSurfaceCreateRefusesExistingID(t *testing.T) {
	f := newFakeAPI()
	f.objs["w1"] = map[string]any{"identifier": "w1", "name": "original", "url": "http://a"}
	s := fakeWebhookSurface(f)
	dir := t.TempDir()
	ctx := context.Background()

	// A new file (no _server) that still carries the existing id w1.
	if err := os.WriteFile(filepath.Join(dir, "clone.json"),
		[]byte(`{"identifier":"w1","name":"clone","url":"http://evil"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatal(err)
	}
	if got := f.objs["w1"]["name"]; got != "original" {
		t.Errorf("create-collision overwrote the live object: name=%v", got)
	}
	if !strings.Contains(buf.String(), "already exists live") {
		t.Errorf("expected a collision refusal, got:\n%s", buf.String())
	}
}
