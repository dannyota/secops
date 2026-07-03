# 13 · Scheduled jobs

SOAR **jobs** are scheduled Python scripts that run on a timer inside the
platform. They range from built-in system housekeeping (sync, monitoring,
collection) to custom automation you write yourself.

The resource hierarchy:

```text
integration
  └── job (definition — the Python script + metadata)
        └── jobInstance (the scheduled config — interval, params, enabled)
              └── logs (per-run execution history)
```

An **integration** ships one or more **job definitions** (the code). A **job
instance** is a configured run of that definition — the schedule, the parameter
values, and the enabled/disabled toggle. Each run produces a **log entry** with
start/end times, status (`SUCCESS`/`ERROR`), and the script's stdout.

## Listing instances

```bash
# All instances across all integrations (modern API, wildcard path)
secopsctl soar jobs instance list

# With integration/status filter
secopsctl soar jobs instance list --grep GoogleChronicle

# Full JSON (scripts, parameters, timestamps)
secopsctl soar jobs instance list --json
```

The list command uses the modern v1alpha API by default and falls back to the
legacy AppKey path on error. Pass `--legacy` to force the legacy path.

## Inspecting an instance

```bash
# Key-value summary (omits script body)
secopsctl soar jobs instance get --instance "Google Chronicle Sync Job"

# Full JSON including script
secopsctl soar jobs instance get --instance "Google Chronicle Sync Job" --json
```

The `--instance` selector matches by display name, numeric id, or
`uniqueIdentifier`.

## Run history

```bash
# Recent runs (table: START END STATUS MESSAGE)
secopsctl soar jobs instance history --instance "Cases Collector DB_PublisherID_1"

# Filter to failures only
secopsctl soar jobs instance history --instance 7 --status ERROR

# Paginate
secopsctl soar jobs instance history --instance 7 --page-size 50

# Full log JSON
secopsctl soar jobs instance history --instance 7 --json
```

This is the per-instance execution log (the "History" tab in the console). For
the CloudLogging-based Python logs, use `soar jobs logs` instead.

## Creating a job instance

Two modes: flag-based (preferred) or raw JSON escape hatch.

### Flag-based create

```bash
# Simple interval schedule (every 5 minutes, disabled)
secopsctl soar jobs instance create \
  --integration MyIntegration \
  --job "Custom Sync" \
  --display-name "Custom Sync - Hourly" \
  --interval 300 \
  --param "API Key=your-api-key" \
  --param "Environment=Default Environment" \
  --disable \
  --yes

# Advanced weekly schedule
secopsctl soar jobs instance create \
  --integration MyIntegration \
  --job "Weekly Report" \
  --display-name "Weekly Report - Mon/Fri" \
  --advanced \
  --schedule-type weekly \
  --timezone "Asia/Ho_Chi_Minh" \
  --start-date 2026-07-07 \
  --time 09:00 \
  --days MONDAY,FRIDAY \
  --yes
```

The `--job` selector resolves against the integration's job definitions by name
or numeric id. When `--param` is given, the key is matched by `displayName`
against the job definition's parameter spec; mandatory parameters are checked
before submission.

Dry-run (the default) prints the constructed JSON body without sending it.

### Raw JSON escape hatch

```bash
secopsctl soar jobs instance create --file instance.json --yes
```

Copy an existing instance from `instance list --json`, edit, and pipe it back.

## Enabling / disabling

```bash
secopsctl soar jobs instance set --instance 7 --disable --yes
secopsctl soar jobs instance set --instance 7 --enable --yes
```

Set warns when targeting a `custom:false` (auto-provisioned) instance.

## Running on demand

```bash
secopsctl soar jobs instance run --instance "Google Chronicle Sync Job" --yes
```

## Deleting an instance

```bash
secopsctl soar jobs instance delete --instance 7 --yes
```

Delete warns when targeting a `custom:false` instance.

## Job definition revisions

Revisions snapshot a job definition before edits, enabling rollback.

```bash
# List revisions for a job definition
secopsctl soar jobs revision list --integration MyIntegration --job 42

# Create a revision (snapshot current state) before editing
secopsctl soar jobs revision create \
  --integration MyIntegration \
  --job 42 \
  --comment "before parameter refactor" \
  --yes

# Rollback to a revision (DANGEROUS on stock jobs)
secopsctl soar jobs revision rollback \
  --integration MyIntegration \
  --job 42 \
  --revision rev-id \
  --yes

# Delete a revision
secopsctl soar jobs revision delete \
  --integration MyIntegration \
  --job 42 \
  --revision rev-id \
  --yes
```

## Typical workflow

1. **Pull current state** to inspect what is configured:

   ```bash
   secopsctl soar pull jobs --out samples
   ```

2. **List and inspect** the running instances:

   ```bash
   secopsctl soar jobs instance list
   secopsctl soar jobs instance get --instance "Sync Job"
   ```

3. **Check recent run history** for errors:

   ```bash
   secopsctl soar jobs instance history --instance "Sync Job" --status ERROR
   ```

4. **Create a revision** before making changes to a job definition:

   ```bash
   secopsctl soar jobs revision create \
     --integration MyIntegration --job 42 \
     --comment "pre-edit snapshot" --yes
   ```

5. **Create a new instance** for a custom job:

   ```bash
   secopsctl soar jobs instance create \
     --integration MyIntegration --job "Custom Sync" \
     --display-name "Custom Sync - 5min" \
     --interval 300 --enable --yes
   ```

6. **Verify** it is running:

   ```bash
   secopsctl soar jobs instance run --instance "Custom Sync - 5min" --yes
   secopsctl soar jobs instance history --instance "Custom Sync - 5min"
   ```
