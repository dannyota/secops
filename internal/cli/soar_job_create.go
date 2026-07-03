package cli

// soar_job_create.go — create a scheduled job instance (flag-based modern +
// file-based legacy escape hatch).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// newSOARJobInstanceCreateCmd creates a scheduled job instance. Two modes:
//   - --file <path>: raw JSON body sent via the legacy API (escape hatch).
//   - Flag-based: --integration + --job + --display-name + schedule flags,
//     resolved and sent via the modern v1alpha API.
func newSOARJobInstanceCreateCmd() *cobra.Command {
	var (
		file        string
		integration string
		job         string
		displayName string
		interval    int
		advanced    bool
		schedType   string
		timezone    string
		startDate   string
		startTime   string
		days        []string
		dayOfMonth  int
		schedInt    int
		params      []string
		enableFlag  bool
		disableFlag bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "create (--file <instance.json> | --integration I --job J --display-name N ...)",
		Short: "GUARDED: create a scheduled job instance",
		Long: "Create a scheduled job instance. Two modes:\n\n" +
			"  --file <path>    Raw JSON body sent via the legacy API (escape hatch).\n" +
			"  --integration + --job + --display-name + schedule flags: resolved and\n" +
			"  sent via the modern v1alpha API.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file != "" {
				return createJobInstanceFromFile(file, dryRun, yes)
			}
			if integration == "" || job == "" || displayName == "" {
				return fmt.Errorf("provide --file for raw JSON or --integration + --job + --display-name for flag-based create")
			}
			return createJobInstanceFromFlags(createJobInstanceOpts{
				integration: integration,
				job:         job,
				displayName: displayName,
				interval:    interval,
				advanced:    advanced,
				schedType:   schedType,
				timezone:    timezone,
				startDate:   startDate,
				startTime:   startTime,
				days:        days,
				dayOfMonth:  dayOfMonth,
				schedInt:    schedInt,
				params:      params,
				enable:      enableFlag,
				disable:     disableFlag,
				dryRun:      dryRun,
				yes:         yes,
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "raw JSON body file (legacy escape hatch)")
	f.StringVar(&integration, "integration", "", "integration identifier (flag-based create)")
	f.StringVar(&job, "job", "", "job name or numeric id (flag-based create)")
	f.StringVar(&displayName, "display-name", "", "display name for the new instance")
	f.IntVar(&interval, "interval", 0, "run interval in seconds (min 60, simple schedule)")
	f.BoolVar(&advanced, "advanced", false, "use advanced (calendar-based) scheduling")
	f.StringVar(&schedType, "schedule-type", "", "advanced schedule type: ONCE|DAILY|WEEKLY|MONTHLY")
	f.StringVar(&timezone, "timezone", "", "advanced schedule timezone (e.g. UTC, America/New_York)")
	f.StringVar(&startDate, "start-date", "", "schedule start date YYYY-MM-DD")
	f.StringVar(&startTime, "time", "", "schedule time HH:MM")
	f.StringSliceVar(&days, "days", nil, "weekly schedule: days (MONDAY,TUESDAY,...)")
	f.IntVar(&dayOfMonth, "day-of-month", 0, "monthly schedule: day of month (1-31)")
	f.IntVar(&schedInt, "schedule-interval", 1, "schedule recurrence interval (every N days/weeks/months)")
	f.StringArrayVar(&params, "param", nil, "parameter KEY=VALUE (repeatable)")
	f.BoolVar(&enableFlag, "enable", false, "enable the instance on creation (default)")
	f.BoolVar(&disableFlag, "disable", false, "create the instance in disabled state")
	cmd.MarkFlagsMutuallyExclusive("enable", "disable")
	guardRunFlags(cmd, &dryRun, &yes)
	return markJSON(cmd)
}

// createJobInstanceFromFile sends a raw JSON file via the legacy API.
func createJobInstanceFromFile(file string, dryRun, yes bool) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	body := json.RawMessage(bytes.TrimSpace(data))
	return caseAction(fmt.Sprintf("create job instance from %s", file), body, dryRun, yes,
		func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
			return lc.CreateJobInstance(ctx, body)
		})
}

type createJobInstanceOpts struct {
	integration string
	job         string
	displayName string
	interval    int
	advanced    bool
	schedType   string
	timezone    string
	startDate   string
	startTime   string
	days        []string
	dayOfMonth  int
	schedInt    int
	params      []string
	enable      bool
	disable     bool
	dryRun      bool
	yes         bool
}

// createJobInstanceFromFlags resolves the job definition, builds a typed body
// from the provided flags, and creates the instance via the modern API.
func createJobInstanceFromFlags(o createJobInstanceOpts) error {
	sc, err := newSOARClient()
	if err != nil {
		return err
	}
	ctx := baseContext()

	// Resolve the job definition to validate the job exists and get its id.
	jobs, err := sc.ListJobs(ctx, o.integration)
	if err != nil {
		return fmt.Errorf("list jobs for %s: %w", o.integration, err)
	}
	jobDef, err := resolveJobDef(jobs, o.job)
	if err != nil {
		return err
	}

	// Build the instance body.
	enabled := !o.disable
	body := soar.JobInstance{
		DisplayName:     o.displayName,
		Enabled:         enabled,
		IntervalSeconds: o.interval,
	}

	if !o.advanced && o.interval > 0 && o.interval < 60 {
		return fmt.Errorf("--interval must be at least 60 seconds, got %d", o.interval)
	}

	if o.advanced {
		body.Advanced = true
		ac, err := buildAdvancedConfig(o)
		if err != nil {
			return err
		}
		body.AdvancedConfig = ac
	}

	// Resolve parameters against the job definition's parameter spec.
	if len(o.params) > 0 {
		defParams, err := fetchJobDefParams(sc, ctx, o.integration, jobDef.PathID())
		if err != nil {
			return err
		}
		resolved, err := resolveParams(o.params, defParams)
		if err != nil {
			return err
		}
		body.Parameters = resolved
	}

	action := fmt.Sprintf("create job instance %q for %s/%s", o.displayName, o.integration, jobDef.DisplayName)
	dr, ay := soarGuard(action, o.dryRun, o.yes)

	if !jsonOut {
		fmt.Fprintf(os.Stdout, "Creating job instance %q\n", o.displayName)
		fmt.Fprintf(os.Stdout, "Integration: %s  Job: %s (id=%s)\n", o.integration, jobDef.DisplayName, jobDef.ID.String())
		fmt.Fprintf(os.Stdout, "Enabled: %v\n", enabled)
		if o.advanced {
			fmt.Fprintf(os.Stdout, "Schedule: advanced (%s)\n", o.schedType)
		} else if o.interval > 0 {
			fmt.Fprintf(os.Stdout, "Interval: %ds\n", o.interval)
		}
	}

	if dr {
		if !jsonOut {
			fmt.Fprintln(os.Stdout, "\nDRY RUN — no mutation sent. Re-run with --yes to apply.")
		}
		return emitGuardedResult(action, dr, false)
	}
	if !ay {
		if !jsonOut {
			fmt.Fprintln(os.Stdout, "\nRefusing to create without confirmation (pass --yes). Aborted.")
		}
		return emitGuardedResult(action, dr, false)
	}

	result, err := sc.CreateJobInstance(ctx, o.integration, jobDef.PathID(), body)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeRawJSON(os.Stdout, result.Raw)
	}
	fmt.Fprintf(os.Stdout, "Done. Job instance created: %s\n", result.Name)
	return nil
}

// resolveJobDef finds a job definition by displayName, numeric id, or identifier.
func resolveJobDef(jobs []soar.JobDef, selector string) (*soar.JobDef, error) {
	selector = strings.TrimSpace(selector)
	for i := range jobs {
		j := &jobs[i]
		if j.DisplayName == selector || j.ID.String() == selector || j.Identifier == selector {
			return j, nil
		}
	}
	names := make([]string, 0, len(jobs))
	for _, j := range jobs {
		names = append(names, j.DisplayName)
	}
	return nil, fmt.Errorf("no job matches %q; available: %s", selector, strings.Join(names, ", "))
}

// fetchJobDefParams fetches the full job definition to get its parameter spec.
func fetchJobDefParams(sc *soar.Client, ctx context.Context, integration, jobID string) ([]soar.JobInstanceParameter, error) {
	def, err := sc.GetJobDef(ctx, integration, jobID)
	if err != nil {
		return nil, fmt.Errorf("get job definition: %w", err)
	}
	// The job definition's raw payload carries the parameter template; parse it
	// to extract the parameter spec for validation.
	var defShape struct {
		Parameters []soar.JobInstanceParameter `json:"parameters"`
	}
	if err := json.Unmarshal(def.Raw, &defShape); err != nil {
		return nil, fmt.Errorf("decode job def parameters: %w", err)
	}
	return defShape.Parameters, nil
}

// resolveParams maps KEY=VALUE pairs against the job definition's parameter
// spec by displayName, returning typed parameters.
func resolveParams(kvs []string, defParams []soar.JobInstanceParameter) ([]soar.JobInstanceParameter, error) {
	byName := make(map[string]soar.JobInstanceParameter, len(defParams))
	for _, p := range defParams {
		byName[strings.ToLower(p.DisplayName)] = p
	}

	result := make([]soar.JobInstanceParameter, 0, len(kvs))
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("--param %q: expected KEY=VALUE", kv)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)

		dp, found := byName[strings.ToLower(k)]
		if !found {
			avail := make([]string, 0, len(defParams))
			for _, p := range defParams {
				avail = append(avail, p.DisplayName)
			}
			return nil, fmt.Errorf("--param %q: unknown parameter; available: %s", k, strings.Join(avail, ", "))
		}
		dp.Value = v
		result = append(result, dp)
	}
	return result, nil
}

// buildAdvancedConfig builds an AdvancedConfig from the flag-based options.
func buildAdvancedConfig(o createJobInstanceOpts) (*soar.AdvancedConfig, error) {
	st := soar.ScheduleType(strings.ToUpper(o.schedType))
	switch st {
	case soar.ScheduleTypeOnce, soar.ScheduleTypeDaily, soar.ScheduleTypeWeekly, soar.ScheduleTypeMonthly:
	default:
		return nil, fmt.Errorf("--schedule-type must be ONCE, DAILY, WEEKLY, or MONTHLY, got %q", o.schedType)
	}

	date, err := parseScheduleDate(o.startDate)
	if err != nil {
		return nil, err
	}
	tod, err := parseTimeOfDay(o.startTime)
	if err != nil {
		return nil, err
	}

	ac := &soar.AdvancedConfig{
		TimeZone:     o.timezone,
		ScheduleType: st,
	}
	switch st {
	case soar.ScheduleTypeOnce:
		ac.OneTimeSchedule = &soar.OneTimeSchedule{Date: date, Time: tod}
	case soar.ScheduleTypeDaily:
		ac.DailySchedule = &soar.DailySchedule{Date: date, Time: tod, Interval: o.schedInt}
	case soar.ScheduleTypeWeekly:
		ac.WeeklySchedule = &soar.WeeklySchedule{Date: date, Time: tod, Days: o.days, Interval: o.schedInt}
	case soar.ScheduleTypeMonthly:
		ac.MonthlySchedule = &soar.MonthlySchedule{Date: date, Time: tod, Day: o.dayOfMonth, Interval: o.schedInt}
	}
	return ac, nil
}

// parseScheduleDate parses "YYYY-MM-DD" into a ScheduleDate.
func parseScheduleDate(s string) (soar.ScheduleDate, error) {
	if s == "" {
		return soar.ScheduleDate{}, nil
	}
	var y, m, d int
	if _, err := fmt.Sscanf(s, "%d-%d-%d", &y, &m, &d); err != nil {
		return soar.ScheduleDate{}, fmt.Errorf("--start-date: expected YYYY-MM-DD, got %q", s)
	}
	return soar.ScheduleDate{Year: y, Month: m, Day: d}, nil
}

// parseTimeOfDay parses "HH:MM" into a TimeOfDay.
func parseTimeOfDay(s string) (soar.TimeOfDay, error) {
	if s == "" {
		return soar.TimeOfDay{}, nil
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return soar.TimeOfDay{}, fmt.Errorf("--time: expected HH:MM, got %q", s)
	}
	return soar.TimeOfDay{Hours: h, Minutes: m}, nil
}
