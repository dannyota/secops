# TODO — secopsctl backlog

Improvement backlog for the tool and SDK. Doc-only gaps are fixed inline as they
surface; this file tracks **code/behavior** work — UX friction and new features —
found by using the tool against its own help and docs.

Priorities: **P1** = correctness/safety or a headline use case that doesn't hold ·
**P2** = real friction with a clear fix · **P3** = polish / nice-to-have.

## CLI-wide & automation

- [x] **P1 — `--json` on `doctor`, `drift`, `push` (W26).** Implemented: doctor
  `{ok, version, checks[]}`, drift per-surface report + `drifted_surfaces`, push
  the reconcile plan/result + `would_change`. `pull --json` is **deferred** — its
  pullers write progress straight to stdout, so clean JSON needs a puller-output
  refactor (its real output is the files it writes; review with `git diff`).
- [x] **P1 — Exit codes all collapse to `1`.** `Execute()` returns 1 for every
  error, so "drift detected" can't be told from "auth expired", "indeterminate",
  or "bad flag". Give `drift` (and dry-run `push`) a detailed-exit-code scheme like
  `terraform plan -detailed-exitcode` / `git diff --exit-code` (e.g. 0 = in sync,
  2 = drift / would-change, 3 = indeterminate/retryable, 1 = error) and document it.
- [x] **P1 — Unknown subcommand exits `0`.** A typo'd subcommand under any group
  (`soar bogus`, `query bogus`, `soar case bogus`, …) prints help and exits 0,
  while an unknown top-level command exits 1. A scripted caller treats the no-op as
  success. Make parent groups error (non-zero) on an unknown/missing subcommand.
- [x] **P2 — Machine-readable results on writes (W26).** `push --json` emits the
  reconcile plan/result (creates/updates/deletes per surface, `would_change`); the
  `soar case` verbs emit `{action, dry_run, applied, ok}`. The engine now reports
  the plan in dry-run too, so the preview is machine-readable.
- [x] **P2 — `secopsctl surfaces [--json]` capability view.** Print, per surface,
  whether it supports pull / push / prune and whether it's read-only — sourced from
  the surface-family registry — so "what's reconcilable vs read-only" is answerable
  without opening `design/catalog.md`.
- [x] **P3 — Global `--non-interactive`.** Prompts are TTY-guarded today, but a
  harness that attaches a PTY has no single switch to guarantee no prompt. Add a
  global `--non-interactive` (or honor an env) that forces the push confirmation off.
- [ ] **P3 — Standardize `--json` help.** Some subcommands redeclare a local
  `--json` with a different description; pick one canonical description (or rely on
  the single global flag) so `--help` introspection is consistent.

## Config & safety

- [x] **P1 — Explicit `--config` / `$SECOPSCTL_CONFIG` to a missing path silently
  falls through to discovery.** A typo'd path loads the *default* config (exit 0) —
  a wrong-tenant footgun given every push is a live deploy. Fail loudly when an
  explicitly-named config path does not exist; reserve fall-through for the
  no-explicit-path case.
- [x] **P1 — Show the resolved config source.** Add `config --show-path` (or a
  `source: <path>` line to `info`) so an operator can confirm which file won —
  i.e. which tenant they're pointed at — before any deploy.
- [ ] **P2 — `doctor` remediation + `--json`.** On a failed check, append a next
  step (auth → `gcloud auth application-default login` / `SECOPS_ACCESS_TOKEN`;
  SOAR → check AppKey / `soar_url`) instead of a bare API error. Add `doctor --json`
  (`{config,auth,siem,soar}` + a meaningful exit code) for CI/onboarding scripts.

## SIEM — rules, query, curated, forwarders

- [x] **P1 — `forwarders` has no `pull` target.** It is a `push`/`drift` target but
  not a `pull` one, so the documented `pull all` → `drift` CI loop reports
  forwarders as permanently drifted (prune-eligible → live-only = delete). Add
  `pull forwarders` for pull/drift/push symmetry (or drop it from `drift`).
- [x] **P2 — `rules list [--json]`.** No way to map a rule's display name / slug to
  the `rule_id` the inspect verbs (`rules detections/errors/alerts`) require without
  opening companion files. Add a read-only `rules list` (displayName · slug ·
  rule_id · enabled) and let the inspect verbs accept a slug/name.
- [ ] **P2 — Per-rule targeting for `push rules-deploy` / `rules-disable`.** Both
  sweep *every* tracked rule whose state differs; add `--rule <slug-or-id>` to scope
  a deploy/disable to one rule.
- [x] **P2 — `drift` / `pull` plane selector (`--siem` / `--soar`).** No-arg `drift`
  spans both planes and needs both credentials; a single-plane CI runner must
  enumerate every surface name by hand. Add a plane flag.
- [ ] **P2 — Indicator/entity enrichment pivot.** Expose the deferred
  `:fetchRelated` / `:fetchIocMatchMetadata` RPCs as `iocs related` / `ti related`
  so a hunt can pivot indicator → campaign/report instead of three disconnected
  lookups.
- [x] **P3 — `iocs find --from-file` / stdin.** Bulk indicator lookup from a file or
  stdin (one per line); document the 1000-per-request chunking/cap.
- [ ] **P3 — Wire `entity summarize` / NL search into the CLI.** The SDK already has
  them (`chronicle/nl_search.go`, `log_search.go`) and the design docs reference
  them, but they're not reachable from the CLI.
- [x] **P3 — `rules validate <file.yaral>`.** Standalone pre-push YARA-L validation
  (today validation only happens inside `push rules-update`).
- [ ] **P3 — Reconcilable curated deployments (`push curated`).** The deployment
  state is already pulled to a file and the SDK has a batch-update primitive; close
  the loop so curated enable/alerting is detection-as-code like every other surface.

## SOAR — cases, integration

- [ ] **P1 — `soar users list` (read-only directory).** `soar case assign --user`
  needs a userId with no in-tool way to discover one (`get` shows the assignee's
  *name*, not the id). Add a user/assignee directory and reference it from
  `assign --help`.
- [ ] **P2 — Modern `soar case list` output parity.** The default (modern) lane
  prints a header-less table without TITLE/ASSIGNEE and with raw priority strings;
  the legacy fallback prints a full labelled table. Bring the modern path to parity.
- [ ] **P2 — Typed close-reason for single-case `close`.** Single `close --reason`
  is free text while `push bulk-close --reason` is a fixed enum, so the same action
  uses two vocabularies and metrics don't aggregate. Offer the typed enum on single
  close (free text as an optional comment).
- [ ] **P3 — `soar case` flag consistency.** `get` takes the id positionally while
  every mutating verb takes `--id`; accept both forms on both for muscle-memory.
- [ ] **P3 — Value discovery for `--tag` / `--stage` / `--root-cause`.** Wire shell
  completion or a `--list-values` helper sourced from `soar pull case-tags` /
  `close-root-causes`, and add discovery pointers to the flag help.

## SDK

- [ ] **P1 — Export `soar.Error` / `soar.IsNotFound` (and `soar/legacy`).** The
  transport already builds a structured `*transport.Error{Status,URL,Body,…}` but it
  is sealed in an internal package, so SOAR consumers must string-parse `err.Error()`.
  Alias it from the public package (as `Settings` already is) and add an `IsNotFound`
  helper, mirroring `chronicle.APIError` / `chronicle.IsNotFound`.
- [ ] **P3 — Minimal typed SOAR case view.** `soar.ListCases` returns
  `[]json.RawMessage`; a small typed view (id · displayName · status) — the shape the
  CLI already parses — would save every consumer from defining their own structs.

## Help-text polish

- [x] **P3 — `curated list --json` help renders as `--json curated set`.** Backticks
  in the flag-description string are taken by cobra as the flag's value-name
  placeholder. Drop backticks from flag-description strings repo-wide.
- [ ] **P3 — Per-target help for `push rules-*`.** The four `rules-create/update/
  deploy/disable` are positional args, so `push rules-update --help` shows the
  generic push help. Make them subcommands or print a target-specific paragraph.
- [ ] **P3 — Per-target help for *all* reconcile targets.** Same positional-arg root
  cause as the rules item above, but it extends to every `pull`/`push`/`drift`
  target (dashboards, feeds, parsers, …): `push dashboards --help` prints the
  generic parent help, so surface-specific behavior (CUSTOM-only, access immutable,
  charts replaced wholesale, NoEtag, parser create-and-activate, non-prune-eligible)
  is invisible at the point of use and lives only in a tip doc.

## Safety footguns (found in round 2)

- [x] **P1 — `push` has no `--out` / data-root flag.** `pull` and `drift` take
  `--out` (data root, default cwd) but `push` always resolves the data root to the
  current directory. `pull dashboards --out /data` writes `/data/dashboards/`, yet
  `push dashboards` run from elsewhere silently reads `./dashboards/` — and with
  `--prune` from an empty/wrong directory that is a path to deleting live objects.
  Give `push` the same `--out` as `pull`/`drift`, and/or refuse a live push when the
  resolved data root has no files for the target.
- [ ] **P2 — `soar push playbook` vs `soar push playbooks` are one character apart
  with different blast radius.** Singular = imperative whole-body save of one
  playbook (mints a version); plural = directory reconcile with `--prune`. A
  mistype runs a different live operation. Rename/disambiguate (e.g. `playbook save`)
  or make each verb's help spell out the contrast.
- [ ] **P2 — `--prune` help is uniform on NoDelete surfaces.** Every reconcile push
  subcommand advertises `--prune to delete`, but only 4 of 16 SOAR surfaces
  (webhooks · connectors · visual-families · networks) are prune-eligible; the other
  12 are NoDelete and silently skip prune. Make the per-command help capability-aware
  (drop/annotate `--prune` on NoDelete surfaces) and have dry-run say up front that
  `--prune` is a no-op for the target.

## SIEM — feeds, parsers, dashboards (round 2)

- [x] **P2 — Parser CLI: test / versions / rollback.** The SDK has `RunParser`
  (inert validate against sample logs), `ActivateParser`/`DeactivateParser`, and
  retains the prior version for rollback, but none are CLI-reachable. `push parsers`
  does create-new-version **and activate** in one step (immediate live cutover) with
  no way to dry-test the parse first or to re-activate a prior version. Add
  `parsers run <logtype> --logs <file>`, `parsers versions <logtype>`, and
  `parsers activate <logtype> <id>`; optionally a two-step `push parsers --inactive`
  then `parsers activate`.
- [x] **P3 — Feed-config schema discovery + pre-push validation in the CLI.** The SDK
  has `ListFeedSourceTypeSchemas` / `ListLogTypeSchemas` (the reference for authoring
  a feed file) but they are SDK-only, so an admin can't discover required fields
  before a `push feeds` 500s. Add `feeds schemas [--source-type X]` + pre-push validation.
- [x] **P3 — `DuplicateDashboard` CLI verb.** Dashboard `access` is immutable; the
  documented "recreate to change visibility" path uses the SDK's `DuplicateDashboard`
  (new access type), which has no CLI verb. Expose it.
- [ ] **P3 — Ship feed/parser file templates under `examples/`.** `examples/` has
  only UDM queries; authoring a new feed YAML or parser `.conf`/`.yaml` from scratch
  has no template or documented writable-field schema.

## SOAR — Content Hub / integrations (round 2)

- [ ] **P1 — `soar integration install` (and/or `soar marketplace install`).**
  `uninstall` exists but there is no `install`, and `integration create` needs an
  already-installed pack — so the browse → install → create-instance flow dead-ends
  at install with no CLI path (the SDK already has the install method). The lone
  `uninstall` implies a matching `install`.
- [ ] **P2 — `soar marketplace get` output.** It always dumps raw JSON with no
  human view and no `--json` of its own, the inverse of sibling `list`/`contentpacks`.
  Give it a readable summary by default (incl. the fields `create` needs) and honor
  `--json` for the raw body.
- [ ] **P3 — `soar marketplace contentpacks get <id>`.** `marketplace integrations`
  has `get` but content packs are list-only — no way to inspect one before install.

## SOAR — legacy escape hatch (round 2)

- [ ] **P1 — `soar legacy call --dry-run`.** Every other mutating path is
  dry-run-first, but the escape hatch jumps from `--write` straight to a
  banner + `--yes` live call with no way to preview the resolved method + URL + body
  first. Add `--dry-run` that prints (and validates) the composed request.
- [ ] **P2 — Op discovery for the escape hatch.** The feature depends on knowing an
  op path, but the only documented source (`third_party/siemplify-swagger.json`) is
  **git-ignored and not shipped**, so a clean-clone user has no way to discover ops.
  Bundle a tenant-neutral op index powering `soar legacy list [--grep]` + completion,
  and/or point at the SecOps UI Network tab.
- [ ] **P3 — `soar legacy call --out` writes `0644`.** Responses can carry sensitive
  operational data; write the dump `0600` (matching the config file's posture).
- [ ] **P3 — `--read`/`--write` flag semantics.** `--read` is silently ignored on
  GET and is not mutually exclusive with `--method PUT|DELETE` (an accepted
  contradiction). Reject the contradiction or document that the intent flags apply to
  POST only.

## Doc ↔ CLI consistency (round 2)

- [ ] **P3 — Extend the `surfaces` capability view to assert doc↔CLI consistency.**
  The catalog's "Function (CLI)" column lists SDK-only rows (enrichment_controls,
  governance, riskConfig, idp-mappings, analytics) that have no CLI verb. Have the
  proposed `secopsctl surfaces` (or a test, like the existing drift-guard) assert
  every advertised CLI entry maps to a real command and every surface's catalog
  section matches its registry plane, so docs can't drift from the binary uncaught.
