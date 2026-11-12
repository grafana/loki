package syntax

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
)

const histogramTestRange = `{app="api"} | json | unwrap duration_seconds(duration) [5m]`

// requireHistogramRoundTrips checks that expr survives printing and parsing,
// cloning and the JSON serialization used to send a plan to a querier.
func requireHistogramRoundTrips(t *testing.T, expr Expr) {
	t.Helper()

	reparsed, err := ParseExpr(expr.String())
	require.NoError(t, err, "String() must parse: %s", expr)
	AssertExpressions(t, expr, reparsed)

	cloned, err := Clone[Expr](expr)
	require.NoError(t, err)
	AssertExpressions(t, expr, cloned)

	var buf bytes.Buffer
	require.NoError(t, EncodeJSON(expr, &buf))
	decoded, err := DecodeJSON(buf.String())
	require.NoError(t, err, buf.String())
	AssertExpressions(t, expr, decoded)

	old := MaxCharsPerLine
	MaxCharsPerLine = 20
	defer func() { MaxCharsPerLine = old }()
	prettified, err := ParseExpr(Prettify(expr))
	require.NoError(t, err, "Prettify() must parse: %s", Prettify(expr))
	AssertExpressions(t, expr, prettified)
}

// histogramParseCases are valid histogram queries and their canonical form.
// They are the seeds of FuzzParseHistogram too.
var histogramParseCases = []struct {
	name string
	in   string
	want string
}{
	{
		name: "quantile of a summed histogram",
		in:   `histogram_quantile(0.99, sum by (path) (histogram_over_time(` + histogramTestRange + `)))`,
		want: `histogram_quantile(0.99,sum by (path)(histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m])))`,
	},
	{
		name: "quantile with the grouping inside the range aggregation",
		in:   `histogram_quantile(.5, histogram_over_time(` + histogramTestRange + `) by (path))`,
		want: `histogram_quantile(0.5,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (path))`,
	},
	{
		name: "sum of a sum",
		in:   `histogram_count(sum(sum by (a) (histogram_over_time(` + histogramTestRange + `))))`,
		want: `histogram_count(sum(sum by (a)(histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]))))`,
	},
	{
		name: "sum without",
		in:   `histogram_avg(sum without (pod) (histogram_over_time(` + histogramTestRange + `)))`,
		want: `histogram_avg(sum without (pod)(histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m])))`,
	},
	{
		name: "custom buckets",
		in:   `histogram_count(histogram_over_time(buckets(0.005, 0.05, 0.5, 5), ` + histogramTestRange + `) by (path))`,
		want: `histogram_count(histogram_over_time(buckets(0.005,0.05,0.5,5),{app="api"} | json | unwrap duration_seconds(duration)[5m]) by (path))`,
	},
	{
		name: "custom buckets without grouping",
		in:   `histogram_sum(histogram_over_time(buckets(1, 2), ` + histogramTestRange + `))`,
		want: `histogram_sum(histogram_over_time(buckets(1,2),{app="api"} | json | unwrap duration_seconds(duration)[5m]))`,
	},
	{
		name: "negative and zero bounds",
		in:   `histogram_sum(histogram_over_time(buckets(-1, 0, +1), ` + histogramTestRange + `) by (path))`,
		want: `histogram_sum(histogram_over_time(buckets(-1,0,1),{app="api"} | json | unwrap duration_seconds(duration)[5m]) by (path))`,
	},
	{
		name: "offset",
		in:   `histogram_count(histogram_over_time({app="api"} | json | unwrap duration [5m] offset 1h) without (pod))`,
		want: `histogram_count(histogram_over_time({app="api"} | json | unwrap duration[5m] offset 1h0m0s) without (pod))`,
	},
	{
		name: "several quantiles",
		in:   `histogram_quantiles(histogram_over_time(` + histogramTestRange + `) by (path), "quantile", 0.5, 0.99, 0.999)`,
		want: `histogram_quantiles(histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (path),"quantile",0.5,0.99,0.999)`,
	},
	{
		name: "fraction between two bounds",
		in:   `histogram_fraction(0, 0.005, histogram_over_time(` + histogramTestRange + `) by (user))`,
		want: `histogram_fraction(0,0.005,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (user))`,
	},
	{
		name: "fraction with a lower infinity",
		in:   `histogram_fraction(-Inf, 0.005, histogram_over_time(` + histogramTestRange + `) by (user))`,
		want: `histogram_fraction(-Inf,0.005,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (user))`,
	},
	{
		name: "fraction with an upper infinity",
		in:   `histogram_fraction(0.5, +Inf, histogram_over_time(` + histogramTestRange + `) by (user))`,
		want: `histogram_fraction(0.5,+Inf,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (user))`,
	},
	{
		name: "infinity is case insensitive and its sign is optional",
		in:   `histogram_fraction(0.5, inf, histogram_over_time(` + histogramTestRange + `) by (user))`,
		want: `histogram_fraction(0.5,+Inf,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (user))`,
	},
	{
		name: "reader inside a float expression",
		in:   `sum(histogram_quantile(0.9, histogram_over_time(` + histogramTestRange + `) by (a))) by (b)`,
		want: `sum by (b)(histogram_quantile(0.9,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (a)))`,
	},
	{
		name: "reader in a binary operation",
		in:   `histogram_quantile(0.9, histogram_over_time(` + histogramTestRange + `) by (a)) > 1`,
		want: `(histogram_quantile(0.9,histogram_over_time({app="api"} | json | unwrap duration_seconds(duration)[5m]) by (a)) > 1)`,
	},
}

func TestParseHistogram(t *testing.T) {
	for _, tc := range histogramParseCases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := ParseExpr(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, expr.String())
			requireHistogramRoundTrips(t, expr)
		})
	}
}

func TestParseHistogram_ast(t *testing.T) {
	expr, err := ParseExpr(`histogram_quantiles(histogram_over_time(buckets(0.5, 5), ` + histogramTestRange + `) by (path), "q", 0.5, 0.99)`)
	require.NoError(t, err)

	fn, ok := expr.(*HistogramFuncExpr)
	require.True(t, ok, "%T", expr)
	require.Equal(t, OpHistogramQuantiles, fn.Operation)
	require.Equal(t, "q", fn.Label)
	require.Equal(t, []float64{0.5, 0.99}, fn.Params)

	rng, ok := fn.Left.(*RangeAggregationExpr)
	require.True(t, ok, "%T", fn.Left)
	require.Equal(t, OpRangeTypeHistogram, rng.Operation)
	require.Equal(t, []float64{0.5, 5}, rng.Buckets.Bounds)
	require.Equal(t, []string{"path"}, rng.Grouping.Groups)
	require.Nil(t, rng.Params)

	fn, err = parseHistogramFunc(`histogram_fraction(-Inf, +Inf, histogram_over_time(` + histogramTestRange + `))`)
	require.NoError(t, err)
	require.Equal(t, []float64{math.Inf(-1), math.Inf(1)}, fn.Params)

	fn, err = parseHistogramFunc(`histogram_count(histogram_over_time(` + histogramTestRange + `))`)
	require.NoError(t, err)
	require.Nil(t, fn.Left.(*RangeAggregationExpr).Buckets, "default buckets are a nil spec")
}

func parseHistogramFunc(in string) (*HistogramFuncExpr, error) {
	expr, err := ParseExpr(in)
	if err != nil {
		return nil, err
	}
	fn, ok := expr.(*HistogramFuncExpr)
	if !ok {
		return nil, fmt.Errorf("got %T, want *HistogramFuncExpr", expr)
	}
	return fn, nil
}

func TestParseHistogram_errors(t *testing.T) {
	manyBounds := make([]string, MaxBucketBounds+1)
	for i := range manyBounds {
		manyBounds[i] = fmt.Sprint(i)
	}
	const (
		rangeAgg   = `histogram_over_time(` + histogramTestRange + `) by (a)`
		rootErr    = "a histogram_over_time expression returns a histogram, which a query cannot return"
		boundsErr  = "bucket bounds must be strictly increasing"
		notHistErr = "requires a histogram_over_time expression"
	)

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		// A histogram cannot be the result of a query.
		{"bare range aggregation", `histogram_over_time(` + histogramTestRange + `)`, rootErr},
		{"bare range aggregation with custom buckets", `histogram_over_time(buckets(1, 2), ` + histogramTestRange + `) by (a)`, rootErr},
		{"bare sum", `sum by (a) (` + rangeAgg + `)`, rootErr},
		{"bare sum of a sum", `sum(sum by (a) (` + rangeAgg + `))`, rootErr},

		// histogram_over_time needs an unwrapped value and takes no scalar parameter.
		{"no unwrap", `histogram_count(histogram_over_time({app="api"}[5m]))`, "invalid aggregation histogram_over_time without unwrap"},
		{"scalar parameter", `histogram_count(histogram_over_time(0.5, ` + histogramTestRange + `))`, "syntax error: unexpected NUMBER, expecting { or ( or BUCKETS"},

		// buckets(...) is only valid in histogram_over_time, and its bounds are checked.
		{"buckets on another operation", `quantile_over_time(buckets(1, 2), ` + histogramTestRange + `)`, "syntax error: unexpected BUCKETS, expecting NUMBER or { or ("},
		{"no bounds", `histogram_count(histogram_over_time(buckets(), ` + histogramTestRange + `))`, "syntax error"},
		{"decreasing bounds", `histogram_count(histogram_over_time(buckets(2, 1), ` + histogramTestRange + `))`, boundsErr},
		{"repeated bound", `histogram_count(histogram_over_time(buckets(1, 1), ` + histogramTestRange + `))`, boundsErr},
		{"infinite bound", `histogram_count(histogram_over_time(buckets(1, +Inf), ` + histogramTestRange + `))`, "bucket bound +Inf must be finite"},
		{"too many bounds", `histogram_count(histogram_over_time(buckets(` + strings.Join(manyBounds, ",") + `), ` + histogramTestRange + `))`, fmt.Sprintf("buckets accepts at most %d bounds, got %d", MaxBucketBounds, MaxBucketBounds+1)},
		{"bucket error is not hidden by the reader", `histogram_quantile(0.9, histogram_over_time(buckets(3, 2), ` + histogramTestRange + `) by (a))`, boundsErr},

		// Numbers.
		{"NaN is not a number", `histogram_count(histogram_over_time(buckets(1, NaN), ` + histogramTestRange + `))`, `invalid number "NaN"`},
		{"quantile NaN", `histogram_quantile(nan, ` + rangeAgg + `)`, `invalid number "nan"`},
		{"unknown name as a number", `histogram_fraction(foo, 1, ` + rangeAgg + `)`, `invalid number "foo"`},
		{"number out of range", `histogram_quantile(1e999, ` + rangeAgg + `)`, `invalid number "1e999"`},

		// Reader arguments.
		{"no label for the quantile", `histogram_quantiles(` + rangeAgg + `, "", 0.5)`, "requires a label name for the quantile"},
		{"label with a space", `histogram_quantiles(` + rangeAgg + `, "not a label!", 0.5)`, `"not a label!" is not a valid label name`},
		{"label that starts with a digit", `histogram_quantiles(` + rangeAgg + `, "1abc", 0.5)`, `"1abc" is not a valid label name`},
		{"reserved label", `histogram_quantiles(` + rangeAgg + `, "__name__", 0.5)`, `label name "__name__" is reserved`},
		{"error label", `histogram_quantiles(` + rangeAgg + `, "__error__", 0.5)`, `label name "__error__" is reserved`},
		{"too many quantiles", `histogram_quantiles(` + rangeAgg + `, "q", 1,2,3,4,5,6,7,8,9,10,11)`, "requires between 1 and 10 quantiles, got 11"},

		// A reader needs a histogram.
		{"reader of a rate", `histogram_quantile(0.9, rate({app="api"}[5m]))`, notHistErr},
		{"reader of a sum of a rate", `histogram_quantile(0.9, sum(rate({app="api"}[5m])))`, notHistErr},
		{"reader of another reader", `histogram_count(histogram_quantile(0.9, ` + rangeAgg + `))`, notHistErr},
		{"reader of a literal", `histogram_sum(1)`, notHistErr},

		// A histogram can only be summed or read.
		{"avg", `histogram_count(avg(` + rangeAgg + `))`, "avg is not supported on a histogram_over_time expression, only sum is"},
		{"topk", `histogram_count(topk(3, ` + rangeAgg + `))`, "topk is not supported on a histogram_over_time expression, only sum is"},
		{"binary operation, left", `histogram_count(` + rangeAgg + ` / 2)`, "cannot be used in a binary operation"},
		{"binary operation, right", `histogram_count(2 + ` + rangeAgg + `)`, "cannot be used in a binary operation"},
		{"label_replace", `histogram_count(label_replace(` + rangeAgg + `, "x", "y", "z", "w"))`, "label_replace is not supported on a histogram_over_time expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseExpr(tc.in)
			require.Error(t, err)
			require.ErrorIs(t, err, logqlerr.ErrParse)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// The new function names are keywords only when a parenthesis follows, so
// labels with these names keep working.
func TestParseHistogram_namesAsLabels(t *testing.T) {
	for _, in := range []string{
		`{buckets="x", histogram_sum="y"} | json | buckets="1" | histogram_count="2"`,
		`sum_over_time({app="api"} | json | unwrap buckets [5m])`,
		`sum by (buckets, histogram_quantile) (count_over_time({app="api"}[5m]))`,
	} {
		t.Run(in, func(t *testing.T) {
			_, err := ParseExpr(in)
			require.NoError(t, err)
		})
	}
}

func TestBucketSpec_Validate(t *testing.T) {
	bounds := func(n int) []float64 {
		b := make([]float64, n)
		for i := range b {
			b[i] = float64(i)
		}
		return b
	}

	for _, tc := range []struct {
		name    string
		bounds  []float64
		wantErr string
	}{
		{name: "one bound", bounds: []float64{1}},
		{name: "negative bounds", bounds: []float64{-5, -0.5, 0, 0.5}},
		{name: "the most bounds", bounds: bounds(MaxBucketBounds)},
		{name: "none", bounds: nil, wantErr: "at least one bound"},
		{name: "one too many", bounds: bounds(MaxBucketBounds + 1), wantErr: "at most 32 bounds, got 33"},
		{name: "NaN", bounds: []float64{1, math.NaN()}, wantErr: "must not be NaN"},
		{name: "+Inf", bounds: []float64{1, math.Inf(1)}, wantErr: "+Inf must be finite"},
		{name: "-Inf", bounds: []float64{math.Inf(-1), 1}, wantErr: "-Inf must be finite"},
		{name: "equal", bounds: []float64{1, 2, 2}, wantErr: "2 is not greater than 2"},
		{name: "decreasing", bounds: []float64{1, 3, 2}, wantErr: "2 is not greater than 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := newBucketSpec(tc.bounds).Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// The grammar only accepts buckets(...) in histogram_over_time, but an
// expression can also be built or deserialized without the grammar.
func TestRangeAggregationExpr_Validate_buckets(t *testing.T) {
	withUnwrap := MustParseExpr(`sum_over_time(` + histogramTestRange + `)`).(*RangeAggregationExpr).Left
	withoutUnwrap := MustParseExpr(`count_over_time({app="api"}[5m])`).(*RangeAggregationExpr).Left

	for _, tc := range []struct {
		name    string
		expr    RangeAggregationExpr
		wantErr string
	}{
		{
			name: "histogram with custom buckets",
			expr: RangeAggregationExpr{Left: withUnwrap, Operation: OpRangeTypeHistogram, Buckets: newBucketSpec([]float64{1, 2})},
		},
		{
			name: "histogram with the default buckets",
			expr: RangeAggregationExpr{Left: withUnwrap, Operation: OpRangeTypeHistogram},
		},
		{
			name: "histogram with grouping",
			expr: RangeAggregationExpr{Left: withUnwrap, Operation: OpRangeTypeHistogram, Grouping: &Grouping{Groups: []string{"a"}}},
		},
		{
			name:    "histogram with bad buckets",
			expr:    RangeAggregationExpr{Left: withUnwrap, Operation: OpRangeTypeHistogram, Buckets: newBucketSpec([]float64{2, 1})},
			wantErr: "strictly increasing",
		},
		{
			name:    "histogram without unwrap",
			expr:    RangeAggregationExpr{Left: withoutUnwrap, Operation: OpRangeTypeHistogram},
			wantErr: "invalid aggregation histogram_over_time without unwrap",
		},
		{
			name:    "buckets on another operation",
			expr:    RangeAggregationExpr{Left: withUnwrap, Operation: OpRangeTypeSum, Buckets: newBucketSpec([]float64{1})},
			wantErr: "buckets not allowed for sum_over_time aggregation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.expr.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestHistogramFuncExpr_cloneIsDeep(t *testing.T) {
	const in = `histogram_quantiles(histogram_over_time(buckets(1, 2, 3), ` + histogramTestRange + `) by (a), "q", 0.5, 0.9)`
	expr := MustParseExpr(in)
	cloned := MustClone[Expr](expr).(*HistogramFuncExpr)

	cloned.Params[0] = 0.1
	cloned.Left.(*RangeAggregationExpr).Buckets.Bounds[0] = 100

	require.Equal(t, MustParseExpr(in).String(), expr.String(), "changing the clone must not change the original")
	require.NotEqual(t, expr.String(), cloned.String())
}

func TestHistogramFuncExpr_walkAndVisit(t *testing.T) {
	expr := MustParseExpr(`histogram_quantile(0.9, sum by (a) (histogram_over_time(` + histogramTestRange + `)))`)

	var walked []string
	expr.Walk(func(e Expr) bool {
		walked = append(walked, fmt.Sprintf("%T", e))
		return true
	})
	require.Equal(t, []string{
		"*syntax.HistogramFuncExpr",
		"*syntax.VectorAggregationExpr",
		"*syntax.RangeAggregationExpr",
		"*syntax.LogRangeExpr",
	}, walked[:4], "Walk goes through the function and the sum down to the log range")

	// By default the depth first traversal goes through a histogram function.
	var ops []string
	expr.Accept(&DepthFirstTraversal{
		VisitRangeAggregationFn: func(_ RootVisitor, e *RangeAggregationExpr) {
			ops = append(ops, e.Operation)
		},
	})
	require.Equal(t, []string{OpRangeTypeHistogram}, ops)

	// The function is not shardable. The shard mapper shards the histogram under
	// it and runs the function once on the merged result.
	require.False(t, expr.Shardable(true))
	require.False(t, expr.Shardable(false))
}

// A querier builds the sub-queries of a sharded query from JSON, which does not
// go through the grammar. The checks of the grammar must hold there too, because
// a bad expression would otherwise give wrong results or panic when it is printed.
func TestHistogram_decodeRejectsInvalid(t *testing.T) {
	const rng = `"range":{"interval_nanos":300000000000,"offset_nanos":0,"log_selector":{"raw":"{app=\"api\"}"},` +
		`"unwrap":{"identifier":"d","operation":"","post_filterers":[]}}`
	rangeAgg := func(op, extra string) string {
		return `{"range_agg":{"operation":"` + op + `",` + extra + rng + `}}`
	}
	reader := func(op, params, inner string) string {
		return `{"histogram_func":{"operation":"` + op + `",` + params + `"inner":` + inner + `}}`
	}

	for _, tc := range []struct {
		name    string
		json    string
		wantErr string
	}{
		// The bucket bounds are what the bucketing relies on.
		{"unsorted bounds", reader("histogram_count", "", rangeAgg("histogram_over_time", `"buckets":[3,1,2],`)), "strictly increasing"},
		{"repeated bounds", reader("histogram_count", "", rangeAgg("histogram_over_time", `"buckets":[1,1,1],`)), "strictly increasing"},
		{"no bounds", reader("histogram_count", "", rangeAgg("histogram_over_time", `"buckets":[],`)), "at least one bound"},
		{"buckets on another operation", reader("histogram_count", "", rangeAgg("sum_over_time", `"buckets":[1,2],`)), "buckets not allowed for sum_over_time aggregation"},
		{"scalar parameter", reader("histogram_count", "", rangeAgg("histogram_over_time", `"params":0.5,`)), "parameter not allowed for histogram_over_time aggregation"},
		{"histogram without a range", reader("histogram_count", "", `{"range_agg":{"operation":"histogram_over_time"}}`), "has no range"},

		// A reader needs an argument that is a histogram.
		{"no argument", `{"histogram_func":{"operation":"histogram_count"}}`, "requires a histogram argument"},
		{"argument is not a histogram", reader("histogram_count", "", rangeAgg("rate", "")), "requires a histogram_over_time expression"},
		{"no quantile", reader("histogram_quantile", "", rangeAgg("histogram_over_time", "")), "requires exactly 1 quantile"},
		{"quantile that is not a number", reader("histogram_quantile", `"params":["fast"],`, rangeAgg("histogram_over_time", "")), "invalid syntax"},
		{"quantile that is NaN", reader("histogram_quantile", `"params":["NaN"],`, rangeAgg("histogram_over_time", "")), "must not be NaN"},
		{"reserved quantile label", reader("histogram_quantiles", `"label":"__error__","params":["0.5"],`, rangeAgg("histogram_over_time", "")), "is reserved"},
		{"unknown function", reader("histogram_median", "", rangeAgg("histogram_over_time", "")), "histogram_median"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeJSON(tc.json)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}

	// The same documents are valid once they follow the rules, so the cases above
	// fail for the reason given and not for the way they are built.
	for name, doc := range map[string]string{
		"custom buckets": reader("histogram_count", "", rangeAgg("histogram_over_time", `"buckets":[1,2,3],`)),
		"default":        reader("histogram_quantile", `"params":["0.9"],`, rangeAgg("histogram_over_time", "")),
	} {
		t.Run("valid "+name, func(t *testing.T) {
			expr, err := DecodeJSON(doc)
			require.NoError(t, err)
			_, err = ParseExpr(expr.String())
			require.NoError(t, err, "a decoded expression must print as a query that parses: %s", expr)
		})
	}
}

func TestBucketSpec_Pretty(t *testing.T) {
	old := MaxCharsPerLine
	MaxCharsPerLine = 40
	defer func() { MaxCharsPerLine = old }()

	t.Run("a short list stays on one line", func(t *testing.T) {
		require.Equal(t, "buckets(1,2,3)", newBucketSpec([]float64{1, 2, 3}).Pretty(0))
		require.Equal(t, "  buckets(1,2,3)", newBucketSpec([]float64{1, 2, 3}).Pretty(1))
	})

	t.Run("a long list has one bound per line", func(t *testing.T) {
		spec := newBucketSpec([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1})
		require.Equal(t, `  buckets(
    0.005,
    0.01,
    0.025,
    0.05,
    0.1,
    0.25,
    0.5,
    1
  )`, spec.Pretty(1))
	})

	t.Run("the split list parses back", func(t *testing.T) {
		bounds := make([]string, MaxBucketBounds)
		for i := range bounds {
			bounds[i] = fmt.Sprint(i)
		}
		expr := MustParseExpr(`histogram_count(histogram_over_time(buckets(` + strings.Join(bounds, ",") + `), ` + histogramTestRange + `) by (a))`)

		pretty := Prettify(expr)
		require.Greater(t, strings.Count(pretty, "\n"), MaxBucketBounds, "one bound per line")
		reparsed, err := ParseExpr(pretty)
		require.NoError(t, err, pretty)
		AssertExpressions(t, expr, reparsed)
	})
}

// histogramFuzzTemplates put the fuzzed text where the histogram grammar reads it, in a
// query body that is known to print and parse back, so that the test is about the
// histogram grammar and nothing else.
//
// The arguments are bounds, quantiles, label, lower and upper.
var histogramFuzzTemplates = []string{
	`histogram_count(histogram_over_time(buckets(%[1]s), ` + histogramTestRange + `) by (a))`,
	`histogram_quantile(%[2]s, sum by (a) (histogram_over_time(` + histogramTestRange + `)))`,
	`histogram_quantiles(histogram_over_time(` + histogramTestRange + `) by (a), %[3]q, %[2]s)`,
	`histogram_fraction(%[4]s, %[5]s, histogram_over_time(buckets(1, 2), ` + histogramTestRange + `) without (a))`,
}

// histogramFuzzSeed holds the fuzzed text of one input.
type histogramFuzzSeed struct {
	bounds, quantiles, label, lower, upper string
}

// The seeds cover the part of the grammar that other functions do not share: signed
// numbers, Inf, hex and exponent forms, and long lists.
var histogramFuzzSeeds = []histogramFuzzSeed{
	{"0.005, 0.05, 0.5, 5", "0.99", "quantile", "0", "0.005"},
	{"-1, 0, +1", "0.5, 0.99, 0.999", "q", "-Inf", "+Inf"},
	{"-1e3, -0.5, 0, 0.5, 1e3", "1e-3", "quantile", "+0.5", "inf"},
	{"0.30000000000000004, 1e21, 1e-7", "0x1p-2", "le_q", "-inf", "1"},
	{"1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32", "1, 2, 3, 4, 5, 6, 7, 8, 9, 10", "q", "0", "1"},
}

// checkHistogramFuzz fills each template with the seed and checks every query that
// parses: it must print as something that parses to the same expression, and survive
// the JSON serialization. It returns which templates it checked.
func checkHistogramFuzz(t *testing.T, seed histogramFuzzSeed) []bool {
	checked := make([]bool, len(histogramFuzzTemplates))
	for i, template := range histogramFuzzTemplates {
		in := fmt.Sprintf(template, seed.bounds, seed.quantiles, seed.label, seed.lower, seed.upper)
		expr, err := ParseExpr(in)
		if err != nil {
			continue
		}
		requireHistogramRoundTrips(t, expr)
		checked[i] = true
	}
	return checked
}

// A test that skips what it is given checks nothing, and a fuzz test cannot tell. Here every
// template must be checked for at least one seed, and every seed for at least one template.
func TestHistogramFuzzSeedsAreChecked(t *testing.T) {
	covered := make([]bool, len(histogramFuzzTemplates))
	for i, seed := range histogramFuzzSeeds {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			checked := checkHistogramFuzz(t, seed)
			require.Contains(t, checked, true, "no template accepts the seed %+v", seed)
			for j, ok := range checked {
				covered[j] = covered[j] || ok
			}
		})
	}
	for i, ok := range covered {
		require.True(t, ok, "no seed is checked with the template %s", histogramFuzzTemplates[i])
	}
}

// FuzzParseHistogram runs checkHistogramFuzz on mutations of the seeds. Run it with
//
//	go test -run '^$' -fuzz FuzzParseHistogram -fuzztime 1m ./pkg/logql/syntax
//
// Without -fuzz it runs the seeds as a regular test.
func FuzzParseHistogram(f *testing.F) {
	for _, seed := range histogramFuzzSeeds {
		f.Add(seed.bounds, seed.quantiles, seed.label, seed.lower, seed.upper)
	}
	f.Fuzz(func(t *testing.T, bounds, quantiles, label, lower, upper string) {
		checkHistogramFuzz(t, histogramFuzzSeed{bounds, quantiles, label, lower, upper})
	})
}

func TestFormat_HistogramFunc(t *testing.T) {
	old := MaxCharsPerLine
	MaxCharsPerLine = 20
	defer func() { MaxCharsPerLine = old }()

	for _, tc := range []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "quantile of a sum",
			in:   `histogram_quantile(0.99, sum by (path) (histogram_over_time(` + histogramTestRange + `)))`,
			exp: `histogram_quantile(
  0.99,
  sum by (path)(
    histogram_over_time(
      {app="api"} | json
        | unwrap duration_seconds(duration) [5m]
    )
  )
)`,
		},
		{
			name: "quantile label and quantiles come after the histogram",
			in:   `histogram_quantiles(histogram_over_time(` + histogramTestRange + `) by (path), "quantile", 0.5, 0.99)`,
			exp: `histogram_quantiles(
  histogram_over_time(
    {app="api"} | json
      | unwrap duration_seconds(duration) [5m]
  ) by (path),
  "quantile",
  0.5,
  0.99
)`,
		},
		{
			name: "fraction bounds and custom buckets",
			in:   `histogram_fraction(0, +Inf, histogram_over_time(buckets(0.005, 0.5), {app="api"} | unwrap foo [5m]) by (user))`,
			exp: `histogram_fraction(
  0,
  +Inf,
  histogram_over_time(
    buckets(0.005,0.5),
    {app="api"}
      | unwrap foo [5m]
  ) by (user)
)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.exp, Prettify(MustParseExpr(tc.in)))
		})
	}
}

func TestUsesHistograms(t *testing.T) {
	const rng = `{app="api"} | json | unwrap duration [5m]`
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"a reader", `histogram_quantile(0.9, sum by (a) (histogram_over_time(` + rng + `)))`, true},
		{"the count", `histogram_count(histogram_over_time(` + rng + `))`, true},
		{"custom buckets", `histogram_sum(histogram_over_time(buckets(1, 2), ` + rng + `) by (a))`, true},
		{"in a binary operation", `sum(rate({app="api"}[5m])) + histogram_avg(histogram_over_time(` + rng + `))`, true},
		// A range aggregation that is visited after the histogram must not take the answer back. Walk does
		// not stop when a function returns false: it does not descend into that node, but it still visits
		// the node next to it, and a range aggregation that is the operand of a binary operation is one.
		{"before a range aggregation", `histogram_avg(histogram_over_time(` + rng + `)) + rate({app="api"}[5m])`, true},
		{"before range aggregations", `histogram_avg(histogram_over_time(` + rng + `)) + rate({app="api"}[5m]) + count_over_time({app="b"}[5m])`, true},
		{"in label_replace", `label_replace(histogram_count(histogram_over_time(` + rng + `)), "x", "y", "z", "w")`, true},
		{"inside another aggregation", `sum by (a) (histogram_count(histogram_over_time(` + rng + `)))`, true},

		{"a range aggregation", `quantile_over_time(0.9, ` + rng + `)`, false},
		{"a vector aggregation", `sum by (a) (rate({app="api"}[5m]))`, false},
		{"a literal", `1`, false},
		{"labels with the names of the functions", `sum by (buckets, histogram_sum) (count_over_time({buckets="x"} | json | histogram_count="1" [5m]))`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, UsesHistograms(MustParseExpr(tc.in)))
		})
	}

	// These are not queries that validation accepts, but the evaluator can be given one by a caller that does
	// not validate. A histogram at the root, and a function that reads something that is not a histogram.
	for name, in := range map[string]string{
		"a histogram at the root":         `sum by (a) (histogram_over_time(` + rng + `))`,
		"a range aggregation at the root": `histogram_over_time(` + rng + `)`,
		"a reader of a rate":              `histogram_count(rate({app="api"}[5m]))`,
	} {
		t.Run(name, func(t *testing.T) {
			expr, err := ParseExprWithoutValidation(in)
			require.NoError(t, err)
			require.True(t, UsesHistograms(expr))
		})
	}
}
