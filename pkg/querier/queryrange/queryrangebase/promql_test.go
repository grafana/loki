package queryrangebase

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/util/annotations"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	start  = time.Unix(1000, 0)
	end    = start.Add(3 * time.Minute)
	step   = 30 * time.Second
	ctx    = context.Background()
	engine = promql.NewEngine(promql.EngineOpts{
		Reg:                prometheus.DefaultRegisterer,
		Logger:             util_log.SlogFromGoKit(log.NewNopLogger()),
		Timeout:            1 * time.Hour,
		MaxSamples:         10e6,
		ActiveQueryTracker: nil,
	})
)

// This test allows to verify which PromQL expressions can be parallelized.
func Test_PromQL(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		normalQuery string
		shardQuery  string
		shouldEqual bool
	}{
		// Vector can be parallelized but we need to remove the cortex shard label.
		// It should be noted that the __cortex_shard__ label is required by the engine
		// and therefore should be returned by the storage.
		// Range vectors `bar1{baz="blip"}[1m]` are not tested here because it is not supported
		// by range queries.
		{
			`bar1{baz="blip"}`,
			`label_replace(
				bar1{__cortex_shard__="0_of_3",baz="blip"} or
				bar1{__cortex_shard__="1_of_3",baz="blip"} or
				bar1{__cortex_shard__="2_of_3",baz="blip"},
				"__cortex_shard__","","",""
			)`,
			true,
		},
		// __cortex_shard__ label is required otherwise the or will keep only the first series.
		{
			`sum(bar1{baz="blip"})`,
			`sum(
				sum (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				sum (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				sum (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			false,
		},
		{
			`sum(bar1{baz="blip"})`,
			`sum(
				sum without(__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				sum without(__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				sum without(__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		{
			`sum by (foo) (bar1{baz="blip"})`,
			`sum by (foo) (
				sum by(foo,__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				sum by(foo,__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				sum by(foo,__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		{
			`sum by (foo,bar) (bar1{baz="blip"})`,
			`sum by (foo,bar)(
				sum by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				sum by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				sum by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		// since series are unique to a shard, it's safe to sum without shard first, then reaggregate
		{
			`sum without (foo,bar) (bar1{baz="blip"})`,
			`sum without (foo,bar)(
				sum without(__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				sum without(__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				sum without(__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		{
			`min by (foo,bar) (bar1{baz="blip"})`,
			`min by (foo,bar)(
				min by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				min by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				min by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		{
			`max by (foo,bar) (bar1{baz="blip"})`,
			` max by (foo,bar)(
				max by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				max by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				max by(foo,bar,__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		// avg can be paralleized since we split it into sum / count
		{
			`avg(bar1{baz="blip"})`,
			`(sum(bar1{__cortex_shard__="0_of_3",baz="blip"}) + sum(bar1{__cortex_shard__="1_of_3",baz="blip"}) + sum(bar1{__cortex_shard__="2_of_3",baz="blip"})) /
             (count(bar1{__cortex_shard__="0_of_3",baz="blip"}) + count(bar1{__cortex_shard__="1_of_3",baz="blip"}) + count(bar1{__cortex_shard__="2_of_3",baz="blip"}))`,
			true,
		},
		{
			`avg(bar1{baz="blip"})`,
			`avg(
				avg by(__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				avg by(__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				avg by(__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			true,
		},
		// stddev can't be parallelized.
		{
			`stddev(bar1{baz="blip"})`,
			` stddev(
				stddev by(__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				stddev by(__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				stddev by(__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			false,
		},
		// stdvar can't be parallelized.
		{
			`stdvar(bar1{baz="blip"})`,
			`stdvar(
				stdvar by(__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				stdvar by(__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				stdvar by(__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			  )`,
			false,
		},
		{
			`count(bar1{baz="blip"})`,
			`count(
				count without (__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				count without (__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				count without (__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
				)`,
			true,
		},
		{
			`count by (foo,bar) (bar1{baz="blip"})`,
			`count by (foo,bar) (
				count by (foo,bar,__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				count by (foo,bar,__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				count by (foo,bar,__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			)`,
			true,
		},
		// different ways to represent count without.
		{
			`count without (foo) (bar1{baz="blip"})`,
			`count without (foo) (
				count without (__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				count without (__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				count without (__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			)`,
			true,
		},
		{
			`count without (foo) (bar1{baz="blip"})`,
			`sum without (__cortex_shard__) (
				count without (foo) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				count without (foo) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				count without (foo) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			)`,
			true,
		},
		{
			`count without (foo, bar) (bar1{baz="blip"})`,
			`count without (foo, bar) (
				count without (__cortex_shard__) (bar1{__cortex_shard__="0_of_3",baz="blip"}) or
				count without (__cortex_shard__) (bar1{__cortex_shard__="1_of_3",baz="blip"}) or
				count without (__cortex_shard__) (bar1{__cortex_shard__="2_of_3",baz="blip"})
			)`,
			true,
		},
		{
			`topk(2,bar1{baz="blip"})`,
			`label_replace(
				topk(2,
					topk(2,(bar1{__cortex_shard__="0_of_3",baz="blip"})) without(__cortex_shard__) or
					topk(2,(bar1{__cortex_shard__="1_of_3",baz="blip"})) without(__cortex_shard__) or
					topk(2,(bar1{__cortex_shard__="2_of_3",baz="blip"})) without(__cortex_shard__)
				),
                          "__cortex_shard__","","","")`,
			true,
		},
		{
			`bottomk(2,bar1{baz="blip"})`,
			`label_replace(
				bottomk(2,
					bottomk(2,(bar1{__cortex_shard__="0_of_3",baz="blip"})) without(__cortex_shard__) or
					bottomk(2,(bar1{__cortex_shard__="1_of_3",baz="blip"})) without(__cortex_shard__) or
					bottomk(2,(bar1{__cortex_shard__="2_of_3",baz="blip"})) without(__cortex_shard__)
				),
                          "__cortex_shard__","","","")`,
			true,
		},
		{
			`sum by (foo,bar) (avg_over_time(bar1{baz="blip"}[1m]))`,
			`sum by (foo,bar)(
				sum by(foo,bar,__cortex_shard__) (avg_over_time(bar1{__cortex_shard__="0_of_3",baz="blip"}[1m])) or
				sum by(foo,bar,__cortex_shard__) (avg_over_time(bar1{__cortex_shard__="1_of_3",baz="blip"}[1m])) or
				sum by(foo,bar,__cortex_shard__) (avg_over_time(bar1{__cortex_shard__="2_of_3",baz="blip"}[1m]))
			  )`,
			true,
		},
		{
			`sum by (foo,bar) (min_over_time(bar1{baz="blip"}[1m]))`,
			`sum by (foo,bar)(
				sum by(foo,bar,__cortex_shard__) (min_over_time(bar1{__cortex_shard__="0_of_3",baz="blip"}[1m])) or
				sum by(foo,bar,__cortex_shard__) (min_over_time(bar1{__cortex_shard__="1_of_3",baz="blip"}[1m])) or
				sum by(foo,bar,__cortex_shard__) (min_over_time(bar1{__cortex_shard__="2_of_3",baz="blip"}[1m]))
			  )`,
			true,
		},
		{
			// Sub aggregations must avoid non-associative series merging across shards
			`sum(
			  count(
			    bar1
			  )  by (foo,bazz)
			)`,
			`
			  sum without(__cortex_shard__) (
			    sum by(__cortex_shard__) (
			      count by(foo, bazz) (foo{__cortex_shard__="0_of_2",bar="baz"})
			    ) or
			    sum by(__cortex_shard__) (
			      count by(foo, bazz) (foo{__cortex_shard__="1_of_2",bar="baz"})
			    )
			  )
`,
			false,
		},
		{
			// Note: this is a speculative optimization that we don't currently include due to mapping complexity.
			// Certain sub aggregations may inject __cortex_shard__ for all (by) subgroupings.
			// This is the same as the previous test with the exception that the shard label is injected to the count grouping
			`sum(
			  count(
			    bar1
			  )  by (foo,bazz)
			)`,
			`
			  sum without(__cortex_shard__) (
			    sum by(__cortex_shard__) (
			      count by(foo, bazz, __cortex_shard__) (foo{__cortex_shard__="0_of_2",bar="baz"})
			    ) or
			    sum by(__cortex_shard__) (
			      count by(foo, bazz, __cortex_shard__) (foo{__cortex_shard__="1_of_2",bar="baz"})
			    )
			  )
`,
			true,
		},
		{
			// Note: this is a speculative optimization that we don't currently include due to mapping complexity
			// This example details multiple layers of aggregations.
			// Sub aggregations must inject __cortex_shard__ for all (by) subgroupings.
			`sum(
			  count(
			    count(
			      bar1
			    )  by (foo,bazz)
			  )  by (bazz)
			)`,
			`
			  sum without(__cortex_shard__) (
			    sum by(__cortex_shard__) (
			      count by(bazz, __cortex_shard__) (
				count by(foo, bazz, __cortex_shard__) (
				  foo{__cortex_shard__="0_of_2", bar="baz"}
				)
			      )
			    ) or
			    sum by(__cortex_shard__) (
			      count by(bazz, __cortex_shard__) (
				count by(foo, bazz, __cortex_shard__) (
				  foo{__cortex_shard__="1_of_2", bar="baz"}
				)
			      )
			    )
			  )
`,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.normalQuery, func(t *testing.T) {

			baseQuery, err := engine.NewRangeQuery(context.Background(), shardAwareQueryable, nil, tt.normalQuery, start, end, step)
			require.Nil(t, err)
			shardQuery, err := engine.NewRangeQuery(context.Background(), shardAwareQueryable, nil, tt.shardQuery, start, end, step)
			require.Nil(t, err)
			baseResult := baseQuery.Exec(ctx)
			shardResult := shardQuery.Exec(ctx)
			t.Logf("base: %v\n", baseResult)
			t.Logf("shard: %v\n", shardResult)
			if tt.shouldEqual {
				require.Equal(t, baseResult, shardResult)
				return
			}
			require.NotEqual(t, baseResult, shardResult)
		})
	}

}

func Test_FunctionParallelism(t *testing.T) {
	tpl := `sum(<fn>(bar1{}<fArgs>))`
	shardTpl := `sum(
				sum without(__cortex_shard__) (<fn>(bar1{__cortex_shard__="0_of_3"}<fArgs>)) or
				sum without(__cortex_shard__) (<fn>(bar1{__cortex_shard__="1_of_3"}<fArgs>)) or
				sum without(__cortex_shard__) (<fn>(bar1{__cortex_shard__="2_of_3"}<fArgs>))
			  )`

	mkQuery := func(tpl, fn string, testMatrix bool, fArgs []string) (result string) {
		result = strings.Replace(tpl, "<fn>", fn, -1)

		if testMatrix {
			// turn selectors into ranges
			result = strings.Replace(result, "}<fArgs>", "}[1m]<fArgs>", -1)
		}

		if len(fArgs) > 0 {
			args := "," + strings.Join(fArgs, ",")
			result = strings.Replace(result, "<fArgs>", args, -1)
		} else {
			result = strings.Replace(result, "<fArgs>", "", -1)
		}

		return result
	}

	for _, tc := range []struct {
		fn           string
		fArgs        []string
		isTestMatrix bool
		approximate  bool
	}{
		{
			fn: "abs",
		},
		{
			fn:           "avg_over_time",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn: "ceil",
		},
		{
			fn:           "changes",
			isTestMatrix: true,
		},
		{
			fn:           "count_over_time",
			isTestMatrix: true,
		},
		{
			fn: "days_in_month",
		},
		{
			fn: "day_of_month",
		},
		{
			fn: "day_of_week",
		},
		{
			fn:           "delta",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:           "deriv",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:          "exp",
			approximate: true,
		},
		{
			fn: "floor",
		},
		{
			fn: "hour",
		},
		{
			fn:           "idelta",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:           "increase",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:           "irate",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:          "ln",
			approximate: true,
		},
		{
			fn:          "log10",
			approximate: true,
		},
		{
			fn:          "log2",
			approximate: true,
		},
		{
			fn:           "max_over_time",
			isTestMatrix: true,
		},
		{
			fn:           "min_over_time",
			isTestMatrix: true,
		},
		{
			fn: "minute",
		},
		{
			fn: "month",
		},
		{
			fn:           "rate",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:           "resets",
			isTestMatrix: true,
		},
		{
			fn: "sort",
		},
		{
			fn: "sort_desc",
		},
		{
			fn:          "sqrt",
			approximate: true,
		},
		{
			fn:           "stddev_over_time",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:           "stdvar_over_time",
			isTestMatrix: true,
			approximate:  true,
		},
		{
			fn:           "sum_over_time",
			isTestMatrix: true,
		},
		{
			fn: "timestamp",
		},
		{
			fn: "year",
		},
		{
			fn:    "clamp_max",
			fArgs: []string{"5"},
		},
		{
			fn:    "clamp_min",
			fArgs: []string{"5"},
		},
		{
			fn:           "predict_linear",
			isTestMatrix: true,
			approximate:  true,
			fArgs:        []string{"1"},
		},
		{
			fn:    "round",
			fArgs: []string{"20"},
		},
	} {

		t.Run(tc.fn, func(t *testing.T) {
			baseQuery, err := engine.NewRangeQuery(
				context.Background(),
				shardAwareQueryable,
				nil,
				mkQuery(tpl, tc.fn, tc.isTestMatrix, tc.fArgs),
				start,
				end,
				step,
			)
			require.Nil(t, err)
			shardQuery, err := engine.NewRangeQuery(
				context.Background(),
				shardAwareQueryable,
				nil,
				mkQuery(shardTpl, tc.fn, tc.isTestMatrix, tc.fArgs),
				start,
				end,
				step,
			)
			require.Nil(t, err)
			baseResult := baseQuery.Exec(ctx)
			shardResult := shardQuery.Exec(ctx)
			t.Logf("base: %+v\n", baseResult)
			t.Logf("shard: %+v\n", shardResult)
			if !tc.approximate {
				require.Equal(t, baseResult, shardResult)
			} else {
				// Some functions yield tiny differences when sharded due to combining floating point calculations.
				baseSeries := baseResult.Value.(promql.Matrix)[0]
				shardSeries := shardResult.Value.(promql.Matrix)[0]

				require.Equal(t, len(baseSeries.Floats), len(shardSeries.Floats))
				for i, basePt := range baseSeries.Floats {
					shardPt := shardSeries.Floats[i]
					require.Equal(t, basePt.T, shardPt.T)
					require.Equal(
						t,
						math.Round(basePt.F*1e6)/1e6,
						math.Round(shardPt.F*1e6)/1e6,
					)
				}

			}
		})
	}

}

var shardAwareQueryable = storage.QueryableFunc(func(_, _ int64) (storage.Querier, error) {
	return &testMatrix{
		series: []*promql.StorageSeries{
			newSeries(labels.Labels{{Name: "__name__", Value: "bar1"}, {Name: "baz", Value: "blip"}, {Name: "bar", Value: "blop"}, {Name: "foo", Value: "barr"}}, factor(5)),
			newSeries(labels.Labels{{Name: "__name__", Value: "bar1"}, {Name: "baz", Value: "blip"}, {Name: "bar", Value: "blop"}, {Name: "foo", Value: "bazz"}}, factor(7)),
			newSeries(labels.Labels{{Name: "__name__", Value: "bar1"}, {Name: "baz", Value: "blip"}, {Name: "bar", Value: "blap"}, {Name: "foo", Value: "buzz"}}, factor(12)),
			newSeries(labels.Labels{{Name: "__name__", Value: "bar1"}, {Name: "baz", Value: "blip"}, {Name: "bar", Value: "blap"}, {Name: "foo", Value: "bozz"}}, factor(11)),
			newSeries(labels.Labels{{Name: "__name__", Value: "bar1"}, {Name: "baz", Value: "blip"}, {Name: "bar", Value: "blop"}, {Name: "foo", Value: "buzz"}}, factor(8)),
			newSeries(labels.Labels{{Name: "__name__", Value: "bar1"}, {Name: "baz", Value: "blip"}, {Name: "bar", Value: "blap"}, {Name: "foo", Value: "bazz"}}, identity),
		},
	}, nil
})

type testMatrix struct {
	series []*promql.StorageSeries
}

func (m *testMatrix) Copy() *testMatrix {
	cpy := *m
	return &cpy
}

func (m testMatrix) Next() bool { return len(m.series) != 0 }

func (m *testMatrix) At() storage.Series {
	res := m.series[0]
	m.series = m.series[1:]
	return res
}

func (m *testMatrix) Err() error { return nil }

func (m *testMatrix) Warnings() annotations.Annotations { return annotations.Annotations{} }

func (m *testMatrix) Select(_ context.Context, _ bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	s, _, err := astmapper.ShardFromMatchers(matchers)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	if s != nil {
		return splitByShard(s.Shard, s.Of, m)
	}

	return m.Copy()
}

func (m *testMatrix) LabelValues(_ context.Context, _ string, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (m *testMatrix) LabelNames(_ context.Context, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (m *testMatrix) Close() error { return nil }

func newSeries(metric labels.Labels, generator func(float64) float64) *promql.StorageSeries {
	sort.Sort(metric)
	var points []promql.FPoint

	for ts := start.Add(-step); ts.Unix() <= end.Unix(); ts = ts.Add(step) {
		t := ts.Unix() * 1e3
		points = append(points, promql.FPoint{
			T: t,
			F: generator(float64(t)),
		})
	}

	return promql.NewStorageSeries(promql.Series{
		Metric: metric,
		Floats: points,
	})
}

func identity(t float64) float64 {
	return t
}

func factor(f float64) func(float64) float64 {
	i := 0.
	return func(float64) float64 {
		i++
		res := i * f
		return res
	}
}

// var identity(t int64) float64 {
// 	return float64(t)
// }

// splitByShard returns the shard subset of a testMatrix.
// e.g if a testMatrix has 6 series, and we want 3 shard, then each shard will contain
// 2 series.
func splitByShard(shardIndex, shardTotal int, testMatrices *testMatrix) *testMatrix {
	res := &testMatrix{}
	var it chunkenc.Iterator
	for i, s := range testMatrices.series {
		if i%shardTotal != shardIndex {
			continue
		}
		var points []promql.FPoint
		it = s.Iterator(it)
		for it.Next() != chunkenc.ValNone {
			t, v := it.At()
			points = append(points, promql.FPoint{
				T: t,
				F: v,
			})

		}
		lbs := s.Labels().Copy()
		lbs = append(lbs, labels.Label{Name: "__cortex_shard__", Value: fmt.Sprintf("%d_of_%d", shardIndex, shardTotal)})
		sort.Sort(lbs)
		res.series = append(res.series, promql.NewStorageSeries(promql.Series{
			Metric: lbs,
			Floats: points,
		}))
	}
	return res
}
