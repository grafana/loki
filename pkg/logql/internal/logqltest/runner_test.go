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

// TestComparators_DetectMismatches proves the harness actually fails on wrong results — the
// property the whole oracle depends on. It covers value/series mismatches, duplicate series,
// extra matrix points, and a non-empty result against `expect empty`.
func TestComparators_DetectMismatches(t *testing.T) {
	foo := labels.FromStrings("app", "a")
	rangeCmd := evalCmd{start: time.Minute, end: time.Minute, step: time.Minute}
	ts := epoch.Add(time.Minute).UnixMilli()
	scalar := func(v float64) expectations { return expectations{scalar: &v} }

	for name, err := range map[string]error{
		"scalar value":                   compareScalar("n", scalar(5), promql.Scalar{V: 6}),
		"vector value":                   compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 6}}),
		"vector missing series":          compareVector("n", oneSeries(5), promql.Vector{}),
		"vector extra series":            compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 5}, {Metric: labels.FromStrings("app", "b"), F: 9}}),
		"vector duplicate result series": compareVector("n", oneSeries(5), promql.Vector{{Metric: foo, F: 5}, {Metric: foo, F: 5}}),
		"matrix value":                   compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 6}}}}),
		"matrix missing point":           compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{}}}),
		"matrix extra point":             compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}, {T: ts + 1000, F: 7}}}}),
		"matrix duplicate result series": compareMatrix("n", rangeCmd, oneSeries(5), promql.Matrix{
			{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}},
			{Metric: foo, Floats: []promql.FPoint{{T: ts, F: 5}}},
		}),
		"empty expected but non-empty result": compareResult("n", evalCmd{instant: true, ts: time.Minute}, expectations{empty: true}, promql.Vector{{Metric: foo, F: 5}}),
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, err)
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
