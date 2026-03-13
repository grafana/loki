# correctness-metrics

A CLI tool that parses JUnit XML output from `gotestsum` and pushes LogQL correctness test result metrics to a Prometheus `remote_write` endpoint.

## Overview

This tool is the second step of a two-phase CI pipeline:

1. **`gotestsum`** runs the remote correctness tests and produces JUnit XML
2. **`correctness-metrics`** parses that XML and pushes pass/fail/duration metrics

Separating these into two steps means test failures don't prevent metric collection — you always get visibility into what happened.

## Build

From `pkg/logql/bench/`:

```bash
make build-correctness-metrics
# produces ./build/correctness-metrics
```

## Usage

```
correctness-metrics \
  --input ./build/results.xml \
  --range-type range \
  --remote-write-url https://prometheus.example.com/api/v1/write \
  --remote-write-username "$USERNAME" \
  --remote-write-password "$PASSWORD"
```

### Flags

| Flag | Env Var | Required | Description |
|------|---------|----------|-------------|
| `--input` | — | Yes | Path to JUnit XML file |
| `--range-type` | `RANGE_TYPE` | Yes | `instant` or `range` |
| `--remote-write-url` | `REMOTE_WRITE_URL` | Yes (unless `--dry-run`) | Prometheus remote_write endpoint |
| `--remote-write-username` | `REMOTE_WRITE_USERNAME` | No | Basic auth username (must pair with password) |
| `--remote-write-password` | `REMOTE_WRITE_PASSWORD` | No | Basic auth password (must pair with username) |
| `--job` | — | No | Job label (default: `logql-correctness`) |
| `--dry-run` | — | No | Parse and log metrics without pushing |

### Dry Run

Use `--dry-run` to see what metrics would be pushed without needing a remote_write endpoint:

```bash
correctness-metrics \
  --input ./build/results.xml \
  --range-type range \
  --dry-run
```

## Metrics Produced

### Per-test duration

```
logql_correctness_test_duration_seconds{
  direction="FORWARD",
  job="logql-correctness",
  kind="metric",
  query_file="basic",
  range_type="range",
  status="pass",
  suite="fast"
}
```

One series per test case with a parseable name. Labels are extracted from the Go subtest name format:

```
TestRemoteStorageEquality/{suite}/{file}.yaml:{line}/kind={kind}/direction={direction}
```

### Aggregate counts per suite

```
logql_correctness_tests_total{
  job="logql-correctness",
  range_type="range",
  status="pass",
  suite="fast"
}
```

Four series per suite (one for each status: pass, fail, error, skip).

### Pass ratio per suite

```
logql_correctness_run_pass_ratio{
  job="logql-correctness",
  range_type="range",
  suite="fast"
}
```

`pass / (pass + fail + error)`. Omitted when the denominator is zero (all tests skipped).

### Run duration

```
logql_correctness_run_duration_seconds{
  job="logql-correctness",
  range_type="range"
}
```

Total wall-clock duration from the JUnit XML suite-level `time` attribute.

## Argo Workflow Integration

The intended use is a two-step Argo workflow where step 1 runs the tests and step 2 pushes metrics. The steps share an output directory via an `emptyDir` volume.

### Example Workflow Template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: logql-correctness-
spec:
  entrypoint: correctness-pipeline
  volumes:
    - name: build-output
      emptyDir: {}

  templates:
    - name: correctness-pipeline
      steps:
        # Step 1: Run tests via gotestsum, producing JUnit XML.
        # This step may fail (test failures), but we still want metrics.
        - - name: run-tests
            template: gotestsum-remote
            continueOn:
              failed: true

        # Step 2: Parse JUnit XML and push metrics.
        # Runs regardless of step 1 outcome.
        - - name: push-metrics
            template: push-correctness-metrics

    - name: gotestsum-remote
      container:
        image: <your-test-image>  # image with Go, gotestsum, and the test binary
        command: [gotestsum]
        args:
          - --format=standard-verbose
          - --junitfile=/output/results.xml
          - --
          - -tags=remote_correctness
          - -v
          - ./pkg/logql/bench
          - -run=TestRemoteStorageEquality
          - -timeout=30m
          - -addr-1=$(REMOTE_ADDR_1)
          - -addr-2=$(REMOTE_ADDR_2)
          - -org-id=$(REMOTE_ORG_ID)
          - -username=$(LOKI_USERNAME)
          - -password=$(LOKI_PASSWORD)
          - -metadata-dir=testdata
          - -remote-range-type=range
        env:
          - name: REMOTE_ADDR_1
            valueFrom:
              secretKeyRef:
                name: correctness-test-secrets
                key: remote-addr-1
          - name: REMOTE_ADDR_2
            valueFrom:
              secretKeyRef:
                name: correctness-test-secrets
                key: remote-addr-2
          - name: REMOTE_ORG_ID
            valueFrom:
              secretKeyRef:
                name: correctness-test-secrets
                key: remote-org-id
          - name: LOKI_USERNAME
            valueFrom:
              secretKeyRef:
                name: correctness-test-secrets
                key: loki-username
          - name: LOKI_PASSWORD
            valueFrom:
              secretKeyRef:
                name: correctness-test-secrets
                key: loki-password
        volumeMounts:
          - name: build-output
            mountPath: /output

    - name: push-correctness-metrics
      container:
        image: <your-test-image>  # same image, or one with just the correctness-metrics binary
        command: [correctness-metrics]
        args:
          - --input=/output/results.xml
          - --range-type=range
        env:
          - name: REMOTE_WRITE_URL
            valueFrom:
              secretKeyRef:
                name: correctness-metrics-secrets
                key: remote-write-url
          - name: REMOTE_WRITE_USERNAME
            valueFrom:
              secretKeyRef:
                name: correctness-metrics-secrets
                key: remote-write-username
          - name: REMOTE_WRITE_PASSWORD
            valueFrom:
              secretKeyRef:
                name: correctness-metrics-secrets
                key: remote-write-password
        volumeMounts:
          - name: build-output
            mountPath: /output
```

### Key Design Points

**`continueOn: failed: true`** on the test step is critical. Without it, test failures would prevent metric collection — and test failures are exactly when you most need metrics.

**Shared volume** (`emptyDir`): The test step writes `results.xml` to `/output/`, and the metrics step reads it from the same path. No need for artifact passing or S3.

**Secrets via `secretKeyRef`**: All credentials (Loki endpoints, remote_write auth) are pulled from Kubernetes secrets, never hardcoded. The `correctness-metrics` CLI reads `REMOTE_WRITE_URL`, `REMOTE_WRITE_USERNAME`, and `REMOTE_WRITE_PASSWORD` from environment variables.

**Separate secret objects**: Test secrets (Loki endpoints) and metrics secrets (Prometheus remote_write) are in different Kubernetes Secret resources since they may have different access policies.

### Running Both Range Types

To test both `instant` and `range` query types, duplicate the pipeline with different `--remote-range-type` and `--range-type` values:

```yaml
steps:
  - - name: run-range-tests
      template: gotestsum-remote
      arguments:
        parameters:
          - name: range-type
            value: range
      continueOn:
        failed: true
  - - name: push-range-metrics
      template: push-correctness-metrics
      arguments:
        parameters:
          - name: range-type
            value: range

  - - name: run-instant-tests
      template: gotestsum-remote
      arguments:
        parameters:
          - name: range-type
            value: instant
      continueOn:
        failed: true
  - - name: push-instant-metrics
      template: push-correctness-metrics
      arguments:
        parameters:
          - name: range-type
            value: instant
```

## Architecture

```
gotestsum                    correctness-metrics
    |                               |
    v                               v
[Go tests] --> results.xml --> [parser.go]
                                    |
                                    v
                               [metrics.go] --> Prometheus TimeSeries
                                    |
                                    v
                               [push.go] --> remote_write endpoint
```

- **parser.go** — Parses JUnit XML via `go-junit`, extracts structured labels from test names
- **metrics.go** — Builds Prometheus `TimeSeries` from parsed results
- **push.go** — Encodes with protobuf + snappy, sends via HTTP POST to remote_write
- **main.go** — CLI flag parsing, validation, orchestration
