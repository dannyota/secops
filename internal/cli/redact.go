package cli

import "danny.vn/secops/internal/mirror"

// applyValueRedaction installs the process-wide value redactor from the data-root
// `.secopsctl-redact` patterns file plus any ad-hoc `--redact` patterns, so the
// pull/drift/push that follow mask matching inline-secret values (e.g. a webhook
// URL carrying a token in a playbook step parameter) consistently.
//
// Consistency matters: pull, drift, and push all load the SAME committed
// `.secopsctl-redact` from the same data root, so a value masked on pull is also
// masked when drift/push canonicalize the live object — it never produces a
// phantom diff. The `--redact` flag is a per-invocation override (not committed),
// so it is not drift-safe on its own; put a durable rule in `.secopsctl-redact`.
func applyValueRedaction(root string, extra []string) error {
	filePatterns, err := mirror.LoadRedactPatternsFile(root)
	if err != nil {
		return err
	}
	r, err := mirror.NewValueRedactor(append(filePatterns, extra...))
	if err != nil {
		return err
	}
	mirror.SetValueRedactor(r)
	return nil
}
