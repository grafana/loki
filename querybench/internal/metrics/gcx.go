// Package metrics captures backend system metrics for a query's run window by
// running instant PromQL queries through the gcx CLI.
package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"time"
)

// Runner executes a command and returns its standard output. It is the seam the
// tests replace to avoid launching gcx.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner runs the command for real and returns only its standard output, so
// the gcx hint line (written to standard error) never reaches the JSON parser.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// errNoData reports that a query succeeded but returned no usable sample, so the
// metric is recorded as absent rather than zero.
type errNoData struct{ expr string }

func (e errNoData) Error() string { return "no sample for query: " + e.expr }

// queryInstant runs one instant PromQL query at evalTime through gcx and returns
// the scalar value of its first result sample.
//
// It returns errNoData when the result set is empty or the sample is NaN or
// infinite, so a missing metric is distinguishable from a real zero.
func queryInstant(ctx context.Context, run Runner, datasource, expr string, evalTime time.Time) (float64, error) {
	out, err := run(ctx, "gcx", "metrics", "query",
		"-d", datasource,
		expr,
		"--time", evalTime.UTC().Format(time.RFC3339),
		"-o", "json",
		"--no-color",
	)
	if err != nil {
		// gcx writes its diagnostic to stderr, which .Output() captures on the
		// ExitError; include it so a capture failure is not just "exit status 1".
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return 0, fmt.Errorf("run gcx: %w: %s", err, bytes.TrimSpace(exit.Stderr))
		}
		return 0, fmt.Errorf("run gcx: %w", err)
	}
	return parseScalar(out, expr)
}

// gcxResponse is the subset of the Prometheus instant-query response gcx emits.
type gcxResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value [2]json.RawMessage `json:"value"` // [ <unix seconds>, "<value>" ]
		} `json:"result"`
	} `json:"data"`
}

// parseScalar extracts the scalar value from a gcx instant-query response.
func parseScalar(out []byte, expr string) (float64, error) {
	var resp gcxResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, fmt.Errorf("parse gcx output: %w", err)
	}
	if resp.Status != "" && resp.Status != "success" {
		return 0, fmt.Errorf("gcx status %q for query %s", resp.Status, expr)
	}
	if len(resp.Data.Result) == 0 {
		return 0, errNoData{expr}
	}

	// The value is [timestamp, "stringified float"]; only the second element is
	// the sample.
	var s string
	if err := json.Unmarshal(resp.Data.Result[0].Value[1], &s); err != nil {
		return 0, fmt.Errorf("parse sample value for query %s: %w", expr, err)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sample float %q for query %s: %w", s, expr, err)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errNoData{expr}
	}
	return v, nil
}

// promDuration renders d as a Prometheus range-vector duration in whole seconds,
// e.g. 14m becomes "840s". Seconds avoid any ambiguity from sub-minute windows.
func promDuration(d time.Duration) string {
	secs := int64(d / time.Second)
	if secs < 1 {
		secs = 1
	}
	return strconv.FormatInt(secs, 10) + "s"
}
