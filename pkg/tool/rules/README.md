# Loki Rules Unit Testing

This guide explains how to use `lokitool rules test` to unit test your Loki alerting and recording rules.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Test File Format](#test-file-format)
- [Input Streams](#input-streams)
- [Alert Rule Tests](#alert-rule-tests)
- [LogQL Expression Tests](#logql-expression-tests)
- [Time and Intervals](#time-and-intervals)
- [Command-Line Usage](#command-line-usage)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)
- [Advanced Features](#advanced-features)

## Overview

Lokitool provides a unit testing framework for LogQL alerting and recording rules. The test format is inspired by Prometheus promtool but designed specifically for Loki's log-based queries, with support for:

- Testing alerting rules to verify they fire under expected conditions
- Testing recording rules and LogQL expressions
- Testing LogQL parsing, filtering, and pattern extraction
- Explicit log line content for realistic testing
- Detailed error messages with expected vs actual comparisons

## Quick Start

### 1. Create a Test File

Create a YAML file (e.g., `test.yml`) with your test cases:

```yaml
# Specify the rule files to test
rule_files:
  - rules.yml

# Default evaluation interval
evaluation_interval: 1m

# Test cases
tests:
  - name: "Test JSON Parsing"
    interval: 1m
    input_streams:
      - labels: '{job="app"}'
        lines:
          - '{"level":"info","duration":100}'
          - '{"level":"error","duration":500}'
          - '{"level":"error","duration":800}'

    logql_expr_test:
      - expr: 'count_over_time({job="app"} | json | level="error" [5m])'
        eval_time: 2m
        exp_samples:
          - labels: '{job="app"}'
            value: 2
```

### 2. Run Tests

```bash
lokitool rules test test.yml
```

### 3. View Results

Result output is in the same format as Promtool rules test.

**Success output:**
```
SUCCESS
```

**Failure output:**
```
FAILED:
  name: Test JSON Parsing,
  expr: count_over_time({job="app"} | json | level="error" [5m]), time: 2m,
    exp:[
        0:
          Labels:{job="app"}
          Value:2
        ],
    got:[
        0:
          Labels:{job="app"}
          Value:1
        ]
```

## Test File Format

### Basic Structure

```yaml
# Rule files to test (supports globs)
rule_files:
  - rules.yml
  - alerts/*.yml

# Default evaluation interval
evaluation_interval: 1m

# Optional: specify evaluation order for rule groups
group_eval_order:
  - group1
  - group2

# Test cases
tests:
  - name: "Test Name"
    interval: 1m                # Time between consecutive log entries
    external_labels:            # Optional: external labels added to all alerts
      cluster: prod
    external_url: "https://..."  # Optional: external URL (for compatibility, not used in tests)
    input_streams:
      # Test data streams
    alert_rule_test:
      # Alert tests
    logql_expr_test:
      # LogQL expression tests
```

### Rule Files

Specify the rule files to load and test:

```yaml
rule_files:
  - rules.yml           # Single file
  - alerts/*.yml        # Glob pattern
  - /abs/path/rules.yml # Absolute path
```

Relative paths are resolved relative to the test file location.

## Input Streams

Input streams define test data using explicit log line content.

### Basic Format

```yaml
input_streams:
  - labels: '{job="app"}'
    lines:
      - 'log line 1'
      - 'log line 2'
      - 'log line 3'
```

### Supported LogQL Features

**JSON log lines** for testing `| json` parsing:

```yaml
- labels: '{job="app"}'
  lines:
    - '{"level":"info","duration":100,"status":"success"}'
    - '{"level":"error","duration":500,"status":"failed"}'
    - '{"invalid json'  # Intentionally malformed
```

**Plain text logs** for testing line filters (`|=` and `|~`):

```yaml
- labels: '{job="auth"}'
  lines:
    - 'INFO: user logged in successfully'
    - 'ERROR: authentication failed for user bob'
    - 'ERROR: authentication failed for user charlie'
```

**HTTP access logs** for testing regex filters:

```yaml
- labels: '{job="nginx"}'
  lines:
    - 'GET / HTTP/1.1 200'
    - 'GET /api HTTP/1.1 500'
    - 'POST /api HTTP/1.1 503'
```

Each string in the `lines` array becomes a log entry at successive time intervals. Use this to test:
- JSON parsing (`| json`)
- Logfmt parsing (`| logfmt`)
- Line filters (`|=`, `|~`, `!=`, `!~`)
- Pattern extraction (`| pattern`)
- Regex parsing (`| regexp`)

### Stream Labels

Define labels for your input streams using LogQL label syntax with equality matchers:

```yaml
# Valid - multiple labels
- labels: '{job="app", environment="prod", level="error"}'

# Valid - empty label set
- labels: '{}'

# Invalid - regex matchers not supported when defining stream labels
- labels: '{job=~"app.*"}'  # ❌
```

## Alert Rule Tests

Test that alerts fire (or don't fire) at specific times:

```yaml
alert_rule_test:
  - alertname: HighErrorRate
    eval_time: 5m              # Evaluate at this time
    exp_alerts:
      - exp_labels:            # Expected alert labels
          severity: warning
          job: app
        exp_annotations:        # Expected alert annotations
          summary: High error rate detected
```

### Testing "for" Duration

Alerts with a `for` clause require the condition to be true for the specified duration:

```yaml
# Alert rule: for: 2m
alert_rule_test:
  - alertname: SustainedHighRate
    eval_time: 1m
    exp_alerts: []             # Not firing yet (< 2m)

  - alertname: SustainedHighRate
    eval_time: 3m              # Evaluate after "for" duration expires
    exp_alerts:
      - exp_labels:
          severity: critical
          job: app
```

### Testing No Alerts

```yaml
# Expect no alerts to fire
- alertname: ShouldNotFire
  eval_time: 5m
  exp_alerts: []             # Empty list means no alerts expected
```

## LogQL Expression Tests

Test LogQL queries and recording rules:

### Metric Queries

Metric queries return numeric samples (Vector or Scalar):

```yaml
logql_expr_test:
  # Test JSON parsing with aggregation
  - expr: 'sum by (job) (count_over_time({job="app"} | json | level="error" [5m]))'
    eval_time: 4m
    exp_samples:
      - labels: '{job="app"}'
        value: 5

  # Test line filters
  - expr: 'count_over_time({job="auth"} |= "failed" [5m])'
    eval_time: 5m
    exp_samples:
      - labels: '{job="auth"}'
        value: 10

  # Test regex filters
  - expr: 'count_over_time({job="nginx"} |~ "5[0-9]{2}$" [5m])'
    eval_time: 5m
    exp_samples:
      - labels: '{job="nginx"}'
        value: 3
```

### Log Queries

Log queries return log lines (Streams):

```yaml
logql_expr_test:
  - expr: '{job="app"} |= "error"'
    eval_time: 5m
    exp_logs:
      - labels: '{job="app", level="error"}'
        line: 'ERROR: something went wrong'
        timestamp: 240000000000  # Optional: nanoseconds
```

### Supported LogQL Operations

The framework supports all LogQL features through Loki's MockQuerier:

- **JSON parsing**: `| json`
- **Logfmt parsing**: `| logfmt`
- **Line filters**: `|=`, `|~`, `!=`, `!~`
- **Label filters**: `| label="value"`
- **Pattern extraction**: `| pattern '<pattern>'`
- **Regex parsing**: `| regexp '<regex>'`
- **Unwrap operations**: `| unwrap field`
- **Aggregations**: `sum`, `count`, `rate`, `avg`, `max`, `min`, etc.

## Time and Intervals

There are three time-related settings that control different aspects of test execution:

### 1. `evaluation_interval` (top-level, optional)

Sets the default evaluation frequency for rule groups. Defaults to `1m` if not specified.

```yaml
rule_files:
  - rules.yml
evaluation_interval: 1m    # Rule groups evaluate every 1 minute (unless they specify their own interval)
```

This controls how often rules are evaluated and affects `for` clauses in alerts. An alert with `for: 2m` and `evaluation_interval: 1m` requires the condition to be true for 2 consecutive evaluation cycles.

### 2. `interval` (per test, required)

Controls the time spacing between consecutive log entries in your test data.

```yaml
tests:
  - name: "Test with 30-second interval"
    interval: 30s           # Each entry is 30 seconds apart
    input_streams:
      - labels: '{job="test"}'
        lines:
          - 'log entry 1'    # 1970-01-01T00:00:00Z
          - 'log entry 2'    # 1970-01-01T00:00:30Z
          - 'log entry 3'    # 1970-01-01T00:01:00Z
          - 'log entry 4'    # 1970-01-01T00:01:30Z
          - 'log entry 5'    # 1970-01-01T00:02:00Z
```

All streams start at Unix epoch (1970-01-01T00:00:00Z) and increment by the specified interval.

### 3. `eval_time` (per test case, required)

Specifies the simulation time at which to evaluate a specific test case. This is independent of `evaluation_interval` - while `evaluation_interval` controls how frequently rules are evaluated during the simulation, `eval_time` specifies the single moment in time when you want to check the test's expected outcome.

```yaml
logql_expr_test:
  - expr: 'count_over_time({job="test"} [2m])'
    eval_time: 3m           # Check results at the 3-minute mark
    exp_samples:
      - labels: '{job="test"}'
        value: 5            # Counts entries from 1m to 3m
```

**Key difference:** `evaluation_interval` affects how rules behave (especially `for` clauses), while `eval_time` is just when you observe the result for testing purposes.

## Command-Line Usage

### Basic Usage

```bash
lokitool rules test [flags] <test-files>...
```

### Examples

```bash
# Run a single test file
lokitool rules test test.yml

# Run multiple test files
lokitool rules test test1.yml test2.yml test3.yml

# Use glob patterns
lokitool rules test tests/*.yml
```

## Example Test Files

### Example 1: JSON Parsing Test

```yaml
rule_files:
  - rules.yml

evaluation_interval: 1m

tests:
  - name: "Test JSON error detection"
    interval: 1m
    input_streams:
      - labels: '{job="app"}'
        lines:
          - '{"level":"info","msg":"started"}'
          - '{"level":"error","msg":"database timeout"}'
          - '{"level":"error","msg":"connection failed"}'
          - '{"level":"info","msg":"finished"}'

    logql_expr_test:
      - expr: 'count_over_time({job="app"} | json | level="error" [5m])'
        eval_time: 3m
        exp_samples:
          - labels: '{job="app"}'
            value: 2
```

### Example 2: Alert Test

```yaml
rule_files:
  - rules.yml

evaluation_interval: 1m

tests:
  - name: "Test authentication failures"
    interval: 1m
    input_streams:
      - labels: '{job="auth"}'
        lines:
          - 'INFO: user alice logged in'
          - 'ERROR: authentication failed for user bob'
          - 'INFO: user alice logged out'
          - 'ERROR: authentication failed for user charlie'
          - 'ERROR: authentication failed for user dave'

    alert_rule_test:
      - alertname: HighAuthFailureRate
        eval_time: 4m
        exp_alerts:
          - exp_labels:
              severity: warning
              job: auth
            exp_annotations:
              summary: High rate of authentication failures
```

### Example 3: Regex Filter Test

```yaml
rule_files:
  - rules.yml

evaluation_interval: 1m

tests:
  - name: "Test HTTP 5xx errors"
    interval: 1m
    input_streams:
      - labels: '{job="nginx"}'
        lines:
          - 'GET / HTTP/1.1 200'
          - 'GET /api HTTP/1.1 500'
          - 'POST /api HTTP/1.1 503'
          - 'GET /health HTTP/1.1 200'
          - 'GET /api HTTP/1.1 502'

    logql_expr_test:
      - expr: 'count_over_time({job="nginx"} |~ "5[0-9]{2}$" [5m])'
        eval_time: 4m
        exp_samples:
          - labels: '{job="nginx"}'
            value: 3
```

## Troubleshooting

### Test Failures

When a test fails, the output shows detailed differences between expected and actual results, this is the same output format as Promtool:

```
FAILED:
  name: Test JSON Parsing,
  expr: count_over_time({job="app"} | json | level="error" [5m]), time: 4m,
    exp:[
        0:
          Labels:{job="app"}
          Value:5
        ],
    got:[
        0:
          Labels:{job="app"}
          Value:3
        ]
```

### Common Issues

1. **Wrong eval_time**: Make sure eval_time is after enough log entries have been generated

   ```yaml
   # ❌ Wrong - only 2 entries exist at 1m with 1m interval
   eval_time: 1m

   # ✅ Correct - 5 entries exist at 4m
   eval_time: 4m
   ```

2. **Label mismatch**: Verify that expected labels match the actual labels

   Labels can be specified in any order in your expected values - the comparison automatically normalizes label order.

   ```yaml
   # ✅ Both of these work correctly
   exp_samples:
     - labels: '{environment="prod", job="api"}'
     - labels: '{job="api", environment="prod"}'
   ```

3. **Range query timing**: Range queries `[5m]` look back from eval_time

   ```yaml
   # With 1m interval and eval_time: 3m
   # Range [5m] includes entries at: 0m, 1m, 2m, 3m = 4 entries
   expr: 'count_over_time({job="test"} [5m])'
   eval_time: 3m
   ```

## Advanced Features

### Testing Multiple Streams

Test rules that aggregate across multiple streams:

```yaml
input_streams:
  - labels: '{job="app", instance="host1"}'
    lines: ['error 1', 'error 2', 'error 3']

  - labels: '{job="app", instance="host2"}'
    lines: ['error 1', 'error 2']

logql_expr_test:
  - expr: 'sum by (job) (count_over_time({job="app"} [5m]))'
    eval_time: 3m
    exp_samples:
      - labels: '{job="app"}'
        value: 5  # 3 + 2
```

## Differences from Promtool

While inspired by promtool, lokitool's test format is designed for Loki:

- **Log-focused**: Use explicit log content instead of numeric time series
- **LogQL expressions**: Write LogQL queries instead of PromQL
- **Log stream results**: Use `exp_logs` for log stream query results
- **Stream terminology**: Uses `input_streams` and `stream` (not `series`)

## Example Files

- [testdata/demo_test.yml](testdata/demo_test.yml) - Complete working examples of all test modes
- [testdata/demo_rules.yml](testdata/demo_rules.yml) - Example rule file with LogQL rules
