package executor

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	groupBy = []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}
)

func TestAggregator(t *testing.T) {
	colTs := semconv.ColumnIdentTimestamp.FQN()
	colVal := semconv.ColumnIdentValue.FQN()
	colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String).FQN()
	colSvc := semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String).FQN()

	t.Run("basic SUM aggregation with record building", func(t *testing.T) {
		agg := newAggregator(10, aggregationOperationSum)
		agg.AddLabels(groupBy)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		// Add test data
		// ts1: prod/app1 = 10, prod/app2 = 20, dev/app1 = 30
		agg.Add(ts1, 10, groupBy, []string{"prod", "app1"})
		agg.Add(ts1, 20, groupBy, []string{"prod", "app2"})
		agg.Add(ts1, 30, groupBy, []string{"dev", "app1"})

		// ts2: prod/app1 = 15, prod/app2 = 25, dev/app2 = 35
		agg.Add(ts2, 15, groupBy, []string{"prod", "app1"})
		agg.Add(ts2, 25, groupBy, []string{"prod", "app2"})
		agg.Add(ts2, 35, groupBy, []string{"dev", "app2"})

		// Add more data to same groups to test aggregation
		agg.Add(ts1, 5, groupBy, []string{"prod", "app1"})  // prod/app1 at ts1 should now be 15
		agg.Add(ts2, 10, groupBy, []string{"prod", "app1"}) // prod/app1 at ts2 should now be 25

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: ts1, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
			{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

			{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app1"},
			{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app2"},
			{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic AVG aggregation with record building", func(t *testing.T) {
		agg := newAggregator(10, aggregationOperationAvg)
		agg.AddLabels(groupBy)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		// Add test data
		// ts1: prod/app1 = 10, prod/app2 = 20, dev/app1 = 30
		agg.Add(ts1, 10, groupBy, []string{"prod", "app1"})
		agg.Add(ts1, 20, groupBy, []string{"prod", "app2"})
		agg.Add(ts1, 30, groupBy, []string{"dev", "app1"})

		// ts2: prod/app1 = 15, prod/app2 = 25, dev/app2 = 35
		agg.Add(ts2, 15, groupBy, []string{"prod", "app1"})
		agg.Add(ts2, 25, groupBy, []string{"prod", "app2"})
		agg.Add(ts2, 35, groupBy, []string{"dev", "app2"})

		// Add more data to same groups to test aggregation
		agg.Add(ts1, 5, groupBy, []string{"prod", "app1"})  // prod/app1 at ts1 should now be 7.5
		agg.Add(ts2, 10, groupBy, []string{"prod", "app1"}) // prod/app1 at ts2 should now be 12.5

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: ts1, colVal: float64(7.5), colEnv: "prod", colSvc: "app1"},
			{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
			{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

			{colTs: ts2, colVal: float64(12.5), colEnv: "prod", colSvc: "app1"},
			{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app2"},
			{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic COUNT aggregation with record building", func(t *testing.T) {
		agg := newAggregator(10, aggregationOperationCount)
		agg.AddLabels(groupBy)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)
		ts3 := time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC)

		// Add test data
		// ts1: add one datapoint for prod/app1, prod/app2, and dev/app1
		agg.Add(ts1, 10, groupBy, []string{"prod", "app1"})
		agg.Add(ts1, 20, groupBy, []string{"prod", "app2"})
		agg.Add(ts1, 30, groupBy, []string{"dev", "app1"})

		// ts2: add another datapoint for prod/app1, prod/app2, and dev/app2
		agg.Add(ts2, 15, groupBy, []string{"prod", "app1"})
		agg.Add(ts2, 25, groupBy, []string{"prod", "app2"})
		agg.Add(ts2, 35, groupBy, []string{"dev", "app2"})

		// ts3: add another datapoint for prod/app1, prod/app2, and dev/app2
		agg.Add(ts3, 15, groupBy, []string{"prod", "app1"})
		agg.Add(ts3, 25, groupBy, []string{"prod", "app2"})
		agg.Add(ts3, 35, groupBy, []string{"dev", "app2"})

		// Add more datapoints for prod/app1 and prod/app2
		agg.Add(ts1, 5, groupBy, []string{"prod", "app1"})  // prod/app1 at ts1 should now be count 2
		agg.Add(ts2, 10, groupBy, []string{"prod", "app2"}) // prod/app2 at ts2 should now be count 2
		agg.Add(ts1, 25, groupBy, []string{"prod", "app1"}) // prod/app1 at ts1 should now be count 3

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: ts1, colVal: float64(3), colEnv: "prod", colSvc: "app1"},
			{colTs: ts1, colVal: float64(1), colEnv: "prod", colSvc: "app2"},
			{colTs: ts1, colVal: float64(1), colEnv: "dev", colSvc: "app1"},

			{colTs: ts2, colVal: float64(1), colEnv: "prod", colSvc: "app1"},
			{colTs: ts2, colVal: float64(2), colEnv: "prod", colSvc: "app2"},
			{colTs: ts2, colVal: float64(1), colEnv: "dev", colSvc: "app2"},

			{colTs: ts3, colVal: float64(1), colEnv: "prod", colSvc: "app1"},
			{colTs: ts3, colVal: float64(1), colEnv: "prod", colSvc: "app2"},
			{colTs: ts3, colVal: float64(1), colEnv: "dev", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic MAX aggregation with record building", func(t *testing.T) {
		agg := newAggregator(10, aggregationOperationMax)
		agg.AddLabels(groupBy)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		// Add test data
		// ts1: add one datapoint for prod/app1, prod/app2, and dev/app1
		agg.Add(ts1, 10, groupBy, []string{"prod", "app1"})
		agg.Add(ts1, 20, groupBy, []string{"prod", "app2"})
		agg.Add(ts1, 30, groupBy, []string{"dev", "app1"})

		// ts2: add another datapoint for prod/app1, prod/app2, and dev/app2
		agg.Add(ts2, 15, groupBy, []string{"prod", "app1"})
		agg.Add(ts2, 25, groupBy, []string{"prod", "app2"})
		agg.Add(ts2, 35, groupBy, []string{"dev", "app2"})

		// Add more datapoints for prod/app1 and prod/app2
		agg.Add(ts1, 5, groupBy, []string{"prod", "app1"})  // prod/app1 at ts1 should still be 10
		agg.Add(ts2, 50, groupBy, []string{"prod", "app2"}) // prod/app2 at ts2 should now be 50
		agg.Add(ts1, 15, groupBy, []string{"prod", "app1"}) // prod/app1 at ts1 should now be 15

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: ts1, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
			{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

			{colTs: ts2, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: ts2, colVal: float64(50), colEnv: "prod", colSvc: "app2"},
			{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic MIN aggregation with record building", func(t *testing.T) {
		agg := newAggregator(10, aggregationOperationMin)
		agg.AddLabels(groupBy)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		// Add test data
		// ts1: add one datapoint for prod/app1, prod/app2, and dev/app1
		agg.Add(ts1, 10, groupBy, []string{"prod", "app1"})
		agg.Add(ts1, 20, groupBy, []string{"prod", "app2"})
		agg.Add(ts1, 30, groupBy, []string{"dev", "app1"})

		// ts2: add another datapoint for prod/app1, prod/app2, and dev/app2
		agg.Add(ts2, 15, groupBy, []string{"prod", "app1"})
		agg.Add(ts2, 25, groupBy, []string{"prod", "app2"})
		agg.Add(ts2, 35, groupBy, []string{"dev", "app2"})

		// Add more datapoints for prod/app1 and prod/app2
		agg.Add(ts1, 5, groupBy, []string{"prod", "app1"})  // prod/app1 at ts1 should now be 5
		agg.Add(ts2, 40, groupBy, []string{"prod", "app2"}) // prod/app2 at ts2 should still be 25
		agg.Add(ts1, 25, groupBy, []string{"prod", "app1"}) // prod/app1 at ts1 should still be 5

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: ts1, colVal: float64(5), colEnv: "prod", colSvc: "app1"},
			{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
			{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

			{colTs: ts2, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app2"},
			{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("SUM aggregation with empty groupBy", func(t *testing.T) {
		// Empty groupBy represents sum by () or sum(...) - all values aggregated into single group
		groupBy := []arrow.Field{}

		agg := newAggregator(1, aggregationOperationSum)
		agg.AddLabels(groupBy)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		// Add test data
		// ts1: prod/app1 = 10, prod/app2 = 20, dev/app1 = 30
		agg.Add(ts1, 10, groupBy, []string{}) // "prod", "app1"
		agg.Add(ts1, 20, groupBy, []string{}) // "prod", "app2"
		agg.Add(ts1, 30, groupBy, []string{}) // "dev", "app1"

		// ts2: prod/app1 = 15, prod/app2 = 25, dev/app2 = 35
		agg.Add(ts2, 15, groupBy, []string{}) // "prod", "app1"
		agg.Add(ts2, 25, groupBy, []string{}) // "prod", "app2"
		agg.Add(ts2, 35, groupBy, []string{}) // "dev", "app2"

		agg.Add(ts1, 5, groupBy, []string{})  // "prod", "app1"
		agg.Add(ts2, 10, groupBy, []string{}) // "prod", "app1"

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			// ts1: all series aggregated into single value = 65
			{colTs: ts1, colVal: float64(65)},
			// ts2: all series aggregated into single value = 85
			{colTs: ts2, colVal: float64(85)},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic SUM aggregation with without() grouping", func(t *testing.T) {
		agg := newAggregator(10, aggregationOperationSum)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		buildFields := func(names ...string) []arrow.Field {
			result := make([]arrow.Field, len(names))
			for i, name := range names {
				result[i] = semconv.FieldFromIdent(semconv.NewIdentifier(name, types.ColumnTypeLabel, types.Loki.String), true)
			}
			return result
		}

		agg.AddLabels(buildFields("env", "service", "cluster", "method"))

		// Add test data
		agg.Add(ts1, 10, buildFields("env", "service"), []string{"prod", "app1"})
		agg.Add(ts1, 20, buildFields("env", "cluster"), []string{"prod", "east-1"})
		agg.Add(ts1, 30, buildFields("method"), []string{"init"})

		agg.Add(ts2, 15, buildFields("env", "service"), []string{"prod", "app1"})
		agg.Add(ts2, 25, buildFields("env", "cluster"), []string{"prod", "east-1"})
		agg.Add(ts2, 35, buildFields("method"), []string{"init"})

		// Add more data to same groups to test aggregation
		agg.Add(ts1, 5, buildFields("env", "service"), []string{"prod", "app1"})
		agg.Add(ts2, 10, buildFields("env", "cluster"), []string{"prod", "east-1"})

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		colCluster := semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String).FQN()
		colMethod := semconv.NewIdentifier("method", types.ColumnTypeLabel, types.Loki.String).FQN()

		expect := arrowtest.Rows{
			{colTs: ts1, colVal: float64(15), colEnv: "prod", colSvc: "app1", colMethod: nil, colCluster: nil},
			{colTs: ts1, colVal: float64(20), colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
			{colTs: ts1, colVal: float64(30), colMethod: "init", colEnv: nil, colSvc: nil, colCluster: nil},

			{colTs: ts2, colVal: float64(15), colEnv: "prod", colSvc: "app1", colMethod: nil, colCluster: nil},
			{colTs: ts2, colVal: float64(35), colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
			{colTs: ts2, colVal: float64(35), colMethod: "init", colEnv: nil, colSvc: nil, colCluster: nil},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})
}

func BenchmarkAggregator(b *testing.B) {
	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}

	agg := newAggregator(10, aggregationOperationSum)
	agg.AddLabels(fields)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second)
		env := fmt.Sprintf("env-%d", i%3)
		cluster := fmt.Sprintf("cluster-%d", i%10)
		service := fmt.Sprintf("service-%d", i%7)

		agg.Add(ts, 10, fields, []string{env, cluster, service})

	}
}
