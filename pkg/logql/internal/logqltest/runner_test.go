package logqltest

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
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
	rangeCmd := evalCmd{start: time.Minute, end: time.Minute, step: time.Minute}
	ts := epoch.Add(time.Minute).UnixMilli()
	scalar := func(v float64) expectations { return expectations{scalar: &v} }

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"scalar value": {
			err:  compareScalar("n", scalar(5), promql.Scalar{V: 6}),
			want: "scalar mismatch: want 5, got 6",
		},
		"vector value": {
			err:  compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 6}}),
			want: `series {app="a"} value mismatch: want 5, got 6`,
		},
		"vector missing series": {
			err:  compareVector("n", oneSeries(5), promql.Vector{}),
			want: `series count mismatch: want map[{app="a"}:5], got map[]`,
		},
		"vector extra series": {
			err:  compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 5}, {Metric: labels.FromStrings("app", "b"), F: 9}}),
			want: `series count mismatch: want map[{app="a"}:5], got map[{app="a"}:5 {app="b"}:9]`,
		},
		"vector duplicate result series": {
			err:  compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 5}, {Metric: foo, F: 5}}),
			want: `engine returned duplicate series {app="a"}`,
		},
		"matrix value": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 6}}}}),
			want: `series {app="a"} step 0 value mismatch: want 5, got 6`,
		},
		"matrix missing point": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{}}}),
			want: `series {app="a"} missing point at step 0`,
		},
		"matrix extra point": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}, {T: ts + 1000, F: 7}}}}),
			want: `series {app="a"} has 2 points, expected 1`,
		},
		"matrix duplicate result series": {
			err: compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{
				{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}},
				{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}},
			}),
			want: `engine returned duplicate series {app="a"}`,
		},
		"empty expected but non-empty result": {
			err:  compareResult("n", evalCmd{instant: true, ts: time.Minute}, expectations{empty: true}, promql.Vector{{Metric: foo, F: 5}}),
			want: "expected an empty result",
		},
		"scalar expected but empty vector": {
			err:  compareResult("n", evalCmd{instant: true, ts: time.Minute}, scalar(5), promql.Vector{}),
			want: "expected a scalar, got a vector",
		},
		"scalar expected but empty matrix": {
			err:  compareResult("n", rangeCmd, scalar(5), promql.Matrix{}),
			want: "expected a scalar, got a matrix",
		},
		"empty expected but scalar result": {
			err:  compareResult("n", evalCmd{instant: true, ts: time.Minute}, expectations{empty: true}, promql.Scalar{V: 5}),
			want: "expected an empty result, got scalar 5",
		},
		"empty expected but non-empty matrix": {
			err:  compareResult("n", rangeCmd, expectations{empty: true}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}),
			want: "expected an empty result, got 1 series",
		},
		"scalar result but no scalar expected": {
			err:  compareScalar("n", expectations{}, promql.Scalar{V: 5}),
			want: "scalar result but no scalar value expected",
		},
		"vector missing series (count matches)": {
			err:  compareVector("n", oneSeries(5), promql.Vector{{Metric: labels.FromStrings("app", "b"), F: 5}}),
			want: `missing expected series {app="a"}`,
		},
		"matrix missing series (count matches)": {
			err:  compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: labels.FromStrings("app", "b"), Floats: []promql.FPoint{{T: ts, F: 5}}}}),
			want: `missing expected series {app="a"}`,
		},
		"matrix expected points vs steps": {
			err:  compareMatrix("n", rangeCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}, {present: true, value: 6}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}),
			want: "has 2 points, expected 1 steps",
		},
		"matrix gap but value present": {
			err:  compareMatrix("n", rangeCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: false}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}),
			want: "should be empty, got 5",
		},
		"duplicate expected vector series": {
			err:  compareVector("n", expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}, {labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 5}}),
			want: `duplicate expected series {app="a"}`,
		},
		"duplicate expected matrix series": {
			err:  compareMatrix("n", rangeCmd, expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}, {labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}),
			want: `duplicate expected series {app="a"}`,
		},
		"ordered value mismatch": {
			err:  compareVector("n", expectations{ordered: true, series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 6}}),
			want: `series {app="a"} (position 0) value mismatch: want 5, got 6`,
		},
		"ordered count mismatch": {
			err:  compareVector("n", expectations{ordered: true, series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}}}}}, promql.Vector{{Metric: foo, F: 5}, {Metric: labels.FromStrings("app", "b"), F: 5}}),
			want: "series count mismatch: want 1, got 2",
		},
		"unsupported result type": {
			err:  compareResult("n", evalCmd{instant: true, ts: time.Minute}, oneSeries(5), nil),
			want: "unsupported result type",
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
	rangeCmd := evalCmd{start: time.Minute, end: time.Minute, step: time.Minute}
	ts := epoch.Add(time.Minute).UnixMilli()

	require.NoError(t, compareScalar("n", expectations{scalar: &scalar}, promql.Scalar{V: 5}))
	require.NoError(t, compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 5}}))
	require.NoError(t, compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}))
	require.NoError(t, compareResult("n", evalCmd{instant: true, ts: time.Minute}, expectations{empty: true}, promql.Vector{}))

	// A matrix gap (`_`) matches a step the engine legitimately omitted.
	require.NoError(t, compareMatrix("n",
		evalCmd{start: time.Minute, end: 2 * time.Minute, step: time.Minute},
		expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: 5}, {present: false}}}}},
		promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}}}))

	// Ordered series in the expected order.
	require.NoError(t, compareVector("n",
		expectations{ordered: true, series: []expectedSeries{
			{labels: `{app="a"}`, samples: []sample{{present: true, value: 1}}},
			{labels: `{app="b"}`, samples: []sample{{present: true, value: 2}}},
		}},
		promql.Vector{{Metric: foo, F: 1}, {Metric: labels.FromStrings("app", "b"), F: 2}}))
}

func TestFloatsEqual(t *testing.T) {
	require.True(t, floatsEqual(1, 1))
	require.True(t, floatsEqual(1, 1+1e-12))   // within absolute epsilon
	require.True(t, floatsEqual(1e10, 1e10+1)) // within relative epsilon
	require.True(t, floatsEqual(math.NaN(), math.NaN()))
	require.True(t, floatsEqual(math.Inf(1), math.Inf(1)))
	require.False(t, floatsEqual(1, 1.1))
	require.False(t, floatsEqual(100, 101))
	require.False(t, floatsEqual(math.NaN(), 1))
	require.False(t, floatsEqual(math.Inf(1), math.Inf(-1)))
}

func oneSeries(val float64) expectations {
	return expectations{series: []expectedSeries{{labels: `{app="a"}`, samples: []sample{{present: true, value: val}}}}}
}
