package logqltest

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// TestLogQLScripts runs every declarative `.logqltest` script under testdata/ through the
// logqltest harness. See README.md for the DSL.
func TestLogQLScripts(t *testing.T) {
	files, err := filepath.Glob("testdata/*.logqltest")
	require.NoError(t, err)
	require.NotEmpty(t, files, "no .logqltest scripts found under testdata/")

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			// Different scripts are independent (each builds its own store).
			t.Parallel()

			b, err := os.ReadFile(f)
			require.NoError(t, err)
			RunScript(t, filepath.Base(f), string(b))
		})
	}
}

func TestRunScript_ShouldSupportClear(t *testing.T) {
	// Verify that `clear` resets the loaded streams between scenarios.
	RunScript(t, "clear", `
load
  {app="foo"} "x" @ 30s

clear

load
  {app="bar"} "x" @ 30s

eval instant at 60s count_over_time({app=~"foo|bar"}[1m])
  {app="bar"} 1
`)
}

func TestParseLoadBlock(t *testing.T) {
	// A load block with at least one data line loads it and advances past the block.
	p := newStreamsParser()
	next, err := parseLoadBlock(p, []string{"load", `  {app="foo"} "x" @ 0s`}, 1)
	require.NoError(t, err)
	require.Equal(t, 2, next)
	require.Len(t, p.get(), 1)

	// A load block with no data lines is rejected rather than silently loading nothing.
	_, err = parseLoadBlock(newStreamsParser(), []string{"load", "", `eval instant at 0s vector(1)`}, 1)
	require.Error(t, err)

	// A malformed data line is surfaced as an error.
	_, err = parseLoadBlock(newStreamsParser(), []string{"load", `  {app="foo"} "x"`}, 1) // no timestamp
	require.Error(t, err)
}

func TestRunScript_ExpectEmpty(t *testing.T) {
	// A query matching nothing returns an empty result, asserted explicitly with `expect empty`.
	RunScript(t, "empty", `
load
  {app="a"} "x" @ 30s

eval instant at 60s count_over_time({app="missing"}[1m])
  expect empty
`)
}

func TestRunScript_ExpectFailRegex(t *testing.T) {
	// `expect fail regex:` matches the error message against a pattern.
	RunScript(t, "regex", `
load
  {app="foo", machine="fuzz"} "x" @ 0s [repeat every 10s for 7]
  {app="foo", machine="buzz"} "x" @ 0s [repeat every 10s for 7]

eval instant at 60s sum by (app,machine) (count_over_time({app="foo"}[1m])) > bool ignoring (machine) sum by (app) (count_over_time({app="foo"}[1m]))
  expect fail regex: many-to-one.*group_left
`)
}

// TestComparators_DetectMismatches proves the harness actually fails on wrong results — the
// property the whole oracle depends on. It covers value/series mismatches, duplicate series,
// extra matrix points, and a non-empty result against `expect empty`.
func TestComparators_DetectMismatches(t *testing.T) {
	foo := labels.FromStrings("app", "a")
	rangeCmd := evalCmd{mode: evalRange, start: time.Minute, end: time.Minute, step: time.Minute}
	instantCmd := evalCmd{mode: evalInstant, ts: time.Minute}
	ts := epoch.Add(time.Minute).UnixMilli()
	scalar := func(v float64) expectations { return expectations{scalar: &v} }

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"scalar value": {
			err:  compareScalar("n", scalar(5), promql.Scalar{V: 6}, false, defaultEpsilon),
			want: "scalar mismatch: want 5, got 6",
		},
		"vector value": {
			err:  compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: foo, F: 6, T: ts}}, false, defaultEpsilon),
			want: `series {app="a"} value mismatch: want 5, got 6`,
		},
		"vector wrong timestamp": {
			err:  compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: foo, F: 5, T: 1000}}, false, defaultEpsilon),
			want: fmt.Sprintf(`series {app="a"} has timestamp 1000ms, expected %dms`, ts),
		},
		"vector ordered wrong timestamp": {
			err:  compareVector("n", instantCmd, expectations{ordered: true, series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 5, T: 1000}}, false, defaultEpsilon),
			want: fmt.Sprintf(`series {app="a"} has timestamp 1000ms, expected %dms`, ts),
		},
		"vector missing series": {
			err:  compareVector("n", instantCmd, oneSeries(5), promql.Vector{}, false, defaultEpsilon),
			want: `series count mismatch: want map[{app="a"}:5], got map[]`,
		},
		"vector extra series": {
			err:  compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: foo, F: 5, T: ts}, {Metric: labels.FromStrings("app", "b"), F: 9, T: ts}}, false, defaultEpsilon),
			want: `series count mismatch: want map[{app="a"}:5], got map[{app="a"}:5 {app="b"}:9]`,
		},
		"vector duplicate result series": {
			err:  compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: foo, F: 5, T: ts}, {Metric: foo, F: 5, T: ts}}, false, defaultEpsilon),
			want: `engine returned duplicate series {app="a"}`,
		},
		"matrix value": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 6}}}}, false, defaultEpsilon),
			want: `series {app="a"} step 0 value mismatch: want 5, got 6`,
		},
		"matrix missing point": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{}}}, false, defaultEpsilon),
			want: `series {app="a"} missing point at step 0`,
		},
		"matrix point at unexpected timestamp": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts + 1000, F: 5}}}}, false, defaultEpsilon),
			want: `series {app="a"} has point at unexpected timestamp`,
		},
		"matrix duplicate result series": {
			err: compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{
				{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}},
				{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}},
			}, false, defaultEpsilon),
			want: `engine returned duplicate series {app="a"}`,
		},
		"matrix duplicate point": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}, {T: ts, F: 5}}}}, false, defaultEpsilon),
			want: `engine returned duplicate point for series {app="a"}`,
		},
		"empty expected but non-empty result": {
			err:  compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, expectations{empty: true}, promql.Vector{{Metric: foo, F: 5}}, false, defaultEpsilon),
			want: "expected an empty result",
		},
		"scalar expected but empty vector": {
			err:  compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, scalar(5), promql.Vector{}, false, defaultEpsilon),
			want: "expected a scalar, got a vector",
		},
		"scalar expected but empty matrix": {
			err:  compareResult("n", rangeCmd, scalar(5), promql.Matrix{}, false, defaultEpsilon),
			want: "expected a scalar, got a matrix",
		},
		"empty expected but scalar result": {
			err:  compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, expectations{empty: true}, promql.Scalar{V: 5}, false, defaultEpsilon),
			want: "expected an empty result, got scalar 5",
		},
		"empty expected but non-empty matrix": {
			err:  compareResult("n", rangeCmd, expectations{empty: true}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: "expected an empty result, got 1 series",
		},
		"scalar result but no scalar expected": {
			err:  compareScalar("n", expectations{}, promql.Scalar{V: 5}, false, defaultEpsilon),
			want: "scalar result but no scalar value expected",
		},
		"vector missing series (count matches)": {
			err:  compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: labels.FromStrings("app", "b"), F: 5, T: ts}}, false, defaultEpsilon),
			want: `missing expected series {app="a"}`,
		},
		"matrix missing series (count matches)": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: labels.FromStrings("app", "b"), Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: `missing expected series {app="a"}`,
		},
		"matrix expected points vs steps": {
			err:  compareMatrix("n", rangeCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}, {present: true, value: 6}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: "has 2 points, expected 1 steps",
		},
		"matrix gap but value present": {
			err:  compareMatrix("n", rangeCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: false}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: "should be empty, got 5",
		},
		"duplicate expected vector series": {
			err:  compareVector("n", instantCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}, {labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 5}}, false, defaultEpsilon),
			want: `duplicate expected series {app="a"}`,
		},
		"duplicate expected matrix series": {
			err:  compareMatrix("n", rangeCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}, {labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: `duplicate expected series {app="a"}`,
		},
		"ordered value mismatch": {
			err:  compareVector("n", instantCmd, expectations{ordered: true, series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 6, T: ts}}, false, defaultEpsilon),
			want: `series {app="a"} (position 0) value mismatch: want 5, got 6`,
		},
		"ordered count mismatch": {
			err:  compareVector("n", instantCmd, expectations{ordered: true, series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 5}, {Metric: labels.FromStrings("app", "b"), F: 5}}, false, defaultEpsilon),
			want: "series count mismatch: want 1, got 2",
		},
		"ordered position mismatch": {
			err: compareVector("n", instantCmd, expectations{ordered: true, series: []expectedSeries{
				{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}},
				{labels: `{app="b"}`, samples: []sample{{present: true, value: 6}}},
			}}, promql.Vector{{Metric: labels.FromStrings("app", "b"), F: 6}, {Metric: foo, F: 5}}, false, defaultEpsilon),
			want: `series at position 0: want {app="a"}, got {app="b"}`,
		},
		"matrix ordered unsupported": {
			err:  compareMatrix("n", rangeCmd, expectations{ordered: true, series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: "`expect ordered` is only supported for instant queries",
		},
		"unsupported result type": {
			err:  compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, oneSeries(5), nil, false, defaultEpsilon),
			want: "unsupported result type",
		},
		"streams line mismatch": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "b"}}},
			}, false),
			want: `stream {app="a"} line 0 mismatch: want "a", got "b"`,
		},
		"streams timestamp mismatch": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch.Add(time.Second), Line: "a"}}},
			}, false),
			want: fmt.Sprintf(`stream {app="a"} line 0 has timestamp %dms, expected %dms`, epoch.Add(time.Second).UnixMilli(), epoch.UnixMilli()),
		},
		"streams missing stream": {
			err:  compareStreams("n", oneStream("a"), logqlmodel.Streams{}, false),
			want: `stream count mismatch: want 1, got 0`,
		},
		"streams extra stream": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
				{Labels: `{app="b"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "x"}}},
			}, false),
			want: `stream count mismatch: want 1, got 2`,
		},
		"streams missing stream (count matches)": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="b"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "x"}}},
			}, false),
			want: `missing expected stream {app="a"}`,
		},
		"streams extra line": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}, {Timestamp: epoch, Line: "extra"}}},
			}, false),
			want: `stream {app="a"} has 2 lines, expected 1`,
		},
		"streams missing line": {
			err: compareStreams("n", expectations{streams: []expectedStream{
				{labels: `{app="a"}`, entries: []expectedLogEntry{{ts: 0, line: "a"}, {ts: time.Second, line: "b"}}},
			}}, logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
			}, false),
			want: `stream {app="a"} has 1 lines, expected 2`,
		},
		"streams unexpected structured metadata": {
			// A line with no `[metadata ...]` clause asserts the entry carries no metadata.
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", clauseAdapters("lvl", "error"), nil)}},
			}, false),
			want: `stream {app="a"} line 0 structured metadata mismatch: want {}, got {lvl="error"}`,
		},
		"streams missing structured metadata": {
			err: compareStreams("n", oneStreamWithMetadata("a", "lvl", "error"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", nil, nil)}},
			}, false),
			want: `stream {app="a"} line 0 structured metadata mismatch: want {lvl="error"}, got {}`,
		},
		"streams structured metadata value mismatch": {
			err: compareStreams("n", oneStreamWithMetadata("a", "lvl", "error"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", clauseAdapters("lvl", "info"), nil)}},
			}, false),
			want: `structured metadata mismatch: want {lvl="error"}, got {lvl="info"}`,
		},
		"streams structured metadata mis-categorized as parsed": {
			// Both categories carry the same pair, so only comparing them apart tells the two results
			// apart — this is the mistake `| label_format` can make.
			err: compareStreams("n", oneStreamWithMetadata("a", "lvl", "error"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", nil, clauseAdapters("lvl", "error"))}},
			}, false),
			want: `structured metadata mismatch: want {lvl="error"}, got {}`,
		},
		"streams unexpected parsed label": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", nil, clauseAdapters("lvl", "error"))}},
			}, false),
			want: `stream {app="a"} line 0 parsed labels mismatch: want {}, got {lvl="error"}`,
		},
		"streams parsed label value mismatch": {
			err: compareStreams("n", oneStreamWithParsed("a", "lvl", "error"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", nil, clauseAdapters("lvl", "info"))}},
			}, false),
			want: `parsed labels mismatch: want {lvl="error"}, got {lvl="info"}`,
		},
		"streams parsed label mis-categorized as metadata": {
			err: compareStreams("n", oneStreamWithParsed("a", "lvl", "error"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("a", clauseAdapters("lvl", "error"), nil)}},
			}, false),
			want: `structured metadata mismatch: want {}, got {lvl="error"}`,
		},
		"streams duplicate expected stream": {
			err: compareStreams("n", expectations{streams: []expectedStream{
				{labels: `{app="a"}`, entries: []expectedLogEntry{{ts: 0, line: "a"}}},
				{labels: `{app="a"}`, entries: []expectedLogEntry{{ts: 0, line: "a"}}},
			}}, logqlmodel.Streams{{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}}}, false),
			want: `duplicate expected stream {app="a"}`,
		},
		"streams duplicate result stream": {
			err: compareStreams("n", oneStream("a"), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
			}, false),
			want: `engine returned duplicate stream {app="a"}`,
		},
		"vector expected but got streams": {
			err:  compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, oneSeries(5), logqlmodel.Streams{}, false, defaultEpsilon),
			want: "expected series, got log streams",
		},
		"streams expected but got vector": {
			err:  compareVector("n", instantCmd, oneStream("a"), promql.Vector{{Metric: foo, F: 5}}, false, defaultEpsilon),
			want: "expected log streams, got a vector",
		},
		"streams expected but got matrix": {
			err:  compareMatrix("n", rangeCmd, oneStream("a"), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon),
			want: "expected log streams, got a matrix",
		},
		"streams expected but got scalar": {
			err:  compareScalar("n", oneStream("a"), promql.Scalar{V: 5}, false, defaultEpsilon),
			want: "expected log streams, got a scalar",
		},
		"series expected but got scalar": {
			err:  compareScalar("n", oneSeries(5), promql.Scalar{V: 5}, false, defaultEpsilon),
			want: "expected series, got a scalar",
		},
		"scalar expected but got streams": {
			err: compareStreams("n", scalar(5), logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
			}, false),
			want: "expected a scalar, got log streams",
		},
		"empty expected but non-empty streams": {
			err: compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, expectations{empty: true}, logqlmodel.Streams{
				{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
			}, false, defaultEpsilon),
			want: "expected an empty result, got 1 streams",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, tc.err, tc.want)
		})
	}
}

func TestComparators_AcceptMatches(t *testing.T) {
	foo := labels.FromStrings("app", "a")
	scalar := 5.0
	rangeCmd := evalCmd{mode: evalRange, start: time.Minute, end: time.Minute, step: time.Minute}
	instantCmd := evalCmd{mode: evalInstant, ts: time.Minute}
	ts := epoch.Add(time.Minute).UnixMilli()

	require.NoError(t, compareScalar("n", expectations{scalar: &scalar}, promql.Scalar{V: 5}, false, defaultEpsilon))
	require.NoError(t, compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: foo, F: 5, T: ts}}, false, defaultEpsilon))
	require.NoError(t, compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon))
	require.NoError(t, compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, expectations{empty: true}, promql.Vector{}, false, defaultEpsilon))

	// A matrix gap (`_`) matches a step the engine legitimately omitted.
	require.NoError(t, compareMatrix("n",
		evalCmd{mode: evalRange, start: time.Minute, end: 2 * time.Minute, step: time.Minute},
		expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}, {present: false}}}}},
		promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, false, defaultEpsilon))

	// Ordered series in the expected order.
	require.NoError(t, compareVector("n", instantCmd,
		expectations{ordered: true, series: []expectedSeries{
			{labels: `{app="a"}`, samples: []sample{{present: true, value: 1}}},
			{labels: `{app="b"}`, samples: []sample{{present: true, value: 2}}},
		}},
		promql.Vector{{Metric: foo, F: 1, T: ts}, {Metric: labels.FromStrings("app", "b"), F: 2, T: ts}}, false, defaultEpsilon))

	// Log streams: matched as a set by label, lines compared in order within each stream.
	require.NoError(t, compareStreams("n",
		expectations{streams: []expectedStream{
			{labels: `{app="a"}`, entries: []expectedLogEntry{{ts: 0, line: "1st"}, {ts: time.Second, line: "2nd"}}},
			{labels: `{app="b"}`, entries: []expectedLogEntry{{ts: 0, line: "x"}}},
		}},
		logqlmodel.Streams{
			{Labels: `{app="b"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "x"}}},
			{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "1st"}, {Timestamp: epoch.Add(time.Second), Line: "2nd"}}},
		}, false))
	require.NoError(t, compareResult("n", evalCmd{mode: evalInstant, ts: time.Minute}, expectations{empty: true}, logqlmodel.Streams{}, false, defaultEpsilon))

	// A `[metadata ...]` expectation matches whatever order the pairs come back in.
	require.NoError(t, compareStreams("n",
		expectations{streams: []expectedStream{{
			labels:  `{app="a", lvl="error", pod="p1"}`,
			entries: []expectedLogEntry{{ts: 0, line: "x", metadata: labels.FromStrings("lvl", "error", "pod", "p1")}},
		}}},
		logqlmodel.Streams{{Labels: `{app="a", lvl="error", pod="p1"}`, Entries: []push.Entry{
			gotEntry("x", []logproto.LabelAdapter{{Name: "pod", Value: "p1"}, {Name: "lvl", Value: "error"}}, nil),
		}}}, false))
}

func TestComparators_SkipValues(t *testing.T) {
	var (
		foo        = labels.FromStrings("app", "a")
		bar        = labels.FromStrings("app", "b")
		instantCmd = evalCmd{mode: evalInstant, ts: time.Minute}
		rangeCmd   = evalCmd{mode: evalRange, start: time.Minute, end: time.Minute, step: time.Minute}
		ts         = epoch.Add(time.Minute).UnixMilli()
		scalar     = func(v float64) expectations { return expectations{scalar: &v} }
	)

	t.Run("value mismatches are ignored", func(t *testing.T) {
		require.NoError(t, compareScalar("n", scalar(5), promql.Scalar{V: 6}, true, defaultEpsilon))
		require.NoError(t, compareVector("n", instantCmd, oneSeries(5), promql.Vector{{Metric: foo, F: 6, T: ts}}, true, defaultEpsilon))
		require.NoError(t, compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 6}}}}, true, defaultEpsilon))
		require.NoError(t, compareStreams("n", oneStream("a"),
			logqlmodel.Streams{{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "wrong line"}}}}, true))
	})

	// The values below are deliberately wrong (6, not 5): the shape check must still fire.
	t.Run("sample count mismatches still error", func(t *testing.T) {
		// Vector: too few, too many, or a differently-labelled series.
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(5), promql.Vector{}, true, defaultEpsilon), "series count mismatch")
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(5),
			promql.Vector{{Metric: foo, F: 6, T: ts}, {Metric: bar, F: 6, T: ts}}, true, defaultEpsilon), "series count mismatch")
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(5),
			promql.Vector{{Metric: bar, F: 6, T: ts}}, true, defaultEpsilon), "missing expected series")

		// Matrix: a step the engine omitted, or a gap the engine filled.
		require.ErrorContains(t, compareMatrix("n", rangeCmd, oneSeries(5),
			promql.Matrix{{Metric: foo, Floats: []promql.FPoint{}}}, true, defaultEpsilon), "missing point at step 0")
		require.ErrorContains(t, compareMatrix("n", rangeCmd,
			expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: false}}}}},
			promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}, true, defaultEpsilon), "should be empty")

		// Streams: a missing stream, or a missing line within one, still errors even though a
		// present line's text would have been skipped.
		require.ErrorContains(t, compareStreams("n", oneStream("a"), logqlmodel.Streams{}, true), "stream count mismatch")
		require.ErrorContains(t, compareStreams("n", expectations{streams: []expectedStream{
			{labels: `{app="a"}`, entries: []expectedLogEntry{{ts: 0, line: "a"}, {ts: time.Second, line: "b"}}},
		}}, logqlmodel.Streams{
			{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch, Line: "a"}}},
		}, true), "has 1 lines, expected 2")
	})

	t.Run("structured metadata mismatches still error", func(t *testing.T) {
		// Metadata identifies an entry rather than valuing it, so it is checked even here.
		require.ErrorContains(t, compareStreams("n", oneStream("a"), logqlmodel.Streams{
			{Labels: `{app="a"}`, Entries: []push.Entry{gotEntry("wrong line", clauseAdapters("lvl", "error"), nil)}},
		}, true), `structured metadata mismatch: want {}, got {lvl="error"}`)
	})

	t.Run("timestamp mismatches still error", func(t *testing.T) {
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(5),
			promql.Vector{{Metric: foo, F: 6, T: 1000}}, true, defaultEpsilon), "has timestamp 1000ms")
		require.ErrorContains(t, compareMatrix("n", rangeCmd, oneSeries(5),
			promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts + 1000, F: 6}}}}, true, defaultEpsilon), "unexpected timestamp")
		require.ErrorContains(t, compareStreams("n", oneStream("a"),
			logqlmodel.Streams{{Labels: `{app="a"}`, Entries: []push.Entry{{Timestamp: epoch.Add(time.Second), Line: "a"}}}}, true),
			fmt.Sprintf("has timestamp %dms", epoch.Add(time.Second).UnixMilli()))
	})
}

func oneStream(line string) expectations {
	return expectations{streams: []expectedStream{{labels: `{app="a"}`, entries: []expectedLogEntry{{ts: 0, line: line}}}}}
}

func TestComparators_ValuesToleration(t *testing.T) {
	var (
		foo        = labels.FromStrings("app", "a")
		instantCmd = evalCmd{mode: evalInstant, ts: time.Minute}
		rangeCmd   = evalCmd{mode: evalRange, start: time.Minute, end: time.Minute, step: time.Minute}
		ts         = epoch.Add(time.Minute).UnixMilli()
		scalar     = func(v float64) expectations { return expectations{scalar: &v} }
	)

	t.Run("a value within the given tolerance passes", func(t *testing.T) {
		require.NoError(t, compareScalar("n", scalar(100), promql.Scalar{V: 102}, false, 0.02))
		require.NoError(t, compareVector("n", instantCmd, oneSeries(100), promql.Vector{{Metric: foo, F: 102, T: ts}}, false, 0.02))
		require.NoError(t, compareMatrix("n", rangeCmd, oneSeries(100), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 102}}}}, false, 0.02))
	})

	t.Run("a value outside the given tolerance still fails", func(t *testing.T) {
		require.ErrorContains(t, compareScalar("n", scalar(100), promql.Scalar{V: 103}, false, 0.02), "scalar mismatch")
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(100), promql.Vector{{Metric: foo, F: 103, T: ts}}, false, 0.02), "value mismatch")
		require.ErrorContains(t, compareMatrix("n", rangeCmd, oneSeries(100), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 103}}}}, false, 0.02), "value mismatch")
	})

	t.Run("shape checks still fire regardless of tolerance", func(t *testing.T) {
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(100), promql.Vector{}, false, 0.02), "series count mismatch")
		require.ErrorContains(t, compareVector("n", instantCmd, oneSeries(100),
			promql.Vector{{Metric: foo, F: 102, T: 1000}}, false, 0.02), "has timestamp 1000ms")
	})
}

func TestEffectiveEpsilon(t *testing.T) {
	exp := expectations{valuesToleration: map[string]float64{directStackName: 0.1}}

	require.Equal(t, 0.1, effectiveEpsilon(exp, directStackName))
	// A stack not named in a values-toleration directive keeps the tight default, rather than
	// inheriting a toleration meant for a different stack.
	require.Equal(t, defaultEpsilon, effectiveEpsilon(exp, queryFrontendShardStackName))
	require.Equal(t, defaultEpsilon, effectiveEpsilon(expectations{}, directStackName))
}

// oneStreamWithMetadata builds a single-stream, single-line expectation carrying one structured
// metadata pair. A categorized result keeps metadata out of the stream labels, so the label set
// stays the bare `{app="a"}`.
func oneStreamWithMetadata(line, key, value string) expectations {
	return expectations{streams: []expectedStream{{
		labels:  `{app="a"}`,
		entries: []expectedLogEntry{{ts: 0, line: line, metadata: labels.FromStrings(key, value)}},
	}}}
}

// oneStreamWithParsed builds the same shape carrying one parsed label instead.
func oneStreamWithParsed(line, key, value string) expectations {
	return expectations{streams: []expectedStream{{
		labels:  `{app="a"}`,
		entries: []expectedLogEntry{{ts: 0, line: line, parsed: labels.FromStrings(key, value)}},
	}}}
}

// gotEntry builds a result entry at the script epoch with the given categorized labels.
func gotEntry(line string, structuredMetadata, parsed []logproto.LabelAdapter) push.Entry {
	return push.Entry{Timestamp: epoch, Line: line, StructuredMetadata: structuredMetadata, Parsed: parsed}
}

func clauseAdapters(key, value string) []logproto.LabelAdapter {
	return []logproto.LabelAdapter{{Name: key, Value: value}}
}

func TestFloatsEqual(t *testing.T) {
	require.True(t, floatsEqual(1, 1, defaultEpsilon))
	require.True(t, floatsEqual(1, 1+1e-12, defaultEpsilon))   // within absolute epsilon
	require.True(t, floatsEqual(1e10, 1e10+1, defaultEpsilon)) // within relative epsilon
	require.True(t, floatsEqual(math.NaN(), math.NaN(), defaultEpsilon))
	require.True(t, floatsEqual(math.Inf(1), math.Inf(1), defaultEpsilon))
	require.False(t, floatsEqual(1, 1.1, defaultEpsilon))
	require.False(t, floatsEqual(100, 101, defaultEpsilon))
	require.False(t, floatsEqual(math.NaN(), 1, defaultEpsilon))
	require.False(t, floatsEqual(math.Inf(1), math.Inf(-1), defaultEpsilon))

	// A custom epsilon widens (or narrows) the bound.
	require.True(t, floatsEqual(100, 102, 0.02))
	require.False(t, floatsEqual(100, 103, 0.02))
}

func oneSeries(val float64) expectations {
	return expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: val}}}}}
}
