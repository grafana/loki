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

Test names follow the pattern established in `remote_test.go:176` and `query_registry.go:204`:

```
TestRemoteStorageEquality/{suite}/{file}.yaml:{line}/kind={kind}
```

Examples:

```
TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric
TestRemoteStorageEquality/regression/agg.yaml:12/kind=log
TestRemoteStorageEquality/exhaustive/filters.yaml:7/kind=metric
```

### `DirectionBoth` and Go's `#NN` dedup suffixes

Log queries default to `DirectionBoth` (`query_registry.go:207-209`), which produces two `TestCase` structs with identical `Source` fields — one for forward and one for backward. Because Go's `t.Run` deduplicates identical subtest names, the JUnit output will contain suffixed names:

```
TestRemoteStorageEquality/fast/basic.yaml:3/kind=log#00
TestRemoteStorageEquality/fast/basic.yaml:3/kind=log#01
```

The parser strips the `#NN` suffix before extracting labels. Both direction variants map to the same label set. If both pass, one metric is emitted. If one fails and one passes, the `fail` status takes precedence (worst-status-wins).

### Label extraction

The `correctness-metrics` binary parses test names to extract labels:

| Label | Parsed From | Example Values |
|-------|------------|----------------|
| `suite` | 1st path segment after root test name | `fast`, `regression`, `exhaustive` |
| `query_file` | 2nd segment (filename without `.yaml` extension) | `basic`, `agg`, `filters` |
| `kind` | `kind=` key-value in last segment | `metric`, `log`, `invalid` |
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

- **Counted** in the aggregate `logql_correctness_tests_total` metric with `suite="unknown"`.
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
| `logql_correctness_run_pass_ratio` | Gauge | `suite`, `range_type` | `pass / (pass + fail + error)` — skipped tests excluded from denominator |

### Per-test detail

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `logql_correctness_test_duration_seconds` | Gauge | `suite`, `query_file`, `kind`, `range_type`, `status` | Per-test execution duration (only for parseable test names) |

### Run-level

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `logql_correctness_run_duration_seconds` | Gauge | `range_type` | Total elapsed time from the root `<testsuite time="...">` attribute in JUnit XML |

## `correctness-metrics` CLI

Location: `pkg/logql/bench/cmd/correctness-metrics/main.go`

### Flags

| Flag | Env Var | Required | Default | Description |
|------|---------|----------|---------|-------------|
| `--input` | — | yes | — | Path to JUnit XML file from gotestsum |
| `--remote-write-url` | `REMOTE_WRITE_URL` | yes | — | Prometheus remote_write endpoint |
| `--remote-write-username` | `REMOTE_WRITE_USERNAME` | no | — | Basic auth username |
| `--remote-write-password` | `REMOTE_WRITE_PASSWORD` | no | — | Basic auth password |
| `--range-type` | `RANGE_TYPE` | yes | — | `instant` or `range` — static label on all metrics |
| `--job` | — | no | `logql-correctness` | Job label for all metrics |
| `--dry-run` | — | no | `false` | Parse and log metrics without pushing |

**Auth validation:** `--remote-write-username` and `--remote-write-password` must both be provided or both omitted. Providing only one is a fatal error.

### Behavior

1. Parse JUnit XML using `github.com/joshdk/go-junit`.
2. For each test case, extract labels from the test name using the pattern above.
3. Build Prometheus time series using `github.com/prometheus/prometheus` types (already in `go.mod`).
4. Push via remote_write using `github.com/prometheus/prometheus/storage/remote` (already in `go.mod`). Single attempt with a 30-second timeout; no retries.
5. Log a summary to stdout: `Pushed 47 metrics (32 pass, 12 fail, 3 skip) to https://...`

The `--dry-run` flag allows local testing: run `gotestsum` locally, then `correctness-metrics --dry-run --input results.xml` to verify what would be pushed.

### Exit codes

| Condition | Exit Code | Behavior |
|-----------|-----------|----------|
| Success | `0` | All metrics parsed and pushed (or `--dry-run`) |
| Input file missing | `1` | Log error, exit immediately |
| Input file empty or malformed XML | `1` | Log error describing the parse failure, exit immediately |
| `remote_write` failure (network, auth, 4xx/5xx) | `1` | Log the error from the Prometheus client, exit immediately |
| `--dry-run` | `0` | Always, regardless of what would have been pushed |
| Invalid flag combination (e.g., username without password) | `1` | Log usage error, exit immediately |

A non-zero exit from step 2 will fail the Argo workflow. This is intentional — if we ran tests but couldn't record the results, that's a CI failure worth investigating. Test results are still available as Argo artifacts for manual inspection.

### Dependencies

- JUnit XML parsing: `github.com/joshdk/go-junit` (new dependency)
- Prometheus types and remote_write: `github.com/prometheus/prometheus` (already in `go.mod`)
- No other new dependencies expected

The `github.com/prometheus/prometheus/storage/remote` package API should be verified against the vendored Prometheus version (`v0.310.1`) during implementation. If the client API has changed, use the `net/http` package to POST a snappy-compressed remote_write protobuf payload directly.

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
      - ./pkg/logql/bench/...
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
```

**Key points:**

- `continueOn: failed` on step 1 ensures metrics are always pushed, including on test failures.
- Secrets (`REMOTE_WRITE_PASSWORD`, Loki credentials) come from Kubernetes secrets via Argo env var injection.
- Stdout from step 2 contains the metric push summary.
- If step 1 crashes before producing `results.xml`, the Argo artifact transfer fails and step 2 exits with code `1` (input file missing).

## Makefile Target

Add a `make gotestsum-remote` target in `pkg/logql/bench/Makefile` (alongside the existing `remote-test` target). It requires the same env vars as `make remote-test` (`REMOTE_ADDR_1`, `REMOTE_ADDR_2`, `REMOTE_ORG_ID`, etc.).

```makefile
GOTESTSUM ?= $(shell which gotestsum 2>/dev/null)

.PHONY: gotestsum-remote
gotestsum-remote:
ifndef GOTESTSUM
	$(error gotestsum not found in PATH — install with: go install gotest.tools/gotestsum@latest)
endif
	$(GOTESTSUM) \
		--format standard-verbose \
		--junitfile ./build/results.xml \
		-- go test -tags remote_correctness -count=1 -v -timeout $(REMOTE_TIMEOUT) \
		./... \
		--addr-1 $(REMOTE_ADDR_1) \
		--addr-2 $(REMOTE_ADDR_2) \
		--org-id $(REMOTE_ORG_ID) \
		--metadata-dir $(METADATA_DIR) \
		--remote-range-type $(RANGE_TYPE)
```

Output goes to `./build/results.xml`. `gotestsum` must be pre-installed in `$PATH`.

## What's Not Changing

- The existing `go test` / `make` workflow for ad-hoc remote test runs is untouched.
- The test code in `remote_test.go`, `assertions_test.go`, and `convert_test.go` is not modified.
- The build tag gating (`//go:build remote_correctness`) stays as-is.
