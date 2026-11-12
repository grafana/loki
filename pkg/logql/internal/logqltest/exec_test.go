package logqltest

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestIsQueryShardingSupported(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		want  bool
	}{
		"shardable vector aggregation":                                              {`sum(rate({app="a"}[1m]))`, true},
		"top-level quantile shards":                                                 {`quantile_over_time(0.99, {app="a"} | unwrap v [1m]) by (pod)`, true},
		"nested quantile does not shard":                                            {`max(quantile_over_time(0.99, {app="a"} | unwrap v [1m]))`, false},
		"top-level approx_count_distinct shards":                                    {`approx_count_distinct(id, {app="a"} | logfmt [1m]) by (pod)`, true},
		"non-shardable range op":                                                    {`stddev_over_time({app="a"} | unwrap v [1m])`, false},
		"avg_over_time with an __error__ unwrap post filter does not shard":         {`avg_over_time({app="a"} | unwrap v | __error__="" [1m]) by (pod)`, false},
		"avg_over_time with an __error_details__ unwrap post filter does not shard": {`avg_over_time({app="a"} | unwrap v | __error_details__="" [1m]) by (pod)`, false},
		"avg_over_time with another unwrap post filter shards":                      {`avg_over_time({app="a"} | unwrap v | pod="p1" [1m]) by (pod)`, true},
		"vector() literal does not shard":                                           {`vector(1)`, false},
		"unparseable query is unsupported":                                          {`}{ not a query`, false},
		"bare log selector shards":                                                  {`{app="a"}`, true},
		"log selector with line filter shards":                                      {`{app="a"} |= "100"`, true},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, isQueryShardingSupported(tc.query))
		})
	}
}

func TestCheckDataObjReads(t *testing.T) {
	dataObjStart := epoch.Add(90 * time.Second)
	// A one-minute window ends at the sample and starts at or after dataObjStart.
	afterStart := dataObjStart.Add(2 * time.Minute)
	// A one-minute window starts before dataObjStart.
	nearStart := dataObjStart.Add(30 * time.Second)

	const (
		nested      = `max(sum by (a) (count_over_time({a="b"}[1m])))`
		noRowsError = `query %q returned samples after the data-object start time, but read no data-object rows`
	)

	// vectorResult returns an instant query result with one sample at ts. The query read
	// dataObjRows data-object rows.
	vectorResult := func(ts time.Time, dataObjRows int64) logqlmodel.Result {
		return logqlmodel.Result{
			Data: promql.Vector{{T: ts.UnixMilli()}},
			Statistics: stats.Result{
				Querier: stats.Querier{Store: stats.Store{Dataobj: stats.Dataobj{PrePredicateDecompressedRows: dataObjRows}}},
			},
		}
	}

	t.Run("returns an error when a nested stream-first part reads no data-object rows for a sample after the start time", func(t *testing.T) {
		err := checkDataObjReads(nested, vectorResult(afterStart, 0), dataObjStart)

		require.EqualError(t, err, fmt.Sprintf(noRowsError, nested))
	})

	t.Run("returns an error when a range query result holds a sample after the start time and no data-object rows were read", func(t *testing.T) {
		res := logqlmodel.Result{Data: promql.Matrix{{Floats: []promql.FPoint{
			{T: nearStart.UnixMilli()},
			{T: afterStart.UnixMilli()},
		}}}}

		err := checkDataObjReads(nested, res, dataObjStart)

		require.EqualError(t, err, fmt.Sprintf(noRowsError, nested))
	})

	t.Run("returns no error when the query read data-object rows", func(t *testing.T) {
		require.NoError(t, checkDataObjReads(nested, vectorResult(afterStart, 5), dataObjStart))
	})

	t.Run("returns no error when the window of the sample starts before the start time", func(t *testing.T) {
		require.NoError(t, checkDataObjReads(nested, vectorResult(nearStart, 0), dataObjStart))
	})

	t.Run("returns an error when the window of a vector sample starts exactly at the start time", func(t *testing.T) {
		err := checkDataObjReads(nested, vectorResult(dataObjStart.Add(time.Minute), 0), dataObjStart)

		require.EqualError(t, err, fmt.Sprintf(noRowsError, nested))
	})

	t.Run("returns an error when the window of a range query point starts exactly at the start time", func(t *testing.T) {
		res := logqlmodel.Result{Data: promql.Matrix{{Floats: []promql.FPoint{{T: dataObjStart.Add(time.Minute).UnixMilli()}}}}}

		err := checkDataObjReads(nested, res, dataObjStart)

		require.EqualError(t, err, fmt.Sprintf(noRowsError, nested))
	})

	t.Run("returns no error when every point of a range query result has a window that starts before the start time", func(t *testing.T) {
		res := logqlmodel.Result{Data: promql.Matrix{{Floats: []promql.FPoint{{T: nearStart.UnixMilli()}}}}}

		require.NoError(t, checkDataObjReads(nested, res, dataObjStart))
	})

	t.Run("returns no error when the offset moves the window before the start time", func(t *testing.T) {
		const query = `sum(count_over_time({a="b"}[1m] offset 5m))`

		require.NoError(t, checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart))
	})

	t.Run("returns no error when the part with the longest reach starts before the start time", func(t *testing.T) {
		const query = `sum(count_over_time({a="b"}[1m])) + sum(count_over_time({a="b"}[5m]))`

		require.NoError(t, checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart))
	})

	t.Run("returns no error when the query holds a vector expression", func(t *testing.T) {
		const query = `sum(count_over_time({a="b"}[1m])) or vector(0)`

		require.NoError(t, checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart))
	})

	t.Run("returns no error when the query holds a range aggregation that is not stream-first", func(t *testing.T) {
		const query = `sum(count_over_time({a="b"}[1m])) or count_over_time({a="c"}[1m])`

		require.NoError(t, checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart))
	})

	t.Run("returns no error when the query holds a log range under a label aggregation", func(t *testing.T) {
		const query = `sum(count_over_time({a="b"}[1m])) + approx_count_distinct(id, {a="c"} | logfmt [1m]) by (pod)`

		require.NoError(t, checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart))
	})

	t.Run("returns no error when no part of the query is stream-first", func(t *testing.T) {
		const query = `sum(avg_over_time({a="b"} | logfmt | unwrap n [1m]))`

		require.NoError(t, checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart))
	})

	t.Run("returns no error when the start time is the zero value", func(t *testing.T) {
		require.NoError(t, checkDataObjReads(nested, vectorResult(afterStart, 0), time.Time{}))
	})

	t.Run("returns an error when the query does not parse", func(t *testing.T) {
		const query = `sum(`
		_, parseErr := syntax.ParseExpr(query)
		require.Error(t, parseErr)

		err := checkDataObjReads(query, vectorResult(afterStart, 0), dataObjStart)

		require.EqualError(t, err, fmt.Sprintf("parsing query %q: %v", query, parseErr))
	})
}
