# Gotestsum Correctness Metrics Design

## Goal

Add a `correctness-metrics` Go binary and a Makefile target for running tests via `gotestsum`, so that CI (Argo) can run LogQL correctness tests with structured output and push pass/fail/duration metrics to a Prometheus-compatible remote_write endpoint.

## Background

The remote correctness tests in `pkg/logql/bench/remote_test.go` compare query results between two live Loki endpoints. They run via `go test -tags remote_correctness` and produce standard Go test output. There is currently no structured result collection or metric emission.

Ad-hoc usage via `go test` and existing Makefile targets remains unchanged. This design only adds the CI-oriented workflow.

## Approach: Two-Phase Pipeline (Separate Argo Steps)

Two independent Argo workflow steps:

1. **Step 1 — Test execution:** `gotestsum` runs the tests, streams `standard-verbose` output to stdout, and writes a JUnit XML file.
2. **Step 2 — Metric push:** A Go binary (`correctness-metrics`) parses the JUnit XML and pushes metrics via Prometheus `remote_write`.

### Why two steps instead of one binary

- Clean separation of concerns — test execution is decoupled from metric emission.
- If metric push fails, test results are still available as Argo artifacts.
- The Go binary stays focused (parse + push) — easy to test independently.
- JUnit XML artifact can be consumed by Argo's test result UI.
- `gotestsum` handles test running, formatting, and JUnit generation — battle-tested.
- If we later want retry logic, `gotestsum` supports `--rerun-fails` natively.

### Why not an in-process test reporter

An alternative was to write a custom Go test reporter that hooks into `testing` directly and pushes metrics from `TestMain`. This was rejected because it mixes test logic with metric emission, is harder to maintain, and loses `gotestsum`'s other benefits (output formatting, retries, JUnit generation).

## Test Name Convention & Label Extraction

Test names currently follow the pattern from `remote_test.go:176` and `query_registry.go:204`:

```
TestRemoteStorageEquality/{suite}/{file}.yaml:{line}/kind={kind}
```

**This design requires a small change to `remote_test.go`** to add `direction` to the test name, producing:

```
TestRemoteStorageEquality/{suite}/{file}.yaml:{line}/kind={kind}/direction={direction}
```

This is a one-line change to the `t.Run` format string at `remote_test.go:176`:

```go
// Before:
t.Run(fmt.Sprintf("%s/kind=%s", tc.Source, tc.Kind()), func(t *testing.T) {

// After:
t.Run(fmt.Sprintf("%s/kind=%s/direction=%s", tc.Source, tc.Kind(), tc.Direction), func(t *testing.T) {
```

`tc.Direction` is `logproto.FORWARD` or `logproto.BACKWARD`, which stringifies to `FORWARD` or `BACKWARD`.

### Why direction is in the test name

Log queries default to `DirectionBoth` (`query_registry.go:207-209`), which produces two `TestCase` structs with identical `Source` fields — one for forward and one for backward. Without `direction` in the name, Go's `t.Run` deduplicates identical subtest names by appending `#00` / `#01` suffixes, which are opaque and require complex merge logic in the parser. By including `direction` in the name, each test is unique and no dedup logic is needed. Metric queries always use `direction=FORWARD`.

Examples:

```
TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD
TestRemoteStorageEquality/regression/agg.yaml:12/kind=log/direction=FORWARD
TestRemoteStorageEquality/regression/agg.yaml:12/kind=log/direction=BACKWARD
TestRemoteStorageEquality/exhaustive/filters.yaml:7/kind=metric/direction=FORWARD
```

### Label extraction — parsing algorithm

Given a JUnit `<testcase>` name like `TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=BACKWARD`:

1. Strip the root test prefix `TestRemoteStorageEquality/`.
2. Split on `/` to get segments: `["fast", "basic.yaml:3", "kind=log", "direction=BACKWARD"]`.
3. `suite` = segment[0] → `fast`.
4. `query_file` = segment[1], split on `:` to drop line number, then strip `.yaml` or `.yml` extension → `basic`.
5. `kind` = segment[2], parse `kind=` prefix → `log`.
6. `direction` = segment[3], parse `direction=` prefix → `BACKWARD`.

If any step fails (wrong segment count, missing `kind=` or `direction=` prefix, etc.), the test name is unparseable.

| Label | Parsed From | Example Values |
|-------|------------|----------------|
| `suite` | segment[0] | `fast`, `regression`, `exhaustive` |
| `query_file` | segment[1] (filename, no extension or line) | `basic`, `agg`, `filters` |
| `kind` | segment[2] `kind=` value | `metric`, `log`, `invalid` |
| `direction` | segment[3] `direction=` value | `FORWARD`, `BACKWARD` |
| `range_type` | CLI flag `--range-type` (static per run) | `instant`, `range` |
| `status` | JUnit XML test result (see mapping below) | `pass`, `fail`, `skip`, `error` |

### Status mapping from JUnit XML

| JUnit Element | `status` Label |
|--------------|----------------|
| No `<failure>`, `<error>`, or `<skipped>` | `pass` |
| `<failure>` (assertion failed) | `fail` |
| `<error>` (test panicked or unexpected error) | `error` |
| `<skipped>` | `skip` |

### Handling unparseable test names

The parsing logic must be robust — it must not panic on malformed test names with unexpected segment counts or formats. Tests whose names don't match the expected pattern are:

- **Counted** in the aggregate `logql_correctness_tests_total` metric with `suite="unknown"`. The `status` label still reflects the actual JUnit outcome (pass/fail/skip/error).
- **Not emitted** as `logql_correctness_test_duration_seconds` (the per-test metric is dropped for unparseable names).

The parent test `TestRemoteStorageEquality` itself (which appears in JUnit as a `<testsuite>`, not a `<testcase>`) is not counted as a test case.

## Metrics Emitted

All metrics are **gauges** (not counters) because each Argo run is a standalone batch job with no continuity between runs.

All metrics carry a static `job` label (default `logql-correctness`, configurable via `--job` flag).

Each push is a single remote_write request with all metrics timestamped at push time (end of the test run).

### Per-suite aggregate

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `logql_correctness_tests_total` | Gauge | `suite`, `range_type`, `status` | Total test count by suite, range type, and status |
| `logql_correctness_run_pass_ratio` | Gauge | `suite`, `range_type` | `pass / (pass + fail + error)` — skipped tests excluded from denominator. If denominator is zero (all tests skipped), omit this metric for that suite. |

### Per-test detail

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `logql_correctness_test_duration_seconds` | Gauge | `suite`, `query_file`, `kind`, `direction`, `range_type`, `status` | Per-test execution duration (only for parseable test names) |

### Run-level

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `logql_correctness_run_duration_seconds` | Gauge | `range_type` | Total elapsed time for the test run |

**Run duration source:** `gotestsum` emits JUnit XML with a `<testsuites>` root element containing one or more `<testsuite>` children. The run duration is the sum of all `<testsuite time="...">` attributes. If no `time` attribute is present, fall back to summing individual `<testcase time="...">` values.

## `correctness-metrics` CLI

Location: `pkg/logql/bench/cmd/correctness-metrics/main.go`

### Flags

| Flag | Env Var | Required | Default | Description |
|------|---------|----------|---------|-------------|
| `--input` | — | yes (always) | — | Path to JUnit XML file from gotestsum |
| `--remote-write-url` | `REMOTE_WRITE_URL` | yes (push mode only) | — | Prometheus remote_write endpoint |
| `--remote-write-username` | `REMOTE_WRITE_USERNAME` | no | — | Basic auth username |
| `--remote-write-password` | `REMOTE_WRITE_PASSWORD` | no | — | Basic auth password |
| `--range-type` | `RANGE_TYPE` | yes (always) | — | `instant` or `range` — static label on all metrics. Any other value is a fatal error. |
| `--job` | — | no | `logql-correctness` | Job label for all metrics |
| `--dry-run` | — | no | `false` | Parse and log metrics without pushing |

**Conditional requirements:**
- In `--dry-run` mode, only `--input` and `--range-type` are required. `--remote-write-url` and auth flags are ignored.
- In push mode (no `--dry-run`), `--remote-write-url` is additionally required.
- `--remote-write-username` and `--remote-write-password` must both be provided or both omitted. Providing only one is a fatal error.

### Behavior

1. Parse JUnit XML using `github.com/joshdk/go-junit`.
2. For each test case, extract labels from the test name using the parsing algorithm above.
3. Build Prometheus time series using `github.com/prometheus/prometheus` types (already in `go.mod`).
4. Push via remote_write using `github.com/prometheus/prometheus/storage/remote` (already in `go.mod`). Single attempt with a 30-second timeout; no retries.
5. Log a summary to stdout: `Pushed 47 metrics (32 pass, 12 fail, 3 skip) to https://...`

**`--dry-run` behavior:** Input parsing and validation still run fully. The only step skipped is the remote_write push (step 4). If the input file is missing or malformed, dry-run still exits non-zero. This ensures dry-run can be used to validate the pipeline locally.

### Exit codes

| Condition | Exit Code | Behavior |
|-----------|-----------|----------|
| Success (push completed or `--dry-run` with valid input) | `0` | Log summary |
| Input file missing | `1` | Log error, exit immediately |
| Input file empty or malformed XML | `1` | Log error describing the parse failure, exit immediately |
| `remote_write` failure (network, auth, 4xx/5xx) | `1` | Log the error from the Prometheus client, exit immediately |
| Invalid flag combination (e.g., username without password, invalid `--range-type` value) | `1` | Log usage error, exit immediately |

A non-zero exit from step 2 will fail the Argo workflow. This is intentional — if we ran tests but couldn't record the results, that's a CI failure worth investigating. Test results are still available as Argo artifacts for manual inspection.

### Build target

Add to `pkg/logql/bench/Makefile`:

```makefile
.PHONY: build-correctness-metrics
build-correctness-metrics:
	CGO_ENABLED=0 go build -o ./build/correctness-metrics ./cmd/correctness-metrics
```

The binary is built as part of the CI container image. The Dockerfile should include a build step that produces `correctness-metrics` and copies it into the runtime image. (Dockerfile specifics are outside the scope of this spec — they depend on the existing Argo image build pipeline.)

### Dependencies

- JUnit XML parsing: `github.com/joshdk/go-junit` (new dependency)
- Prometheus types and remote_write: `github.com/prometheus/prometheus` (already in `go.mod`, pseudo-version `v0.310.1-0.20260302160042-1751685dd4f6`)
- No other new dependencies expected

## Argo Workflow Integration

### Step 1: Run tests

```yaml
- name: run-correctness-tests
  script:
    image: <loki-test-image>  # needs go, gotestsum
    command: [gotestsum]
    args:
      - --format=standard-verbose
      - --junitfile=/output/results.xml
      - --
      - go
      - test
      - -tags=remote_correctness
      - -count=1
      - -v
      - -timeout=30m
      - ./pkg/logql/bench
      - -run=TestRemoteStorageEquality
      - --addr-1=$(REMOTE_ADDR_1)
      - --addr-2=$(REMOTE_ADDR_2)
      - --org-id=$(REMOTE_ORG_ID)
      - --metadata-dir=/data/metadata
      - --remote-range-type=$(RANGE_TYPE)
  outputs:
    artifacts:
      - name: junit-xml
        path: /output/results.xml
  continueOn:
    failed: true  # Step 2 must run even when tests fail
```

**Test path:** `./pkg/logql/bench` (no `...` suffix) targets only the bench package, matching the existing `remote-test` Makefile target. The `-run=TestRemoteStorageEquality` flag further restricts to only the correctness test.

**Outputs captured by Argo:**

- **Pod logs (stdout):** `standard-verbose` formatted test output — readable in Argo UI.
- **results.xml:** JUnit XML artifact — consumed by step 2 and viewable in Argo test result UI.

### Step 2: Push metrics

```yaml
- name: push-metrics
  script:
    image: <loki-test-image>  # needs correctness-metrics binary
    command: [correctness-metrics]
    args:
      - --input=/output/results.xml
      - --remote-write-url=$(REMOTE_WRITE_URL)
      - --remote-write-username=$(REMOTE_WRITE_USERNAME)
      - --remote-write-password=$(REMOTE_WRITE_PASSWORD)
      - --range-type=$(RANGE_TYPE)
  inputs:
    artifacts:
      - name: junit-xml
        path: /output/results.xml
        optional: true  # Allow step to run even if artifact is missing
```

**Key points:**

- `continueOn: failed` on step 1 ensures step 2 always runs, including on test failures.
- Secrets (`REMOTE_WRITE_PASSWORD`, Loki credentials) come from Kubernetes secrets via Argo env var injection.
- Stdout from step 2 contains the metric push summary.

**Failure modes at the Argo level:**

| Scenario | Argo Behavior | `correctness-metrics` Behavior |
|----------|---------------|-------------------------------|
| Step 1 tests fail, XML produced | Artifact transfers successfully | Parses XML, pushes metrics (including failure counts), exits `0` |
| Step 1 crashes before producing XML | Artifact marked optional — step 2 still runs | Exits `1` (input file missing) |
| Step 2 remote_write fails | Step 2 pod exits non-zero, workflow fails | Exits `1`, logs error |

The `optional: true` on the artifact input ensures step 2 always starts. The binary handles the missing-file case with a clear error message and exit code `1`.

## Makefile Target

Add a `make gotestsum-remote` target in `pkg/logql/bench/Makefile` (alongside the existing `remote-test` target).

```makefile
GOTESTSUM ?= $(shell which gotestsum 2>/dev/null)
RANGE_TYPE ?= range
METADATA_DIR ?= testdata

.PHONY: gotestsum-remote
gotestsum-remote:
ifndef GOTESTSUM
	$(error gotestsum not found in PATH — install with: go install gotest.tools/gotestsum@latest)
endif
ifndef REMOTE_ADDR_1
	$(error REMOTE_ADDR_1 is required — add it to .env (see .env.template))
endif
ifndef REMOTE_ADDR_2
	$(error REMOTE_ADDR_2 is required — add it to .env (see .env.template))
endif
ifndef REMOTE_ORG_ID
	$(error REMOTE_ORG_ID is required — add it to .env (see .env.template))
endif
	$(GOTESTSUM) \
		--format standard-verbose \
		--junitfile ./build/results.xml \
		-- go test -tags=remote_correctness -v \
		./pkg/logql/bench \
		-run TestRemoteStorageEquality \
		-timeout $(REMOTE_TIMEOUT) \
		-addr-1 "$(REMOTE_ADDR_1)" \
		-addr-2 "$(REMOTE_ADDR_2)" \
		-org-id "$(REMOTE_ORG_ID)" \
		-username "$(LOKI_USERNAME)" \
		-password "$(LOKI_PASSWORD)" \
		-metadata-dir $(METADATA_DIR) \
		-remote-range-type $(RANGE_TYPE)
```

**Notes:**
- Uses the same test path (`./pkg/logql/bench`) and `-run` filter as the existing `remote-test` target and the Argo step.
- `RANGE_TYPE` defaults to `range`, `METADATA_DIR` defaults to `testdata` — matching the existing `remote-test` behavior.
- `REMOTE_TIMEOUT` is already defined as `?= 30m` in the existing Makefile.
- Includes the same `ifndef` guards as `remote-test` for required env vars.
- Output goes to `./build/results.xml`. `gotestsum` must be pre-installed in `$PATH`.

## Test Code Change

One small change to `remote_test.go:176` — adding `direction` to the `t.Run()` format string. This affects both ad-hoc and CI test output (test names will include `/direction=FORWARD` or `/direction=BACKWARD`). No behavioral change to the tests themselves.

## What's Not Changing

- The existing `go test` / `make remote-test` workflow for ad-hoc remote test runs continues to work as-is.
- `assertions_test.go` and `convert_test.go` are not modified.
- The build tag gating (`//go:build remote_correctness`) stays as-is.
