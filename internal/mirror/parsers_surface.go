package mirror

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// parsers as code, on the reconcile engine. Parsers are VERSIONED and IMMUTABLE:
// there is no server-side update. "Change the CBN" is modeled as create-new-
// version + activate (the server mints a fresh parser id each time), so the
// Update closure is a create+activate, not a PATCH. The on-disk layout reuses the
// exact `<logType>.conf` (decoded CBN source) + `<logType>.yaml` (metadata) that
// `pull parsers` already writes.
//
// Identity is the active parser's resource name (volatile across edits); the log
// type is the stable slug. One ACTIVE parser per log type. Not prune-eligible —
// deleting a log type's parser is high-blast — and the live set is derived from
// configured feeds, so a transient/derived gap never drives a deletion.

// parserSpec is the diff basis: the meaningful desired state of a parser is just
// its log type and its (decoded) CBN source. The volatile parser id, state,
// version, and timestamps are deliberately excluded so a re-created version does
// not phantom-diff.
type parserSpec struct {
	LogType string `json:"log_type"`
	CBN     string `json:"cbn"`
}

// parserRawModel is carried in Object.Raw for LIVE/echo objects so the pull
// writer can render the faithful `<logType>.yaml` metadata. Local objects carry
// none.
type parserRawModel struct {
	Parser chronicle.Parser `json:"parser"`
}

// parserMeta is the subset of `<logType>.yaml` LoadDir reads back: the server
// identity (and the log type, for clarity). The rest is informational.
type parserMeta struct {
	Name    string `yaml:"name"`
	LogType string `yaml:"log_type"`
}

func parsersSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "parsers",
		Dir:     DirParsers,
		Product: reconcile.ProductSIEM,
		// Immutable/versioned, no etag; delete exists for the Update old-version
		// path and the smoke, but prune stays off (high-blast, derived live set).
		Caps: reconcile.Capabilities{NoEtag: true},

		List:    parsersList(c),
		LoadDir: loadParsers,
		Write:   writeParserObject,
		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			return parserCreateActivate(ctx, c, local)
		},
		// Update is a create-new-version + activate (parsers are immutable). The
		// old version is left inactive so a rollback stays available; the active-
		// only List ignores it.
		Update: func(ctx context.Context, local, _ reconcile.Object) (reconcile.Object, error) {
			return parserCreateActivate(ctx, c, local)
		},
		Delete: func(ctx context.Context, live reconcile.Object) error {
			lt := live.Slug
			id := lastSegment(live.ServerID)
			_ = c.DeactivateParser(ctx, lt, id) // best-effort; force-delete handles an active parser too
			return c.DeleteParser(ctx, lt, id, true)
		},
	}
}

// parsersList reads the single ACTIVE parser per log type into engine objects.
// The log-type set is derived from configured feeds (as the puller does), so it
// can miss a parser on a feedless log type — a per-type read failure marks the
// listing incomplete (never mistaken for a deletion).
func parsersList(c *chronicle.Client) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		logTypes, err := logTypesInUse(ctx, c)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		for _, lt := range logTypes {
			parsers, perr := c.ListParsers(ctx, lt)
			if perr != nil {
				warnf("parsers: list %s: %v", lt, perr)
				res.Incomplete = true
				continue
			}
			active := activeParser(parsers)
			if active == nil {
				warnf("parsers: no ACTIVE parser for %s", lt)
				res.Incomplete = true
				continue
			}
			o, berr := parserLiveObject(lt, *active)
			if berr != nil {
				warnf("parsers: build %s: %v", lt, berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// activeParser returns the first ACTIVE parser, or nil.
func activeParser(parsers []chronicle.Parser) *chronicle.Parser {
	for i := range parsers {
		if parsers[i].State == "ACTIVE" {
			return &parsers[i]
		}
	}
	return nil
}

// parserLiveObject builds the engine object for a live active parser.
func parserLiveObject(logType string, p chronicle.Parser) (reconcile.Object, error) {
	cbn, err := decodeCBN(p.CBN)
	if err != nil {
		return reconcile.Object{}, err
	}
	canon, err := canonicalParser(parserSpec{LogType: logType, CBN: cbn})
	if err != nil {
		return reconcile.Object{}, err
	}
	raw, err := json.Marshal(parserRawModel{Parser: p})
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: logType, ServerID: p.Name, Canonical: canon, Raw: raw}, nil
}

// loadParsers reads every `<logType>.yaml` + sibling `<logType>.conf` into
// objects. A `.conf` file without a companion `.yaml` is treated as a new
// custom parser to create (the stem is the log type), matching the
// `push rules-create` pattern where only the source file is needed.
func loadParsers(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	yamlSeen := map[string]bool{}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		yamlSeen[stem] = true
		var meta parserMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		logType := meta.LogType
		if logType == "" {
			logType = stem
		}
		cbn, rerr := os.ReadFile(filepath.Join(dir, stem+".conf"))
		if rerr != nil {
			if os.IsNotExist(rerr) {
				cbn = nil
			} else {
				return nil, rerr
			}
		}
		canon, cerr := canonicalParser(parserSpec{LogType: logType, CBN: string(cbn)})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{Slug: stem, ServerID: meta.Name, Canonical: canon})
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".conf")
		if yamlSeen[stem] {
			continue
		}
		cbn, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalParser(parserSpec{LogType: stem, CBN: string(cbn)})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{Slug: stem, Canonical: canon})
	}
	return objs, nil
}

// writeParserObject renders a LIVE/echo object back to `<slug>.conf` (decoded CBN
// source) + `<slug>.yaml` (metadata). The CBN bytes written are byte-identical to
// what List canonicalizes, so a pulled parser pushes back in sync.
func writeParserObject(dir string, o reconcile.Object) error {
	if len(o.Raw) == 0 {
		return fmt.Errorf("parsers: cannot write %q without a live model", o.Slug)
	}
	var m parserRawModel
	if err := json.Unmarshal(o.Raw, &m); err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	cbn, err := decodeCBN(m.Parser.CBN)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, o.Slug+".conf"), []byte(cbn), 0o644); err != nil {
		return err
	}
	// Same metadata `pull parsers` writes, minus the pull-time inactive aggregate.
	meta := map[string]any{
		"log_type":           o.Slug,
		"parser_id":          lastSegment(m.Parser.Name),
		"name":               m.Parser.Name,
		"creator_source":     mapString(m.Parser.Creator, "source"),
		"state":              m.Parser.State,
		"type":               m.Parser.Type,
		"release_stage":      m.Parser.ReleaseStage,
		"create_time":        m.Parser.CreateTime,
		"version":            m.Parser.VersionInfo["version"],
		"rollback_available": m.Parser.VersionInfo["rollbackAvailable"],
		"cbn_bytes":          len(cbn),
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), meta)
}

// parserCreateActivate creates a new parser version from the local CBN and
// activates it (the load-bearing step — a created-but-inactive parser is not
// live), then re-reads it so the on-disk identity matches the new active version.
func parserCreateActivate(ctx context.Context, c *chronicle.Client, local reconcile.Object) (reconcile.Object, error) {
	spec, err := decodeParserSpec(local.Canonical)
	if err != nil {
		return reconcile.Object{}, err
	}
	created, err := c.CreateParser(ctx, spec.LogType, spec.CBN, true)
	if err != nil {
		return reconcile.Object{}, err
	}
	id := lastSegment(created.Name)
	if err := c.ActivateParser(ctx, spec.LogType, id); err != nil {
		return reconcile.Object{}, err
	}
	active, err := c.GetParser(ctx, spec.LogType, id)
	if err != nil {
		return reconcile.Object{}, err
	}
	return parserLiveObject(spec.LogType, *active)
}

// --- helpers ----------------------------------------------------------------

func canonicalParser(spec parserSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeParserSpec(canonical []byte) (parserSpec, error) {
	var spec parserSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}

// decodeCBN base64-decodes a parser's CBN; an empty string decodes to empty.
func decodeCBN(b64 string) (string, error) {
	if b64 == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
