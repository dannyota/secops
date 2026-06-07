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

// dataTaps (stream UDM events to a Cloud Pub/Sub topic) as code, on the reconcile
// engine. A tap is a typed object: display name, a filter (which events), a
// serialization format, and a Pub/Sub topic. Full CRUD — create (server-assigns
// the id), update (PATCH), delete (prune-eligible). NoEtag (no concurrency token).
//
// On disk each tap is one `<slug>.yaml` (display_name, name, filter,
// serialization_format, topic). Prerequisite for a working tap: grant Pub/Sub
// Publisher to publisher@chronicle-data-tap.iam.gserviceaccount.com on the topic.

// dataTapSerialization defaults a blank format to MARSHALLED_PROTO (the server
// default) so a pulled tap and a format-omitting local file canonicalize equal.
func dataTapSerialization(s string) string {
	if s == "" {
		return string(chronicle.DataTapProto)
	}
	return strings.ToUpper(s)
}

// dataTapSpec is the diff basis: the operator-editable tap config.
type dataTapSpec struct {
	DisplayName         string `json:"display_name"`
	Filter              string `json:"filter,omitempty"`
	SerializationFormat string `json:"serialization_format,omitempty"`
	Topic               string `json:"topic,omitempty"`
}

// dataTapMeta is the on-disk `<slug>.yaml` shape (spec + server identity).
type dataTapMeta struct {
	DisplayName         string `yaml:"display_name"`
	Name                string `yaml:"name"`
	Filter              string `yaml:"filter"`
	SerializationFormat string `yaml:"serialization_format"`
	Topic               string `yaml:"topic"`
}

func dataTapsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "datataps",
		Dir:     DirDataTaps,
		Product: reconcile.ProductSIEM,
		// Clean delete-by-id (a tap is a recreatable stream config) → prune-eligible.
		// No etag on the resource.
		Caps: reconcile.Capabilities{PruneEligible: true, NoEtag: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			taps, err := c.ListDataTaps(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for i := range taps {
				o, berr := dataTapObject(taps[i])
				if berr != nil {
					warnf("datataps: build %s: %v", taps[i].ID(), berr)
					res.Incomplete = true
					continue
				}
				res.Objects = append(res.Objects, o)
			}
			return res, nil
		},

		LoadDir: loadDataTaps,
		Write:   writeDataTapObject,

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeDataTapSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			created, err := c.CreateDataTap(ctx, dataTapFromSpec(spec))
			if err != nil {
				return reconcile.Object{}, err
			}
			return dataTapObject(*created)
		},

		// dataTaps PATCH is UNIMPLEMENTED (501) on the v1alpha backend, so an "update"
		// is done as delete-old + create-new (the id is server-assigned and changes;
		// a tap is a stateless stream config, so the brief gap is acceptable). When
		// the backend implements PATCH this can switch to UpdateDataTap.
		//
		// Limitation: because the id changes, a failure to write the refreshed local
		// file after the recreate leaves the stale (deleted) id on disk, so the next
		// push would re-create a duplicate. Both halves self-heal on a clean re-push
		// (re-pull reconciles the live set); acceptable for this Pre-GA surface.
		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeDataTapSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			if err := c.DeleteDataTap(ctx, lastSegment(live.ServerID)); err != nil {
				return reconcile.Object{}, fmt.Errorf("datataps: replace (delete old): %w", err)
			}
			created, err := c.CreateDataTap(ctx, dataTapFromSpec(spec))
			if err != nil {
				return reconcile.Object{}, fmt.Errorf("datataps: replace (create new) — the old tap was deleted: %w", err)
			}
			return dataTapObject(*created)
		},

		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteDataTap(ctx, lastSegment(live.ServerID))
		},
	}
}

// dataTapFromSpec builds the SDK type (always carrying the defaulted serialization
// format) from the diff-basis spec.
func dataTapFromSpec(spec dataTapSpec) chronicle.DataTap {
	t := chronicle.DataTap{
		DisplayName:         spec.DisplayName,
		Filter:              chronicle.DataTapFilter(spec.Filter),
		SerializationFormat: chronicle.DataTapSerialization(dataTapSerialization(spec.SerializationFormat)),
	}
	if spec.Topic != "" {
		t.CloudPubsubSink = &chronicle.CloudPubSubSink{Topic: spec.Topic}
	}
	return t
}

// dataTapObject builds the engine object (canonical diff basis + identity).
func dataTapObject(t chronicle.DataTap) (reconcile.Object, error) {
	display := t.DisplayName
	if display == "" {
		display = t.ID()
	}
	topic := ""
	if t.CloudPubsubSink != nil {
		topic = t.CloudPubsubSink.Topic
	}
	canon, err := canonicalDataTap(dataTapSpec{
		DisplayName:         display,
		Filter:              string(t.Filter),
		SerializationFormat: dataTapSerialization(string(t.SerializationFormat)),
		Topic:               topic,
	})
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: t.Name, Canonical: canon}, nil
}

func loadDataTaps(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		var meta dataTapMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalDataTap(dataTapSpec{
			DisplayName:         meta.DisplayName,
			Filter:              meta.Filter,
			SerializationFormat: dataTapSerialization(meta.SerializationFormat),
			Topic:               meta.Topic,
		})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".yaml"),
			ServerID:  meta.Name,
			Canonical: canon,
		})
	}
	return objs, nil
}

// writeDataTapObject renders one object back to `<slug>.yaml`.
func writeDataTapObject(dir string, o reconcile.Object) error {
	spec, err := decodeDataTapSpec(o.Canonical)
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), dataTapMeta{
		DisplayName:         spec.DisplayName,
		Name:                o.ServerID,
		Filter:              spec.Filter,
		SerializationFormat: spec.SerializationFormat,
		Topic:               spec.Topic,
	})
}

func canonicalDataTap(spec dataTapSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeDataTapSpec(canonical []byte) (dataTapSpec, error) {
	var spec dataTapSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}
