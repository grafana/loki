package engine

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"

	"github.com/grafana/loki/pkg/push"
)

func TestStreamsResultBuilder(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newStreamsResultBuilder()
		md, _ := metadata.NewContext(t.Context())
		require.NotNil(t, builder.Build(stats.Result{}, md).Data)
	})

	t.Run("rows without log line, timestamp, or labels are ignored", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeMetadata, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, true),  // in practice timestamp is not nullable!
				semconv.FieldFromIdent(colMsg, true), // in practice message is not nullable!
				semconv.FieldFromIdent(colEnv, true),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{
				colTs.FQN():  time.Unix(0, 1620000000000000001).UTC(),
				colMsg.FQN(): nil,
				colEnv.FQN(): "prod",
			},
			{
				colTs.FQN():  nil,
				colMsg.FQN(): "log line",
				colEnv.FQN(): "prod",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000003).UTC(),
				colMsg.FQN(): "log line",
				colEnv.FQN(): nil,
			},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newStreamsResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 0, builder.Len(), "expected no entries to be collected")
	})

	t.Run("successful conversion of labels, log line, timestamp, and structured metadata ", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)
		colNs := semconv.NewIdentifier("namespace", types.ColumnTypeLabel, types.Loki.String)
		colTid := semconv.NewIdentifier("traceID", types.ColumnTypeMetadata, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, true),
				semconv.FieldFromIdent(colNs, true),
				semconv.FieldFromIdent(colTid, true),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{
				colTs.FQN():  time.Unix(0, 1620000000000000001).UTC(),
				colMsg.FQN(): "log line 1",
				colEnv.FQN(): "dev",
				colNs.FQN():  "loki-dev-001",
				colTid.FQN(): "860e403fcf754312",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000002).UTC(),
				colMsg.FQN(): "log line 2",
				colEnv.FQN(): "prod",
				colNs.FQN():  "loki-prod-001",
				colTid.FQN(): "46ce02549441e41c",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000003).UTC(),
				colMsg.FQN(): "log line 3",
				colEnv.FQN(): "dev",
				colNs.FQN():  "loki-dev-002",
				colTid.FQN(): "61330481e1e59b18",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000004).UTC(),
				colMsg.FQN(): "log line 4",
				colEnv.FQN(): "prod",
				colNs.FQN():  "loki-prod-001",
				colTid.FQN(): "40e50221e284b9d2",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000005).UTC(),
				colMsg.FQN(): "log line 5",
				colEnv.FQN(): "dev",
				colNs.FQN():  "loki-dev-002",
				colTid.FQN(): "0cf883f112ad239b",
			},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newStreamsResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 5, builder.Len())

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		require.Equal(t, 5, result.Data.(logqlmodel.Streams).Len())

		expected := logqlmodel.Streams{
			push.Stream{
				Labels: labels.FromStrings("env", "dev", "namespace", "loki-dev-001", "traceID", "860e403fcf754312").String(),
				Entries: []logproto.Entry{
					{Line: "log line 1", Timestamp: time.Unix(0, 1620000000000000001), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "860e403fcf754312")), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "dev", "namespace", "loki-dev-002", "traceID", "0cf883f112ad239b").String(),
				Entries: []logproto.Entry{
					{Line: "log line 5", Timestamp: time.Unix(0, 1620000000000000005), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "0cf883f112ad239b")), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "dev", "namespace", "loki-dev-002", "traceID", "61330481e1e59b18").String(),
				Entries: []logproto.Entry{
					{Line: "log line 3", Timestamp: time.Unix(0, 1620000000000000003), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "61330481e1e59b18")), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "prod", "namespace", "loki-prod-001", "traceID", "40e50221e284b9d2").String(),
				Entries: []logproto.Entry{
					{Line: "log line 4", Timestamp: time.Unix(0, 1620000000000000004), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "40e50221e284b9d2")), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "prod", "namespace", "loki-prod-001", "traceID", "46ce02549441e41c").String(),
				Entries: []logproto.Entry{
					{Line: "log line 2", Timestamp: time.Unix(0, 1620000000000000002), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "46ce02549441e41c")), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
		}
		require.Equal(t, expected, result.Data.(logqlmodel.Streams))
	})
}

func TestVectorResultBuilder(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newVectorResultBuilder()
		md, _ := metadata.NewContext(t.Context())
		require.NotNil(t, builder.Build(stats.Result{}, md).Data)
	})

	t.Run("successful conversion of vector data", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colVal := semconv.ColumnIdentValue
		colInst := semconv.NewIdentifier("instance", types.ColumnTypeMetadata, types.Loki.String)
		colJob := semconv.NewIdentifier("job", types.ColumnTypeMetadata, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colVal, false),
				semconv.FieldFromIdent(colInst, false),
				semconv.FieldFromIdent(colJob, false),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): float64(42), colInst.FQN(): "localhost:9090", colJob.FQN(): "prometheus"},
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): float64(23), colInst.FQN(): "localhost:9100", colJob.FQN(): "node-exporter"},
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): float64(32), colInst.FQN(): nil, colJob.FQN(): "node-exporter"},
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): float64(15), colInst.FQN(): "localhost:9100", colJob.FQN(): "prometheus"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newVectorResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 4, builder.Len())

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		vector := result.Data.(promql.Vector)
		require.Equal(t, 4, len(vector))

		expected := promql.Vector{
			{
				T:      int64(1620000000000),
				F:      42.0,
				Metric: labels.FromStrings("instance", "localhost:9090", "job", "prometheus"),
			},
			{
				T:      int64(1620000000000),
				F:      23.0,
				Metric: labels.FromStrings("instance", "localhost:9100", "job", "node-exporter"),
			},
			{
				T:      int64(1620000000000),
				F:      15.0,
				Metric: labels.FromStrings("instance", "localhost:9100", "job", "prometheus"),
			},
			{
				T:      int64(1620000000000),
				F:      32.0,
				Metric: labels.FromStrings("job", "node-exporter"),
			},
		}
		require.Equal(t, expected, vector)
	})

	// TODO:(ashwanth) also enforce grouping labels are all present?
	t.Run("rows without timestamp or value are ignored", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colVal := semconv.ColumnIdentValue
		colInst := semconv.NewIdentifier("instance", types.ColumnTypeMetadata, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colVal, false),
				semconv.FieldFromIdent(colInst, false),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{colTs.FQN(): nil, colVal.FQN(): float64(42), colInst.FQN(): "localhost:9090"},
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): nil, colInst.FQN(): "localhost:9100"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newVectorResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 0, builder.Len(), "expected no samples to be collected")
	})
}

func TestMatrixResultBuilder(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newMatrixResultBuilder()
		md, _ := metadata.NewContext(t.Context())
		require.NotNil(t, builder.Build(stats.Result{}, md).Data)
	})

	t.Run("successful conversion of matrix data", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colVal := semconv.ColumnIdentValue
		colInst := semconv.NewIdentifier("instance", types.ColumnTypeMetadata, types.Loki.String)
		colJob := semconv.NewIdentifier("job", types.ColumnTypeMetadata, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colVal, false),
				semconv.FieldFromIdent(colInst, false),
				semconv.FieldFromIdent(colJob, false),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): float64(42), colInst.FQN(): "localhost:9090", colJob.FQN(): "prometheus"},
			{colTs.FQN(): time.Unix(0, 1620000001000000000).UTC(), colVal.FQN(): float64(43), colInst.FQN(): "localhost:9090", colJob.FQN(): "prometheus"},
			{colTs.FQN(): time.Unix(0, 1620000002000000000).UTC(), colVal.FQN(): float64(44), colInst.FQN(): "localhost:9090", colJob.FQN(): "prometheus"},
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colVal.FQN(): float64(23), colInst.FQN(): "localhost:9100", colJob.FQN(): "node-exporter"},
			{colTs.FQN(): time.Unix(0, 1620000001000000000).UTC(), colVal.FQN(): float64(24), colInst.FQN(): "localhost:9100", colJob.FQN(): "node-exporter"},
			{colTs.FQN(): time.Unix(0, 1620000002000000000).UTC(), colVal.FQN(): float64(25), colInst.FQN(): "localhost:9100", colJob.FQN(): "node-exporter"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newMatrixResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 6, builder.Len())

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		matrix := result.Data.(promql.Matrix)
		require.Equal(t, 2, len(matrix))

		expected := promql.Matrix{
			{
				Metric: labels.FromStrings("instance", "localhost:9090", "job", "prometheus"),
				Floats: []promql.FPoint{
					{T: int64(1620000000000), F: 42.0},
					{T: int64(1620000001000), F: 43.0},
					{T: int64(1620000002000), F: 44.0},
				},
			},
			{
				Metric: labels.FromStrings("instance", "localhost:9100", "job", "node-exporter"),
				Floats: []promql.FPoint{
					{T: int64(1620000000000), F: 23.0},
					{T: int64(1620000001000), F: 24.0},
					{T: int64(1620000002000), F: 25.0},
				},
			},
		}
		require.Equal(t, expected, matrix)
	})
}
