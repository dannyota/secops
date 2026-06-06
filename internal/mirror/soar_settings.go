package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"danny.vn/secops/soar/legacy"
)

// SOAR case-routing policies are SINGLETONS (one record, get + upsert, no
// id/list/delete), so they do not fit the per-object reconcile engine. They are
// operated as imperative get/set verbs here. A get is read-only; a set is
// live-guarded (dry-run default).

// rawGetter / rawSetter are the singleton settings get/set shapes (legacy SDK).
type (
	rawGetter = func(context.Context) (legacy.RawJSON, error)
	rawSetter = func(context.Context, any) (legacy.RawJSON, error)
)

// PrintSOARSettingSingleton fetches a singleton settings object and prints it as
// pretty JSON. Read-only.
func PrintSOARSettingSingleton(ctx context.Context, label string, get rawGetter, w io.Writer) error {
	raw, err := get(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%s:\n", label)
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		buf.WriteByte('\n')
		_, err = w.Write(buf.Bytes())
		return err
	}
	_, err = w.Write(append([]byte(raw), '\n'))
	return err
}

// PushSOARSettingPolicy upserts a singleton case-routing policy: a single integer
// enum field. Guarded — dry-run previews; a real change needs assumeYes.
func PushSOARSettingPolicy(ctx context.Context, label, field string, value int, set rawSetter, dryRun, assumeYes bool, w io.Writer) error {
	liveBanner(w, "SET "+label)
	fmt.Fprintf(w, "%s -> %s=%d\n\n", label, field, value)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to change a routing policy without confirmation (pass --yes). Aborted.")
		return nil
	}
	if _, err := set(ctx, map[string]any{field: value}); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done. %s set to %d.\n", label, value)
	return nil
}
