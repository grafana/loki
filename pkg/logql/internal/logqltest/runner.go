package logqltest

import (
	"context"
	"fmt"
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

const (
	tenant = "fake"

	// defaultEpsilon is the tolerance used when comparing floating point results.
	defaultEpsilon = 1e-9
)

// RunScript parses and executes a single `.logqltest` script, failing t on any mismatch.
//
// A script describes log streams to load and metric queries to evaluate against absolute
// expected results, in a DSL documented in README.md. Loaded streams are encoded into a real
// chunk store and each query runs through the production storage read path + logql.Engine, so
// the full chunk-decode/parsing/extraction pipeline is exercised end-to-end.
func RunScript(t *testing.T, name, script string) {
	t.Helper()

	var (
		store          *testingChunkStore
		querier        logql.Querier
		streams        = newStreamsParser()
		streamsChanged = true
	)

	// The loaded streams are materialised into a real chunk store, rebuilt whenever the data
	// changes (after load/clear) and reused across the evals that query it.
	defer func() {
		if store != nil {
			store.close()
		}
	}()
	getQuerier := func() logql.Querier {
		if !streamsChanged {
			return querier
		}

		if store != nil {
			store.close()
		}

		store = newTestingChunkStore(t)
		store.write(t, streams.get())
		store.flush(t)
		querier = store.querier()
		streamsChanged = false

		return querier
	}

	lines := strings.Split(script, "\n")

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(stripComment(lines[i]))
		if trimmed == "" {
			i++
			continue
		}

		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "clear":
			// Reset the loaded streams so the next scenario starts from a clean slate.
			streams = newStreamsParser()
			streamsChanged = true
			i++
		case "load":
			var err error
			i, err = parseLoadBlock(streams, lines, i+1)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			streamsChanged = true
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
			exp := expected.get()
			if err := exp.validate(); err != nil {
				t.Fatalf("%s: eval %q: %v", name, cmd.query, err)
			}
			runEval(t, name, getQuerier(), cmd, exp)
		default:
			t.Fatalf("%s: unexpected command %q", name, fields[0])
		}
	}
}

func runEval(t *testing.T, name string, querier logql.Querier, cmd evalCmd, exp expectations) {
	t.Helper()
	label := cmd.query
	if cmd.instant {
		label = "instant: " + label
	} else {
		label = "range: " + label
	}

	// Every scenario runs under both execution paths: the default per-timestamp order and the
	// opt-in per-stream order. Both must produce the expected result. Queries that are ineligible
	// for stream-first execution transparently fall back to the default path, so running them with
	// the flag on is still valid and must match.
	modes := []struct {
		name          string
		streamOrdered bool
	}{
		{"timestamp-first", false},
		{"stream-first", true},
	}

	t.Run(label, func(t *testing.T) {
		for _, mode := range modes {
			t.Run(mode.name, func(t *testing.T) {
				engine := logql.NewEngine(logql.EngineOpts{StreamOrderedExecutionEnabled: mode.streamOrdered}, querier, logql.NoLimits, log.NewNopLogger())

				var start, end, step time.Duration
				if cmd.instant {
					start, end, step = cmd.ts, cmd.ts, 0
				} else {
					start, end, step = cmd.start, cmd.end, cmd.step
				}

				ctx := user.InjectOrgID(context.Background(), tenant)

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
					case failMsg:
						require.Contains(t, err.Error(), exp.failText, "%s: failure message", name)
					case failRegex:
						require.Regexp(t, exp.failText, err.Error(), "%s: failure regex", name)
					}
					return
				}

				require.NoError(t, err, "%s: query %q", name, cmd.query)
				require.NoError(t, compareResult(name, cmd, exp, res.Data))
			})
		}
	})
}

// parseLoadBlock consumes a load command's indented data lines into p, returning the index of the
// next line to process. A block with no data lines is an error rather than a silently empty store.
func parseLoadBlock(p *streamsParser, lines []string, i int) (int, error) {
	loaded := 0
	var parseErr error
	next := consumeBlock(lines, i, func(content string) {
		if parseErr != nil {
			return
		}
		if err := p.parse(content); err != nil {
			parseErr = fmt.Errorf("invalid load line %q: %w", content, err)
			return
		}
		loaded++
	})
	if parseErr != nil {
		return next, parseErr
	}
	if loaded == 0 {
		return next, fmt.Errorf("load block has no data lines")
	}
	return next, nil
}

// consumeBlock feeds each indented, non-blank, non-comment line following a command to fn. It
// skips comment lines (indented or not) and stops at the first blank line, non-indented
// non-comment line, or EOF, returning the next line index.
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

// compareResult checks a query result against the expectation, returning a descriptive error on the
// first mismatch (nil on success). Keeping the comparators pure (error-returning rather than
// asserting on a *testing.T) lets the tests exercise the failure path directly.
func compareResult(name string, cmd evalCmd, exp expectations, data any) error {
	switch v := data.(type) {
	case promql.Scalar:
		if exp.empty {
			return fmt.Errorf("%s: expected an empty result, got scalar %v", name, v.V)
		}
		return compareScalar(name, exp, v)
	case promql.Vector:
		if exp.empty {
			if len(v) != 0 {
				return fmt.Errorf("%s: expected an empty result, got %v", name, v)
			}
			return nil
		}
		if exp.scalar != nil {
			return fmt.Errorf("%s: expected a scalar, got a vector", name)
		}
		return compareVector(name, cmd, exp, v)
	case promql.Matrix:
		if exp.empty {
			if len(v) != 0 {
				return fmt.Errorf("%s: expected an empty result, got %d series", name, len(v))
			}
			return nil
		}
		if exp.scalar != nil {
			return fmt.Errorf("%s: expected a scalar, got a matrix", name)
		}
		return compareMatrix(name, cmd, exp, v)
	default:
		return fmt.Errorf("%s: unsupported result type %T (only metric queries are supported)", name, data)
	}
}

func compareScalar(name string, exp expectations, s promql.Scalar) error {
	if exp.scalar == nil {
		return fmt.Errorf("%s: scalar result but no scalar value expected", name)
	}
	if !floatsEqual(*exp.scalar, s.V) {
		return fmt.Errorf("%s: scalar mismatch: want %v, got %v", name, *exp.scalar, s.V)
	}
	return nil
}

func compareVector(name string, cmd evalCmd, exp expectations, v promql.Vector) error {
	// Instant results carry a single timestamp: the query's evaluation time.
	wantTS := epoch.Add(cmd.ts).UnixMilli()

	// With `expect ordered` the series must appear in the given order (for sort/sort_desc).
	if exp.ordered {
		if len(v) != len(exp.series) {
			return fmt.Errorf("%s: series count mismatch: want %d, got %d", name, len(exp.series), len(v))
		}
		for i, es := range exp.series {
			if len(es.samples) != 1 {
				return fmt.Errorf("%s: instant result expects one value per series: %s", name, es.labels)
			}
			if !es.samples[0].present {
				return fmt.Errorf("%s: instant result cannot have a gap: %s", name, es.labels)
			}
			if es.labels != v[i].Metric.String() {
				return fmt.Errorf("%s: series at position %d: want %s, got %s", name, i, es.labels, v[i].Metric.String())
			}
			if v[i].T != wantTS {
				return fmt.Errorf("%s: series %s has timestamp %dms, expected %dms", name, v[i].Metric.String(), v[i].T, wantTS)
			}
			if !floatsEqual(es.samples[0].value, v[i].F) {
				return fmt.Errorf("%s: series %s (position %d) value mismatch: want %v, got %v", name, es.labels, i, es.samples[0].value, v[i].F)
			}
		}
		return nil
	}

	want := map[string]float64{}
	for _, es := range exp.series {
		if len(es.samples) != 1 {
			return fmt.Errorf("%s: instant result expects one value per series: %s", name, es.labels)
		}
		if !es.samples[0].present {
			return fmt.Errorf("%s: instant result cannot have a gap: %s", name, es.labels)
		}
		if _, dup := want[es.labels]; dup {
			return fmt.Errorf("%s: duplicate expected series %s", name, es.labels)
		}
		want[es.labels] = es.samples[0].value
	}

	got := map[string]float64{}
	for _, s := range v {
		key := s.Metric.String()
		if s.T != wantTS {
			return fmt.Errorf("%s: series %s has timestamp %dms, expected %dms", name, key, s.T, wantTS)
		}
		if _, dup := got[key]; dup {
			return fmt.Errorf("%s: engine returned duplicate series %s", name, key)
		}
		got[key] = s.F
	}

	if len(got) != len(want) {
		return fmt.Errorf("%s: series count mismatch: want %v, got %v", name, want, got)
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return fmt.Errorf("%s: missing expected series %s; got %v", name, k, got)
		}
		if !floatsEqual(wv, gv) {
			return fmt.Errorf("%s: series %s value mismatch: want %v, got %v", name, k, wv, gv)
		}
	}
	return nil
}

func compareMatrix(name string, cmd evalCmd, exp expectations, m promql.Matrix) error {
	if exp.ordered {
		return fmt.Errorf("%s: `expect ordered` is only supported for instant queries", name)
	}

	// Timestamps (ms) of each range step.
	var stepMillis []int64
	for ts := cmd.start; ts <= cmd.end; ts += cmd.step {
		stepMillis = append(stepMillis, epoch.Add(ts).UnixMilli())
		if cmd.step == 0 {
			break
		}
	}
	stepMillisSet := make(map[int64]struct{}, len(stepMillis))
	for _, ms := range stepMillis {
		stepMillisSet[ms] = struct{}{}
	}

	want := map[string][]sample{}
	for _, es := range exp.series {
		if len(es.samples) != len(stepMillis) {
			return fmt.Errorf("%s: series %s has %d points, expected %d steps", name, es.labels, len(es.samples), len(stepMillis))
		}
		if _, dup := want[es.labels]; dup {
			return fmt.Errorf("%s: duplicate expected series %s", name, es.labels)
		}
		want[es.labels] = es.samples
	}

	got := map[string]map[int64]float64{}
	for _, s := range m {
		key := s.Metric.String()
		if _, dup := got[key]; dup {
			return fmt.Errorf("%s: engine returned duplicate series %s", name, key)
		}
		byTS := map[int64]float64{}
		for _, p := range s.Floats {
			if _, ok := stepMillisSet[p.T]; !ok {
				return fmt.Errorf("%s: series %s has point at unexpected timestamp t=%dms", name, key, p.T)
			}
			if _, dup := byTS[p.T]; dup {
				return fmt.Errorf("%s: engine returned duplicate point for series %s at t=%dms", name, key, p.T)
			}
			byTS[p.T] = p.F
		}
		got[key] = byTS
	}

	if len(got) != len(want) {
		return fmt.Errorf("%s: series count mismatch: want %d, got %d (%v)", name, len(want), len(got), keys(got))
	}
	for k, points := range want {
		gotTS, ok := got[k]
		if !ok {
			return fmt.Errorf("%s: missing expected series %s; got series %v", name, k, keys(got))
		}
		for i, p := range points {
			ts := stepMillis[i]
			gv, has := gotTS[ts]
			if !p.present {
				if has {
					return fmt.Errorf("%s: series %s step %d (t=%dms) should be empty, got %v", name, k, i, ts, gv)
				}
				continue
			}
			if !has {
				return fmt.Errorf("%s: series %s missing point at step %d (t=%dms)", name, k, i, ts)
			}
			if !floatsEqual(p.value, gv) {
				return fmt.Errorf("%s: series %s step %d value mismatch: want %v, got %v", name, k, i, p.value, gv)
			}
		}
	}
	return nil
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
