package logqltest

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// defaultEpsilon is the tolerance used when comparing floating point results.
const defaultEpsilon = 1e-9

// RunScript parses and executes a single `.test` script, failing t on any mismatch.
//
// A script describes log streams to load and metric queries to evaluate against
// absolute expected results, in a DSL documented in README.md. Each query is executed
// through the real logql.Engine over an in-memory pipeline-running querier, so the full
// parsing/extraction pipeline is exercised end-to-end.
func RunScript(t *testing.T, name, script string) {
	t.Helper()

	streams := newStreamsParser()
	lines := strings.Split(script, "\n")

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(stripComment(lines[i]))
		if trimmed == "" {
			i++
			continue
		}

		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "load":
			i++
			i = consumeBlock(lines, i, func(content string) {
				if err := streams.parse(content); err != nil {
					t.Fatalf("%s: invalid load line %q: %v", name, content, err)
				}
			})
		case "eval":
			cmd, err := parseEval(trimmed)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			i++
			expected := newExpectationsParser()
			i = consumeBlock(lines, i, func(content string) {
				if err := expected.parse(content); err != nil {
					t.Fatalf("%s: invalid expectation %q: %v", name, content, err)
				}
			})
			runEval(t, name, streams.get(), cmd, expected.get())
		default:
			t.Fatalf("%s: unexpected command %q", name, fields[0])
		}
	}
}

func runEval(t *testing.T, name string, streams []logproto.Stream, cmd evalCmd, exp expectations) {
	t.Helper()
	label := cmd.query
	if cmd.instant {
		label = "instant: " + label
	} else {
		label = "range: " + label
	}

	t.Run(label, func(t *testing.T) {
		engine := logql.NewEngine(logql.EngineOpts{}, logql.NewMockQuerier(0, streams), logql.NoLimits, log.NewNopLogger())

		var start, end, step time.Duration
		if cmd.instant {
			start, end, step = cmd.ts, cmd.ts, 0
		} else {
			start, end, step = cmd.start, cmd.end, cmd.step
		}

		ctx := user.InjectOrgID(context.Background(), "fake")

		// Build the params and execute. A failure can surface at either step (parse-time
		// errors come from NewLiteralParams, evaluation errors from Exec).
		var res logqlmodel.Result
		params, err := logql.NewLiteralParams(
			cmd.query,
			epoch.Add(start), epoch.Add(end), step, 0,
			logproto.FORWARD, 1000, nil, nil,
		)
		if err == nil {
			res, err = engine.Query(params).Exec(ctx)
		}

		if exp.fail {
			require.Errorf(t, err, "%s: expected query %q to fail", name, cmd.query)
			switch exp.failKind {
			case "msg":
				require.Contains(t, err.Error(), exp.failText, "%s: failure message", name)
			case "regex":
				require.Regexp(t, exp.failText, err.Error(), "%s: failure regex", name)
			}
			return
		}

		require.NoError(t, err, "%s: query %q", name, cmd.query)
		compare(t, name, cmd, exp, res.Data)
	})
}

// consumeBlock feeds each indented (non-blank, non-comment) line following a command to fn,
// stopping at the first blank line, non-indented line, or EOF. It returns the next line index.
func consumeBlock(lines []string, i int, fn func(content string)) int {
	for i < len(lines) {
		raw := lines[i]
		if strings.TrimSpace(raw) == "" {
			// A blank line ends the block.
			break
		}
		if strings.HasPrefix(strings.TrimSpace(raw), "#") {
			// Skip a comment line inside the block.
			i++
			continue
		}
		if raw[0] != ' ' && raw[0] != '\t' {
			// A non-indented line ends the block.
			break
		}

		if content := strings.TrimSpace(stripComment(raw)); content != "" {
			fn(content)
		}
		i++
	}
	return i
}

func compare(t *testing.T, name string, cmd evalCmd, exp expectations, data interface{}) {
	t.Helper()
	switch v := data.(type) {
	case promql.Scalar:
		compareScalar(t, name, exp, v)
	case promql.Vector:
		compareVector(t, name, exp, v)
	case promql.Matrix:
		compareMatrix(t, name, cmd, exp, v)
	default:
		t.Fatalf("%s: unsupported result type %T (only metric queries are supported)", name, data)
	}
}

func compareScalar(t *testing.T, name string, exp expectations, s promql.Scalar) {
	t.Helper()
	require.NotNilf(t, exp.scalar, "%s: scalar result but no scalar value expected", name)
	require.Truef(t, floatsEqual(*exp.scalar, s.V), "%s: scalar mismatch: want %v, got %v", name, *exp.scalar, s.V)
}

func compareVector(t *testing.T, name string, exp expectations, v promql.Vector) {
	t.Helper()
	want := map[string]float64{}
	for _, es := range exp.series {
		require.Lenf(t, es.samples, 1, "%s: instant result expects one value per series: %s", name, es.labels)
		require.Truef(t, es.samples[0].present, "%s: instant result cannot have a gap: %s", name, es.labels)
		want[es.labels] = es.samples[0].value
	}

	got := map[string]float64{}
	for _, s := range v {
		got[s.Metric.String()] = s.F
	}

	require.Lenf(t, got, len(want), "%s: series count mismatch: want %v, got %v", name, want, got)
	for k, wv := range want {
		gv, ok := got[k]
		require.Truef(t, ok, "%s: missing expected series %s; got %v", name, k, got)
		require.Truef(t, floatsEqual(wv, gv), "%s: series %s value mismatch: want %v, got %v", name, k, wv, gv)
	}
}

func compareMatrix(t *testing.T, name string, cmd evalCmd, exp expectations, m promql.Matrix) {
	t.Helper()

	// Timestamps (ms) of each range step.
	var stepMillis []int64
	for ts := cmd.start; ts <= cmd.end; ts += cmd.step {
		stepMillis = append(stepMillis, epoch.Add(ts).UnixMilli())
		if cmd.step == 0 {
			break
		}
	}

	want := map[string][]sample{}
	for _, es := range exp.series {
		require.Lenf(t, es.samples, len(stepMillis), "%s: series %s has %d points, expected %d steps", name, es.labels, len(es.samples), len(stepMillis))
		want[es.labels] = es.samples
	}

	got := map[string]map[int64]float64{}
	for _, s := range m {
		byTS := map[int64]float64{}
		for _, p := range s.Floats {
			byTS[p.T] = p.F
		}
		got[s.Metric.String()] = byTS
	}

	require.Lenf(t, got, len(want), "%s: series count mismatch: want %d, got %d (%v)", name, len(want), len(got), keys(got))
	for k, points := range want {
		gotTS, ok := got[k]
		require.Truef(t, ok, "%s: missing expected series %s; got series %v", name, k, keys(got))
		for i, p := range points {
			ts := stepMillis[i]
			gv, present := gotTS[ts]
			if !p.present {
				require.Falsef(t, present, "%s: series %s step %d (t=%dms) should be empty, got %v", name, k, i, ts, gv)
				continue
			}
			require.Truef(t, present, "%s: series %s missing point at step %d (t=%dms)", name, k, i, ts)
			require.Truef(t, floatsEqual(p.value, gv), "%s: series %s step %d value mismatch: want %v, got %v", name, k, i, p.value, gv)
		}
	}
}

func floatsEqual(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return a == b
	}
	diff := math.Abs(a - b)
	if diff <= defaultEpsilon {
		return true
	}
	return diff/math.Max(math.Abs(a), math.Abs(b)) <= defaultEpsilon
}

func keys(m map[string]map[int64]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
