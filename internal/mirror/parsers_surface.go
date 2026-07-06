package mirror

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// activeParser returns the ACTIVE parser to use as the canonical version.
// When multiple parsers are active (e.g. a PREBUILT and a CUSTOM), the CUSTOM
// version is preferred — it supersedes the prebuilt one.
func activeParser(parsers []chronicle.Parser) *chronicle.Parser {
	var first *chronicle.Parser
	for i := range parsers {
		if parsers[i].State != "ACTIVE" {
			continue
		}
		if parsers[i].Type == "CUSTOM" {
			return &parsers[i]
		}
		if first == nil {
			first = &parsers[i]
		}
	}
	return first
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
//
// The activate must WAIT for the server's async validation of the fresh
// version: activating immediately after create fails with a bare
// FAILED_PRECONDITION even when the validation is about to pass.
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
	if err := waitParserValidated(ctx, c, spec.LogType, id); err != nil {
		return reconcile.Object{}, err
	}
	if err := retryActivateParser(ctx, c, spec.LogType, id); err != nil {
		return reconcile.Object{}, err
	}
	active, err := c.GetParser(ctx, spec.LogType, id)
	if err != nil {
		return reconcile.Object{}, err
	}
	return parserLiveObject(spec.LogType, *active)
}

// retryActivateParser retries activation on FAILED_PRECONDITION (HTTP 400).
// Even after validation passes, the server may not accept the activate call
// immediately — especially for log types with an existing prebuilt parser.
// A brief retry bridges the gap that manual `parsers activate` crosses naturally.
func retryActivateParser(ctx context.Context, c *chronicle.Client, logType, id string) error {
	const maxAttempts = 4
	wait := 3 * time.Second
	for attempt := range maxAttempts {
		err := c.ActivateParser(ctx, logType, id)
		if err == nil {
			return nil
		}
		var apiErr *chronicle.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 400 || attempt == maxAttempts-1 {
			return err
		}
		fmt.Fprintf(os.Stderr, "  (retry) activate %s: %v — retrying in %s\n", id, err, wait)
		time.Sleep(wait)
		wait = min(wait*2, 15*time.Second)
	}
	return nil // unreachable
}

// parserValidateTimeout bounds the wait for a fresh parser version's async
// server-side validation before activating it.
const parserValidateTimeout = 5 * time.Minute

// waitParserValidated polls a fresh parser version until its validation
// settles. On FAILED / INTERNAL_ERROR it reports the parsing errors; on
// PASSED / VALIDATION_SKIPPED it returns success; on timeout it says how to
// finish by hand (the version is created, only the activation is pending).
func waitParserValidated(ctx context.Context, c *chronicle.Client, logType, id string) error {
	deadline := time.Now().Add(parserValidateTimeout)
	wait := 2 * time.Second
	for {
		p, err := c.GetParser(ctx, logType, id)
		if err != nil {
			return err
		}
		switch stage := strings.ToUpper(p.ValidationStage); {
		case strings.Contains(stage, "PASSED"), strings.Contains(stage, "SKIPPED"):
			return nil
		case strings.Contains(stage, "FAILED"),
			strings.Contains(stage, "INTERNAL_ERROR"),
			strings.Contains(stage, "DELETE_CANDIDATE"):
			msg := fmt.Sprintf("parser %s validation %s", id, p.ValidationStage)
			if p.ValidationReport != "" {
				if errs, lerr := c.ListParsingErrors(ctx, p.ValidationReport, 5); lerr == nil && len(errs) > 0 {
					msg += ": " + string(errs[0].Error)
				}
			}
			return fmt.Errorf("%s", msg)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("parser %s created but validation did not settle within %s (stage %q) — activate it once validated: `ingest parsers activate %s %s --yes`",
				id, parserValidateTimeout, p.ValidationStage, logType, id)
		}
		time.Sleep(wait)
		wait = min(wait*2, 30*time.Second)
	}
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
