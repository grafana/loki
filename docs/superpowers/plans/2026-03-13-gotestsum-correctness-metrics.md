# Gotestsum Correctness Metrics Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `correctness-metrics` CLI that parses gotestsum JUnit XML output and pushes test result metrics to a Prometheus remote_write endpoint.

**Architecture:** Two-phase pipeline — gotestsum runs tests and emits JUnit XML, then `correctness-metrics` parses it and pushes metrics. A small test code change adds `direction` to test names. Makefile targets wire everything together.

**Tech Stack:** Go, `github.com/joshdk/go-junit` (JUnit parsing), `github.com/prometheus/prometheus` (remote_write + types), `gotestsum` (test runner, not a Go dependency)

**Spec:** `docs/superpowers/specs/2026-03-12-gotestsum-correctness-metrics-design.md`

---

### Task 1: Add `direction` to remote test names

This is the only change to existing test code. It ensures each test has a unique name including direction, avoiding Go's `#NN` dedup suffixes.

**Files:**
- Modify: `pkg/logql/bench/remote_test.go:176`

- [ ] **Step 1: Change the `t.Run` format string**

In `pkg/logql/bench/remote_test.go`, line 176, change:

```go
t.Run(fmt.Sprintf("%s/kind=%s", tc.Source, tc.Kind()), func(t *testing.T) {
```

to:

```go
t.Run(fmt.Sprintf("%s/kind=%s/direction=%s", tc.Source, tc.Kind(), tc.Direction), func(t *testing.T) {
```

`tc.Direction` is `logproto.FORWARD` or `logproto.BACKWARD`, which stringifies to `FORWARD` or `BACKWARD`.

- [ ] **Step 2: Verify it compiles**

Run: `go build -tags remote_correctness ./pkg/logql/bench/...`
Expected: Clean build, no errors.

- [ ] **Step 3: Commit**

```bash
git add pkg/logql/bench/remote_test.go
git commit -m "test: add direction to remote correctness test names"
```

---

### Task 2: Add `go-junit` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/joshdk/go-junit@latest`

- [ ] **Step 2: Verify it resolved**

Run: `grep joshdk go.mod`
Expected: `github.com/joshdk/go-junit` appears in `require` block.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add github.com/joshdk/go-junit dependency"
```

---

### Task 3: Implement JUnit XML parser and test name label extractor

This is the core parsing logic — read JUnit XML, extract labels from test names, and produce structured results. Build this as a standalone, testable package within the `correctness-metrics` command directory.

**Files:**
- Create: `pkg/logql/bench/cmd/correctness-metrics/parser.go`
- Create: `pkg/logql/bench/cmd/correctness-metrics/parser_test.go`

- [ ] **Step 1: Write the failing test for label extraction**

Create `pkg/logql/bench/cmd/correctness-metrics/parser_test.go`:

```go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTestName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *testLabels
		wantErr  bool
	}{
		{
			name:  "metric query",
			input: "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD",
			expected: &testLabels{
				Suite:     "fast",
				QueryFile: "basic",
				Kind:      "metric",
				Direction: "FORWARD",
			},
		},
		{
			name:  "log query forward",
			input: "TestRemoteStorageEquality/regression/agg.yaml:12/kind=log/direction=FORWARD",
			expected: &testLabels{
				Suite:     "regression",
				QueryFile: "agg",
				Kind:      "log",
				Direction: "FORWARD",
			},
		},
		{
			name:  "log query backward",
			input: "TestRemoteStorageEquality/regression/agg.yaml:12/kind=log/direction=BACKWARD",
			expected: &testLabels{
				Suite:     "regression",
				QueryFile: "agg",
				Kind:      "log",
				Direction: "BACKWARD",
			},
		},
		{
			name:  "yml extension",
			input: "TestRemoteStorageEquality/exhaustive/filters.yml:7/kind=metric/direction=FORWARD",
			expected: &testLabels{
				Suite:     "exhaustive",
				QueryFile: "filters",
				Kind:      "metric",
				Direction: "FORWARD",
			},
		},
		{
			name:    "too few segments",
			input:   "TestRemoteStorageEquality/fast",
			wantErr: true,
		},
		{
			name:    "missing kind prefix",
			input:   "TestRemoteStorageEquality/fast/basic.yaml:3/metric/direction=FORWARD",
			wantErr: true,
		},
		{
			name:    "missing direction prefix",
			input:   "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/FORWARD",
			wantErr: true,
		},
		{
			name:    "completely unrelated test name",
			input:   "TestSomethingElse/foo/bar",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTestName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestParseTestName -v`
Expected: FAIL — `parseTestName` not defined.

- [ ] **Step 3: Implement the parser**

Create `pkg/logql/bench/cmd/correctness-metrics/parser.go`:

```go
package main

import (
	"fmt"
	"strings"
)

const testPrefix = "TestRemoteStorageEquality/"

// testLabels holds the labels extracted from a JUnit test case name.
type testLabels struct {
	Suite     string
	QueryFile string
	Kind      string
	Direction string
}

// parseTestName extracts structured labels from a JUnit test case name.
// Expected format: TestRemoteStorageEquality/{suite}/{file}.yaml:{line}/kind={kind}/direction={direction}
func parseTestName(name string) (*testLabels, error) {
	if !strings.HasPrefix(name, testPrefix) {
		return nil, fmt.Errorf("missing prefix %q", testPrefix)
	}
	rest := strings.TrimPrefix(name, testPrefix)

	segments := strings.Split(rest, "/")
	if len(segments) != 4 {
		return nil, fmt.Errorf("expected 4 segments, got %d: %q", len(segments), rest)
	}

	suite := segments[0]

	// Parse query_file: "basic.yaml:3" → split on ":" → "basic.yaml" → strip extension
	fileWithLine := segments[1]
	filePart, _, _ := strings.Cut(fileWithLine, ":")
	queryFile := strings.TrimSuffix(strings.TrimSuffix(filePart, ".yaml"), ".yml")

	// Parse kind: "kind=metric" → "metric"
	kindSeg := segments[2]
	if !strings.HasPrefix(kindSeg, "kind=") {
		return nil, fmt.Errorf("segment %q missing 'kind=' prefix", kindSeg)
	}
	kind := strings.TrimPrefix(kindSeg, "kind=")

	// Parse direction: "direction=FORWARD" → "FORWARD"
	dirSeg := segments[3]
	if !strings.HasPrefix(dirSeg, "direction=") {
		return nil, fmt.Errorf("segment %q missing 'direction=' prefix", dirSeg)
	}
	direction := strings.TrimPrefix(dirSeg, "direction=")

	return &testLabels{
		Suite:     suite,
		QueryFile: queryFile,
		Kind:      kind,
		Direction: direction,
	}, nil
}

// testStatus maps JUnit result to a status string.
type testStatus string

const (
	statusPass  testStatus = "pass"
	statusFail  testStatus = "fail"
	statusError testStatus = "error"
	statusSkip  testStatus = "skip"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestParseTestName -v`
Expected: PASS for all cases.

- [ ] **Step 5: Write the failing test for JUnit XML parsing and metric aggregation**

Add to `parser_test.go`:

```go
func TestParseJUnitResults(t *testing.T) {
	// Minimal JUnit XML that gotestsum would produce
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="pkg/logql/bench" tests="3" failures="1" time="12.5">
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD" time="2.1"></testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=FORWARD" time="3.2">
      <failure message="assertion failed">expected equal</failure>
    </testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=BACKWARD" time="4.0"></testcase>
  </testsuite>
</testsuites>`

	results, err := parseJUnitXML([]byte(xml))
	assert.NoError(t, err)
	assert.Equal(t, 12.5, results.TotalDurationSec)
	assert.Len(t, results.Tests, 3)

	// First test: pass
	assert.Equal(t, statusPass, results.Tests[0].Status)
	assert.Equal(t, "fast", results.Tests[0].Labels.Suite)
	assert.Equal(t, 2.1, results.Tests[0].DurationSec)

	// Second test: fail
	assert.Equal(t, statusFail, results.Tests[1].Status)

	// Third test: pass
	assert.Equal(t, statusPass, results.Tests[2].Status)
	assert.Equal(t, "BACKWARD", results.Tests[2].Labels.Direction)
}

func TestParseJUnitResults_UnparseableName(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="pkg/logql/bench" tests="1" time="1.0">
    <testcase name="TestSomethingElse/unknown" time="0.5"></testcase>
  </testsuite>
</testsuites>`

	results, err := parseJUnitXML([]byte(xml))
	assert.NoError(t, err)
	assert.Len(t, results.Tests, 1)
	assert.Nil(t, results.Tests[0].Labels)
	assert.Equal(t, statusPass, results.Tests[0].Status)
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestParseJUnit -v`
Expected: FAIL — `parseJUnitXML` not defined.

- [ ] **Step 7: Implement JUnit XML parsing**

Add to `parser.go`:

```go
import (
	junit "github.com/joshdk/go-junit"
)

// parsedTest holds a single test result with optional parsed labels.
type parsedTest struct {
	Name        string
	Labels      *testLabels // nil if name is unparseable
	Status      testStatus
	DurationSec float64
}

// parsedResults holds all parsed test results and run-level data.
type parsedResults struct {
	Tests           []parsedTest
	TotalDurationSec float64
}

// parseJUnitXML parses JUnit XML bytes into structured test results.
func parseJUnitXML(data []byte) (*parsedResults, error) {
	suites, err := junit.Ingest(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JUnit XML: %w", err)
	}

	var results parsedResults

	for _, suite := range suites {
		results.TotalDurationSec += suite.Totals.Duration.Seconds()

		for _, tc := range suite.Tests {
			pt := parsedTest{
				Name:        tc.Name,
				DurationSec: tc.Duration.Seconds(),
			}

			// Map JUnit status
			switch tc.Status {
			case junit.StatusPassed:
				pt.Status = statusPass
			case junit.StatusFailed:
				pt.Status = statusFail
			case junit.StatusError:
				pt.Status = statusError
			case junit.StatusSkipped:
				pt.Status = statusSkip
			default:
				pt.Status = statusPass
			}

			// Try to parse labels from test name
			labels, parseErr := parseTestName(tc.Name)
			if parseErr == nil {
				pt.Labels = labels
			}
			// If parseErr != nil, Labels stays nil — handled as unparseable

			results.Tests = append(results.Tests, pt)
		}
	}

	// Fallback: if suite duration was zero, sum test case durations
	if results.TotalDurationSec == 0 {
		for _, t := range results.Tests {
			results.TotalDurationSec += t.DurationSec
		}
	}

	return &results, nil
}
```

**Note:** The `go-junit` library uses `junit.Ingest(data)` to parse bytes. Check the actual API during implementation — if the function signature differs (e.g., takes `io.Reader`), adapt accordingly. The `suite.Totals.Duration` field provides the suite-level duration from the `time` attribute.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestParseJUnit -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add pkg/logql/bench/cmd/correctness-metrics/parser.go pkg/logql/bench/cmd/correctness-metrics/parser_test.go
git commit -m "feat: add JUnit XML parser and test name label extractor"
```

---

### Task 4: Implement metric builder

Converts parsed test results into Prometheus time series. Pure data transformation — no I/O.

**Files:**
- Create: `pkg/logql/bench/cmd/correctness-metrics/metrics.go`
- Create: `pkg/logql/bench/cmd/correctness-metrics/metrics_test.go`

- [ ] **Step 1: Write the failing test for metric building**

Create `pkg/logql/bench/cmd/correctness-metrics/metrics_test.go`:

```go
package main

import (
	"testing"

	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMetrics(t *testing.T) {
	results := &parsedResults{
		TotalDurationSec: 12.5,
		Tests: []parsedTest{
			{
				Name:        "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD",
				Labels:      &testLabels{Suite: "fast", QueryFile: "basic", Kind: "metric", Direction: "FORWARD"},
				Status:      statusPass,
				DurationSec: 2.1,
			},
			{
				Name:        "TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=FORWARD",
				Labels:      &testLabels{Suite: "fast", QueryFile: "basic", Kind: "log", Direction: "FORWARD"},
				Status:      statusFail,
				DurationSec: 3.2,
			},
			{
				Name:   "TestSomethingElse/unknown",
				Labels: nil, // unparseable
				Status: statusPass,
				DurationSec: 0.5,
			},
		},
	}

	ts := buildMetrics(results, "range", "logql-correctness")
	require.NotEmpty(t, ts)

	// Check that we have the expected metric names
	names := map[string]bool{}
	for _, s := range ts {
		for _, l := range s.Labels {
			if l.Name == "__name__" {
				names[l.Value] = true
			}
		}
	}

	assert.True(t, names["logql_correctness_tests_total"])
	assert.True(t, names["logql_correctness_test_duration_seconds"])
	assert.True(t, names["logql_correctness_run_duration_seconds"])
	assert.True(t, names["logql_correctness_run_pass_ratio"])

	// Check that unparseable test is counted in tests_total with suite=unknown
	// but NOT in test_duration_seconds
	var hasUnknownTotal bool
	var hasUnknownDuration bool
	for _, s := range ts {
		nameLabel := ""
		suiteLabel := ""
		for _, l := range s.Labels {
			if l.Name == "__name__" {
				nameLabel = l.Value
			}
			if l.Name == "suite" {
				suiteLabel = l.Value
			}
		}
		if nameLabel == "logql_correctness_tests_total" && suiteLabel == "unknown" {
			hasUnknownTotal = true
		}
		if nameLabel == "logql_correctness_test_duration_seconds" && suiteLabel == "unknown" {
			hasUnknownDuration = true
		}
	}
	assert.True(t, hasUnknownTotal, "unparseable test should be counted in tests_total with suite=unknown")
	assert.False(t, hasUnknownDuration, "unparseable test should NOT appear in test_duration_seconds")
}

func TestBuildMetrics_PassRatioZeroDenominator(t *testing.T) {
	results := &parsedResults{
		TotalDurationSec: 1.0,
		Tests: []parsedTest{
			{
				Name:        "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD",
				Labels:      &testLabels{Suite: "fast", QueryFile: "basic", Kind: "metric", Direction: "FORWARD"},
				Status:      statusSkip,
				DurationSec: 0,
			},
		},
	}

	ts := buildMetrics(results, "range", "logql-correctness")

	// pass_ratio should be omitted for suite "fast" since all tests are skipped
	for _, s := range ts {
		for _, l := range s.Labels {
			if l.Name == "__name__" && l.Value == "logql_correctness_run_pass_ratio" {
				for _, sl := range s.Labels {
					if sl.Name == "suite" {
						assert.NotEqual(t, "fast", sl.Value, "pass_ratio should be omitted when denominator is zero")
					}
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestBuildMetrics -v`
Expected: FAIL — `buildMetrics` not defined.

- [ ] **Step 3: Implement metric builder**

Create `pkg/logql/bench/cmd/correctness-metrics/metrics.go`:

```go
package main

import (
	"time"

	"github.com/prometheus/prometheus/prompb"
)

// buildMetrics converts parsed test results into Prometheus TimeSeries.
// rangeType is "instant" or "range" (static label from CLI flag).
// job is the job label value (e.g., "logql-correctness").
func buildMetrics(results *parsedResults, rangeType, job string) []prompb.TimeSeries {
	now := time.Now().UnixMilli()
	var series []prompb.TimeSeries

	// --- Per-test duration (only for parseable names) ---
	for _, t := range results.Tests {
		if t.Labels == nil {
			continue // skip unparseable
		}
		series = append(series, prompb.TimeSeries{
			Labels: []prompb.Label{
				{Name: "__name__", Value: "logql_correctness_test_duration_seconds"},
				{Name: "job", Value: job},
				{Name: "suite", Value: t.Labels.Suite},
				{Name: "query_file", Value: t.Labels.QueryFile},
				{Name: "kind", Value: t.Labels.Kind},
				{Name: "direction", Value: t.Labels.Direction},
				{Name: "range_type", Value: rangeType},
				{Name: "status", Value: string(t.Status)},
			},
			Samples: []prompb.Sample{{Value: t.DurationSec, Timestamp: now}},
		})
	}

	// --- Aggregate: tests_total and pass_ratio per suite ---
	type suiteStats struct {
		pass, fail, err, skip int
	}
	suites := map[string]*suiteStats{}

	for _, t := range results.Tests {
		suite := "unknown"
		if t.Labels != nil {
			suite = t.Labels.Suite
		}
		if suites[suite] == nil {
			suites[suite] = &suiteStats{}
		}
		switch t.Status {
		case statusPass:
			suites[suite].pass++
		case statusFail:
			suites[suite].fail++
		case statusError:
			suites[suite].err++
		case statusSkip:
			suites[suite].skip++
		}
	}

	for suite, stats := range suites {
		// tests_total per status
		for _, entry := range []struct {
			status string
			count  int
		}{
			{"pass", stats.pass},
			{"fail", stats.fail},
			{"error", stats.err},
			{"skip", stats.skip},
		} {
			series = append(series, prompb.TimeSeries{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "logql_correctness_tests_total"},
					{Name: "job", Value: job},
					{Name: "suite", Value: suite},
					{Name: "range_type", Value: rangeType},
					{Name: "status", Value: entry.status},
				},
				Samples: []prompb.Sample{{Value: float64(entry.count), Timestamp: now}},
			})
		}

		// pass_ratio — omit if denominator is zero
		denom := stats.pass + stats.fail + stats.err
		if denom > 0 {
			ratio := float64(stats.pass) / float64(denom)
			series = append(series, prompb.TimeSeries{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "logql_correctness_run_pass_ratio"},
					{Name: "job", Value: job},
					{Name: "suite", Value: suite},
					{Name: "range_type", Value: rangeType},
				},
				Samples: []prompb.Sample{{Value: ratio, Timestamp: now}},
			})
		}
	}

	// --- Run-level duration ---
	series = append(series, prompb.TimeSeries{
		Labels: []prompb.Label{
			{Name: "__name__", Value: "logql_correctness_run_duration_seconds"},
			{Name: "job", Value: job},
			{Name: "range_type", Value: rangeType},
		},
		Samples: []prompb.Sample{{Value: results.TotalDurationSec, Timestamp: now}},
	})

	return series
}
```

**Note:** This uses `prompb.TimeSeries` from `github.com/prometheus/prometheus/prompb`. Verify the exact import path during implementation — it may be `github.com/prometheus/prometheus/prompb` or in a different sub-package depending on the vendored version.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestBuildMetrics -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/logql/bench/cmd/correctness-metrics/metrics.go pkg/logql/bench/cmd/correctness-metrics/metrics_test.go
git commit -m "feat: add metric builder for correctness test results"
```

---

### Task 5: Implement remote_write pusher

Sends Prometheus time series to a remote_write endpoint. This is the I/O layer.

**Files:**
- Create: `pkg/logql/bench/cmd/correctness-metrics/push.go`
- Create: `pkg/logql/bench/cmd/correctness-metrics/push_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/logql/bench/cmd/correctness-metrics/push_test.go`:

```go
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushMetrics(t *testing.T) {
	var received prompb.WriteRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
		assert.Equal(t, "snappy", r.Header.Get("Content-Encoding"))
		assert.Equal(t, http.MethodPost, r.Method)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		decoded, err := snappy.Decode(nil, body)
		require.NoError(t, err)

		err = proto.Unmarshal(decoded, &received)
		require.NoError(t, err)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	series := []prompb.TimeSeries{
		{
			Labels:  []prompb.Label{{Name: "__name__", Value: "test_metric"}},
			Samples: []prompb.Sample{{Value: 1.0, Timestamp: 1000}},
		},
	}

	err := pushMetrics(server.URL, "", "", series)
	require.NoError(t, err)
	assert.Len(t, received.Timeseries, 1)
	assert.Equal(t, "test_metric", received.Timeseries[0].Labels[0].Value)
}

func TestPushMetrics_BasicAuth(t *testing.T) {
	var receivedUser, receivedPass string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, receivedPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	series := []prompb.TimeSeries{
		{
			Labels:  []prompb.Label{{Name: "__name__", Value: "test_metric"}},
			Samples: []prompb.Sample{{Value: 1.0, Timestamp: 1000}},
		},
	}

	err := pushMetrics(server.URL, "myuser", "mypass", series)
	require.NoError(t, err)
	assert.Equal(t, "myuser", receivedUser)
	assert.Equal(t, "mypass", receivedPass)
}

func TestPushMetrics_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	series := []prompb.TimeSeries{
		{
			Labels:  []prompb.Label{{Name: "__name__", Value: "test_metric"}},
			Samples: []prompb.Sample{{Value: 1.0, Timestamp: 1000}},
		},
	}

	err := pushMetrics(server.URL, "", "", series)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestPushMetrics -v`
Expected: FAIL — `pushMetrics` not defined.

- [ ] **Step 3: Implement the pusher**

Create `pkg/logql/bench/cmd/correctness-metrics/push.go`:

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

// pushMetrics sends time series to a Prometheus remote_write endpoint.
// Single attempt with a 30-second timeout; no retries.
// username and password are optional (both empty = no auth).
func pushMetrics(url, username, password string, series []prompb.TimeSeries) error {
	req := &prompb.WriteRequest{
		Timeseries: series,
	}

	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal write request: %w", err)
	}

	compressed := snappy.Encode(nil, data)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "snappy")
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	if username != "" && password != "" {
		httpReq.SetBasicAuth(username, password)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("remote_write request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote_write returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
```

**Note:** This uses the raw `net/http` approach with snappy + protobuf encoding rather than the Prometheus `storage/remote` client, which avoids API compatibility concerns with the vendored Prometheus version. The `prompb` types and `gogo/protobuf` + `golang/snappy` are already transitive dependencies in the Loki `go.mod`. Verify the exact import paths during implementation.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/logql/bench/cmd/correctness-metrics/ -run TestPushMetrics -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/logql/bench/cmd/correctness-metrics/push.go pkg/logql/bench/cmd/correctness-metrics/push_test.go
git commit -m "feat: add Prometheus remote_write pusher"
```

---

### Task 6: Implement `main.go` — CLI entrypoint

Wires together flag parsing, the parser, metric builder, and pusher. Follows the existing pattern from `cmd/generate-k6/main.go` (main calls run, run returns int exit code).

**Files:**
- Create: `pkg/logql/bench/cmd/correctness-metrics/main.go`

- [ ] **Step 1: Implement `main.go`**

Create `pkg/logql/bench/cmd/correctness-metrics/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	os.Exit(run())
}

func run() int {
	input := flag.String("input", "", "Path to JUnit XML file from gotestsum (required)")
	remoteWriteURL := flag.String("remote-write-url", envOrDefault("REMOTE_WRITE_URL", ""), "Prometheus remote_write endpoint")
	remoteWriteUsername := flag.String("remote-write-username", envOrDefault("REMOTE_WRITE_USERNAME", ""), "Basic auth username")
	remoteWritePassword := flag.String("remote-write-password", envOrDefault("REMOTE_WRITE_PASSWORD", ""), "Basic auth password")
	rangeType := flag.String("range-type", envOrDefault("RANGE_TYPE", ""), "Query range type: instant or range (required)")
	job := flag.String("job", "logql-correctness", "Job label for all metrics")
	dryRun := flag.Bool("dry-run", false, "Parse and log metrics without pushing")
	flag.Parse()

	// --- Validate flags ---
	if *input == "" {
		log.Println("ERROR: --input is required")
		flag.Usage()
		return 1
	}

	if *rangeType != "instant" && *rangeType != "range" {
		log.Printf("ERROR: --range-type must be 'instant' or 'range', got %q", *rangeType)
		return 1
	}

	if !*dryRun && *remoteWriteURL == "" {
		log.Println("ERROR: --remote-write-url is required (or use --dry-run)")
		flag.Usage()
		return 1
	}

	if (*remoteWriteUsername == "") != (*remoteWritePassword == "") {
		log.Println("ERROR: --remote-write-username and --remote-write-password must both be provided or both omitted")
		return 1
	}

	// --- Read and parse JUnit XML ---
	data, err := os.ReadFile(*input)
	if err != nil {
		log.Printf("ERROR: failed to read input file %q: %v", *input, err)
		return 1
	}

	results, err := parseJUnitXML(data)
	if err != nil {
		log.Printf("ERROR: failed to parse JUnit XML: %v", err)
		return 1
	}

	// --- Build metrics ---
	series := buildMetrics(results, *rangeType, *job)

	// --- Summary ---
	counts := map[testStatus]int{}
	for _, t := range results.Tests {
		counts[t.Status]++
	}
	summary := fmt.Sprintf("%d pass, %d fail, %d error, %d skip",
		counts[statusPass], counts[statusFail], counts[statusError], counts[statusSkip])

	if *dryRun {
		log.Printf("DRY RUN: would push %d time series (%s)", len(series), summary)
		for _, s := range series {
			var name string
			for _, l := range s.Labels {
				if l.Name == "__name__" {
					name = l.Value
					break
				}
			}
			log.Printf("  %s = %.4f", name, s.Samples[0].Value)
		}
		return 0
	}

	// --- Push ---
	if err := pushMetrics(*remoteWriteURL, *remoteWriteUsername, *remoteWritePassword, series); err != nil {
		log.Printf("ERROR: failed to push metrics: %v", err)
		return 1
	}

	log.Printf("Pushed %d metrics (%s) to %s", len(series), summary, *remoteWriteURL)
	return 0
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/logql/bench/cmd/correctness-metrics/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add pkg/logql/bench/cmd/correctness-metrics/main.go
git commit -m "feat: add correctness-metrics CLI entrypoint"
```

---

### Task 7: Add Makefile targets

Add the `build-correctness-metrics` and `gotestsum-remote` targets to the existing Makefile.

**Files:**
- Modify: `pkg/logql/bench/Makefile`

- [ ] **Step 1: Add variable defaults near the top of the Makefile**

In `pkg/logql/bench/Makefile`, after the existing variable definitions (around line 15), add:

```makefile
GOTESTSUM ?= $(shell which gotestsum 2>/dev/null)
RANGE_TYPE ?= range
METADATA_DIR ?= testdata
```

- [ ] **Step 2: Add to the `.PHONY` line**

Update the `.PHONY` line (line 1) to include the new targets:

```makefile
.PHONY: generate bench list run run-debug stream grafana grafana-stop discover remote-test generate-k6 gotestsum-remote build-correctness-metrics
```

- [ ] **Step 3: Add the `build-correctness-metrics` target**

After the existing `remote-test` target (after line 186), add:

```makefile
#####################################
# Correctness Metrics Binary        #
#####################################

build-correctness-metrics:
	CGO_ENABLED=0 go build -o ./build/correctness-metrics ./cmd/correctness-metrics
```

- [ ] **Step 4: Add the `gotestsum-remote` target**

After the `build-correctness-metrics` target, add:

```makefile
#####################################
# gotestsum Remote Test Runner      #
#####################################

# gotestsum-remote runs the remote correctness tests via gotestsum,
# producing JUnit XML output in ./build/results.xml.
# Requires gotestsum in $PATH: go install gotest.tools/gotestsum@latest
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
	@mkdir -p ./build
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

- [ ] **Step 5: Verify `make build-correctness-metrics` works**

Run (from `pkg/logql/bench/`): `make build-correctness-metrics`
Expected: Binary produced at `./build/correctness-metrics`.

- [ ] **Step 6: Verify `build/` is gitignored**

Check if `build/` is in `.gitignore`. If not, add it.

- [ ] **Step 7: Commit**

```bash
git add pkg/logql/bench/Makefile
git commit -m "build: add gotestsum-remote and build-correctness-metrics Makefile targets"
```

---

### Task 8: End-to-end dry-run test

Verify the full pipeline works by running the binary against a sample JUnit XML file.

**Files:**
- Create: `pkg/logql/bench/cmd/correctness-metrics/testdata/sample_results.xml` (test fixture)

- [ ] **Step 1: Create a sample JUnit XML fixture**

Create `pkg/logql/bench/cmd/correctness-metrics/testdata/sample_results.xml`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="github.com/grafana/loki/v3/pkg/logql/bench" tests="4" failures="1" time="15.3">
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD" classname="pkg/logql/bench" time="2.1"></testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=FORWARD" classname="pkg/logql/bench" time="3.2">
      <failure message="assertion failed: expected values to be equal">values differ</failure>
    </testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=BACKWARD" classname="pkg/logql/bench" time="4.0"></testcase>
    <testcase name="TestRemoteStorageEquality/regression/agg.yaml:12/kind=metric/direction=FORWARD" classname="pkg/logql/bench" time="6.0"></testcase>
  </testsuite>
</testsuites>
```

- [ ] **Step 2: Build the binary**

Run (from repo root): `go build -o ./pkg/logql/bench/build/correctness-metrics ./pkg/logql/bench/cmd/correctness-metrics/`

- [ ] **Step 3: Run dry-run against fixture**

Run:
```bash
./pkg/logql/bench/build/correctness-metrics \
  --input ./pkg/logql/bench/cmd/correctness-metrics/testdata/sample_results.xml \
  --range-type range \
  --dry-run
```

Expected output (approximately):
```
DRY RUN: would push N time series (3 pass, 1 fail, 0 error, 0 skip)
  logql_correctness_test_duration_seconds = 2.1000
  logql_correctness_test_duration_seconds = 3.2000
  logql_correctness_test_duration_seconds = 4.0000
  logql_correctness_test_duration_seconds = 6.0000
  logql_correctness_tests_total = ...
  ...
  logql_correctness_run_duration_seconds = 15.3000
```

Verify:
- Exit code is `0`
- All 4 tests are counted
- Pass ratio for "fast" suite: `2 / (2 + 1 + 0) = 0.6667`
- Run duration is `15.3` (from `<testsuite time="15.3">`)

- [ ] **Step 4: Test error cases**

Run without required flags:
```bash
./pkg/logql/bench/build/correctness-metrics --input nonexistent.xml --range-type range --dry-run
```
Expected: Exit code `1`, error about missing file.

Run with invalid range type:
```bash
./pkg/logql/bench/build/correctness-metrics --input ./pkg/logql/bench/cmd/correctness-metrics/testdata/sample_results.xml --range-type foo --dry-run
```
Expected: Exit code `1`, error about invalid range type.

- [ ] **Step 5: Commit test fixture**

```bash
git add pkg/logql/bench/cmd/correctness-metrics/testdata/sample_results.xml
git commit -m "test: add sample JUnit XML fixture for dry-run verification"
```

---

### Task 9: Run all tests and final cleanup

- [ ] **Step 1: Run all tests in the correctness-metrics package**

Run: `go test -v ./pkg/logql/bench/cmd/correctness-metrics/...`
Expected: All tests pass.

- [ ] **Step 2: Run `go vet`**

Run: `go vet ./pkg/logql/bench/cmd/correctness-metrics/...`
Expected: No issues.

- [ ] **Step 3: Run the bench package tests (ensure no regressions)**

Run: `go test -v ./pkg/logql/bench/ -run TestRemote -count=0`
(count=0 just checks compilation with the build tag excluded)

Also verify with build tag: `go build -tags remote_correctness ./pkg/logql/bench/...`
Expected: Clean build.

- [ ] **Step 4: Final commit if any cleanup was needed**

```bash
git add -A
git commit -m "chore: final cleanup for correctness-metrics implementation"
```
