package executor

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var groupBy = []arrow.Field{
	semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
	semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
}

// runAggregator runs fn against map and array aggregator storage.
func runAggregator(t *testing.T, op aggregationOperation, fn func(t *testing.T, agg *aggregator)) {
	t.Helper()

	fn(t, newAggregator(0, op))
}

var samplesMem = memory.NewGoAllocator()

func samplesSchema(labelFields []arrow.Field) *arrow.Schema {
	return arrow.NewSchema(append([]arrow.Field{
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	}, labelFields...), nil)
}

// addSamples ingests rows through AddBatch. schema may be nil to infer from labelFields or rows.
func addSamples(agg *aggregator, labelFields []arrow.Field, schema *arrow.Schema, rows arrowtest.Rows) error {
	labelValuesForRow := make([]string, len(labelFields))
	labelFieldsForRow := make([]arrow.Field, len(labelFields))

	for i := 0; i < len(rows); i++ {
		row := rows[i]
		ts := row[colTs].(time.Time)
		val := row[colVal].(float64)
		next := 0
		for _, field := range labelFields {
			if row[field.Name] != nil {
				labelValuesForRow[next] = row[field.Name].(string)
				labelFieldsForRow[next] = field
				next++
				continue
			}
		}
		if err := agg.Add(ts, val, labelFieldsForRow[:next], labelValuesForRow[:next]); err != nil {
			return err
		}
	}
	return nil
}

func mustAddSamples(t *testing.T, agg *aggregator, labelFields []arrow.Field, schema *arrow.Schema, rows arrowtest.Rows) {
	t.Helper()
	require.NoError(t, addSamples(agg, labelFields, schema, rows))
}

func requireAggregatedRows(t *testing.T, agg *aggregator, expect arrowtest.Rows) {
	t.Helper()

	record, err := agg.BuildRecord()
	require.NoError(t, err)

	rows, err := arrowtest.RecordRows(record)
	require.NoError(t, err, "should be able to convert record back to rows")
	require.Equal(t, len(expect), len(rows), "number of rows should match")
	require.ElementsMatch(t, expect, rows)
}

func TestAggregator(t *testing.T) {
	colTs := semconv.ColumnIdentTimestamp.FQN()
	colVal := semconv.ColumnIdentValue.FQN()
	colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String).FQN()
	colSvc := semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String).FQN()

	ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)
	ts3 := time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC)

	basicInput := arrowtest.Rows{
		{colTs: ts1, colVal: 10.0, colEnv: "prod", colSvc: "app1"},
		{colTs: ts1, colVal: 20.0, colEnv: "prod", colSvc: "app2"},
		{colTs: ts1, colVal: 30.0, colEnv: "dev", colSvc: "app1"},
		{colTs: ts2, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
		{colTs: ts2, colVal: 25.0, colEnv: "prod", colSvc: "app2"},
		{colTs: ts2, colVal: 35.0, colEnv: "dev", colSvc: "app2"},
		{colTs: ts1, colVal: 5.0, colEnv: "prod", colSvc: "app1"},
		{colTs: ts2, colVal: 10.0, colEnv: "prod", colSvc: "app1"},
	}

	t.Run("basic SUM aggregation with record building", func(t *testing.T) {
		runAggregator(t, aggregationOperationSum, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(groupBy)
			mustAddSamples(t, agg, groupBy, nil, basicInput)

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

				{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
			})
		})
	})

	t.Run("basic AVG aggregation with record building", func(t *testing.T) {
		runAggregator(t, aggregationOperationAvg, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(groupBy)
			mustAddSamples(t, agg, groupBy, nil, basicInput)

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(7.5), colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

				{colTs: ts2, colVal: float64(12.5), colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
			})
		})
	})

	t.Run("basic COUNT aggregation with record building", func(t *testing.T) {
		runAggregator(t, aggregationOperationCount, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(groupBy)
			mustAddSamples(t, agg, groupBy, nil, arrowtest.Rows{
				{colTs: ts1, colVal: 10.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: 20.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: 30.0, colEnv: "dev", colSvc: "app1"},
				{colTs: ts2, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: 25.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: 35.0, colEnv: "dev", colSvc: "app2"},
				{colTs: ts3, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts3, colVal: 25.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts3, colVal: 35.0, colEnv: "dev", colSvc: "app2"},
				{colTs: ts1, colVal: 5.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: 10.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: 25.0, colEnv: "prod", colSvc: "app1"},
			})

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(3), colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: float64(1), colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: float64(1), colEnv: "dev", colSvc: "app1"},

				{colTs: ts2, colVal: float64(1), colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: float64(2), colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: float64(1), colEnv: "dev", colSvc: "app2"},

				{colTs: ts3, colVal: float64(1), colEnv: "prod", colSvc: "app1"},
				{colTs: ts3, colVal: float64(1), colEnv: "prod", colSvc: "app2"},
				{colTs: ts3, colVal: float64(1), colEnv: "dev", colSvc: "app2"},
			})
		})
	})

	t.Run("basic MAX aggregation with record building", func(t *testing.T) {
		runAggregator(t, aggregationOperationMax, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(groupBy)
			mustAddSamples(t, agg, groupBy, nil, arrowtest.Rows{
				{colTs: ts1, colVal: 10.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: 20.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: 30.0, colEnv: "dev", colSvc: "app1"},
				{colTs: ts2, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: 25.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: 35.0, colEnv: "dev", colSvc: "app2"},
				{colTs: ts1, colVal: 5.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: 50.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
			})

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

				{colTs: ts2, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: float64(50), colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
			})
		})
	})

	t.Run("basic MIN aggregation with record building", func(t *testing.T) {
		runAggregator(t, aggregationOperationMin, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(groupBy)
			mustAddSamples(t, agg, groupBy, nil, arrowtest.Rows{
				{colTs: ts1, colVal: 10.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: 20.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: 30.0, colEnv: "dev", colSvc: "app1"},
				{colTs: ts2, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: 25.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: 35.0, colEnv: "dev", colSvc: "app2"},
				{colTs: ts1, colVal: 5.0, colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: 40.0, colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: 25.0, colEnv: "prod", colSvc: "app1"},
			})

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(5), colEnv: "prod", colSvc: "app1"},
				{colTs: ts1, colVal: float64(20), colEnv: "prod", colSvc: "app2"},
				{colTs: ts1, colVal: float64(30), colEnv: "dev", colSvc: "app1"},

				{colTs: ts2, colVal: float64(15), colEnv: "prod", colSvc: "app1"},
				{colTs: ts2, colVal: float64(25), colEnv: "prod", colSvc: "app2"},
				{colTs: ts2, colVal: float64(35), colEnv: "dev", colSvc: "app2"},
			})
		})
	})

	t.Run("SUM aggregation with empty groupBy", func(t *testing.T) {
		emptyGroupBy := []arrow.Field{}
		emptyInput := arrowtest.Rows{
			{colTs: ts1, colVal: 10.0},
			{colTs: ts1, colVal: 20.0},
			{colTs: ts1, colVal: 30.0},
			{colTs: ts2, colVal: 15.0},
			{colTs: ts2, colVal: 25.0},
			{colTs: ts2, colVal: 35.0},
			{colTs: ts1, colVal: 5.0},
			{colTs: ts2, colVal: 10.0},
		}

		runAggregator(t, aggregationOperationSum, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(emptyGroupBy)
			mustAddSamples(t, agg, emptyGroupBy, nil, emptyInput)

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(65)},
				{colTs: ts2, colVal: float64(85)},
			})
		})
	})

	t.Run("basic SUM aggregation with without() grouping", func(t *testing.T) {
		colCluster := semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String).FQN()
		colMethod := semconv.NewIdentifier("method", types.ColumnTypeLabel, types.Loki.String).FQN()

		allLabelFields := []arrow.Field{
			semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
			semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
			semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
			semconv.FieldFromIdent(semconv.NewIdentifier("method", types.ColumnTypeLabel, types.Loki.String), true),
		}

		withoutInput := arrowtest.Rows{
			{colTs: ts1, colVal: 10.0, colEnv: "prod", colSvc: "app1", colCluster: nil, colMethod: nil},
			{colTs: ts1, colVal: 20.0, colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
			{colTs: ts1, colVal: 30.0, colMethod: "init", colEnv: nil, colSvc: nil, colCluster: nil},
			{colTs: ts2, colVal: 15.0, colEnv: "prod", colSvc: "app1", colCluster: nil, colMethod: nil},
			{colTs: ts2, colVal: 25.0, colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
			{colTs: ts2, colVal: 35.0, colMethod: "init", colEnv: nil, colSvc: nil, colCluster: nil},
			{colTs: ts1, colVal: 5.0, colEnv: "prod", colSvc: "app1", colCluster: nil, colMethod: nil},
			{colTs: ts2, colVal: 10.0, colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
		}

		runAggregator(t, aggregationOperationSum, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(allLabelFields)
			mustAddSamples(t, agg, allLabelFields, samplesSchema(allLabelFields), withoutInput)

			requireAggregatedRows(t, agg, arrowtest.Rows{
				{colTs: ts1, colVal: float64(15), colEnv: "prod", colSvc: "app1", colMethod: nil, colCluster: nil},
				{colTs: ts1, colVal: float64(20), colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
				{colTs: ts1, colVal: float64(30), colMethod: "init", colEnv: nil, colSvc: nil, colCluster: nil},

				{colTs: ts2, colVal: float64(15), colEnv: "prod", colSvc: "app1", colMethod: nil, colCluster: nil},
				{colTs: ts2, colVal: float64(35), colEnv: "prod", colCluster: "east-1", colSvc: nil, colMethod: nil},
				{colTs: ts2, colVal: float64(35), colMethod: "init", colEnv: nil, colSvc: nil, colCluster: nil},
			})
		})
	})

	t.Run("series limit enforcement", func(t *testing.T) {
		initialInput := arrowtest.Rows{
			{colTs: ts1, colVal: 10.0, colEnv: "prod", colSvc: "app1"},
			{colTs: ts1, colVal: 20.0, colEnv: "prod", colSvc: "app2"},
			{colTs: ts1, colVal: 30.0, colEnv: "dev", colSvc: "app1"},
		}
		overflowInput := arrowtest.Rows{
			{colTs: ts2, colVal: 10.0, colEnv: "dev", colSvc: "app2"},
		}
		afterLimitInput := arrowtest.Rows{
			{colTs: ts2, colVal: 15.0, colEnv: "prod", colSvc: "app1"},
		}
		finalInput := arrowtest.Rows{
			{colTs: ts2, colVal: 10.0, colEnv: "dev", colSvc: "app2"},
		}

		runAggregator(t, aggregationOperationSum, func(t *testing.T, agg *aggregator) {
			agg.AddLabels(groupBy)
			agg.SetMaxSeries(3)

			require.NoError(t, addSamples(agg, groupBy, nil, initialInput))

			err := addSamples(agg, groupBy, nil, overflowInput)
			require.Error(t, err)
			require.True(t, errors.Is(err, ErrSeriesLimitExceeded))

			require.NoError(t, addSamples(agg, groupBy, nil, afterLimitInput))

			agg.SetMaxSeries(0)
			require.NoError(t, addSamples(agg, groupBy, nil, finalInput))
		})
	})
}

func TestAggregator_computeGroupKeyFromColumns(t *testing.T) {
	colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String).FQN()
	colSvc := semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String).FQN()
	colCluster := semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String).FQN()

	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
	}

	cases := []struct {
		name      string
		rows      arrowtest.Rows
		row       int
		labels    []arrow.Field
		values    []string
		colFields []arrow.Field
		colIdx    []int
	}{
		{
			name: "by grouping",
			rows: arrowtest.Rows{
				{colEnv: "prod", colSvc: "app1", colCluster: "east-1"},
			},
			row:       0,
			labels:    fields[:2],
			values:    []string{"prod", "app1"},
			colFields: fields[:2],
			colIdx:    []int{0, 1},
		},
		{
			name: "sparse without grouping",
			rows: arrowtest.Rows{
				{colEnv: "prod", colSvc: nil, colCluster: "east-1"},
			},
			row:       0,
			labels:    []arrow.Field{fields[0], fields[2]},
			values:    []string{"prod", "east-1"},
			colFields: fields,
			colIdx:    []int{0, 2},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := tc.rows.Record(memory.NewGoAllocator(), arrow.NewSchema(fields, nil))
			t.Cleanup(func() { record.Release() })

			labelCols := make([]*array.String, len(tc.colIdx))
			labelFields := make([]arrow.Field, len(tc.colIdx))
			for i, idx := range tc.colIdx {
				labelCols[i] = record.Column(idx).(*array.String)
				labelFields[i] = tc.colFields[idx]
			}

			agg := newAggregator(0, aggregationOperationSum)
			require.Equal(t,
				agg.computeGroupKey(tc.labels, tc.values),
				agg.computeGroupKeyFromColumns(labelCols, labelFields, tc.row),
			)
		})
	}
}

var benchGroupingFields = []arrow.Field{
	semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
	semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
	semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
}

// benchRows builds n samples. interleaved rotates label values; otherwise uses one series.
func benchRows(interleaved bool, fields []arrow.Field, startTs time.Time, step time.Duration, numPoints, n int) arrowtest.Rows {
	colTs := semconv.ColumnIdentTimestamp.FQN()
	colVal := semconv.ColumnIdentValue.FQN()

	rows := make(arrowtest.Rows, n)
	for i := range n {
		ts := startTs.Add(step * time.Duration(i%numPoints))
		row := arrowtest.Row{colTs: ts, colVal: 10.0}
		if interleaved {
			row[fields[0].Name] = fmt.Sprintf("env-%d", i%3)
			row[fields[1].Name] = fmt.Sprintf("cluster-%d", i%10)
			row[fields[2].Name] = fmt.Sprintf("service-%d", i%7)
		} else {
			row[fields[0].Name] = "prod"
			row[fields[1].Name] = "app1"
			row[fields[2].Name] = "east-1"
		}
		rows[i] = row
	}
	return rows
}

func BenchmarkAggregatorAdd(b *testing.B) {
	const (
		numPoints = 10_000
		batchSize = 1024
	)
	startTs := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	step := time.Second

	fields := benchGroupingFields
	schema := samplesSchema(fields)

	for _, tc := range []struct {
		name        string
		interleaved bool
	}{
		{"interleaved", true},
		{"single_series", false},
	} {
		batch := benchRows(tc.interleaved, fields, startTs, step, numPoints, batchSize)

		b.Run("case="+tc.name, func(b *testing.B) {
			var agg *aggregator
			agg = newAggregator(0, aggregationOperationSum)
			agg.AddLabels(fields)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := addSamples(agg, fields, schema, batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAggregatorBuildRecord(b *testing.B) {
	const (
		numPoints = 8192
		batchSize = 1024
	)
	startTs := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	step := time.Second

	fields := benchGroupingFields
	schema := samplesSchema(fields)

	agg := newAggregator(0, aggregationOperationSum)
	agg.AddLabels(fields)

	for offset := 0; offset < numPoints; offset += batchSize {
		n := batchSize
		if offset+n > numPoints {
			n = numPoints - offset
		}
		batch := benchRows(true, fields, startTs.Add(step*time.Duration(offset)), step, numPoints, n)
		if err := addSamples(agg, fields, schema, batch); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agg.BuildRecord()
	}
}
