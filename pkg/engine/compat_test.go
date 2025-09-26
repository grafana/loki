package engine

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/push"
)

func TestStreamsResultBuilder(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	mdTypeLabel := datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.Loki.String)
	mdTypeMetadata := datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)

	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newStreamsResultBuilder()
		md, _ := metadata.NewContext(t.Context())
		require.NotNil(t, builder.Build(stats.Result{}, md).Data)
	})

	t.Run("rows without log line, timestamp, or labels are ignored", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadataBuiltinMessage},
				{Name: "env", Type: arrow.BinaryTypes.String, Metadata: mdTypeLabel},
			},
			nil,
		)
		rows := arrowtest.Rows{
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000001).UTC(), types.ColumnNameBuiltinMessage: nil, "env": "prod"},
			{types.ColumnNameBuiltinTimestamp: nil, types.ColumnNameBuiltinMessage: "log line", "env": "prod"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000003).UTC(), types.ColumnNameBuiltinMessage: "log line", "env": nil},
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

	t.Run("fields without metadata are ignored", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns},
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "env", Type: arrow.BinaryTypes.String},
			},
			nil,
		)

		rows := arrowtest.Rows{
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000001).UTC(), types.ColumnNameBuiltinMessage: "log line 1", "env": "prod"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000002).UTC(), types.ColumnNameBuiltinMessage: "log line 2", "env": "prod"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000003).UTC(), types.ColumnNameBuiltinMessage: "log line 3", "env": "prod"},
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
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadataBuiltinMessage},
				{Name: "env", Type: arrow.BinaryTypes.String, Metadata: mdTypeLabel},
				{Name: "namespace", Type: arrow.BinaryTypes.String, Metadata: mdTypeLabel},
				{Name: "traceID", Type: arrow.BinaryTypes.String, Metadata: mdTypeMetadata},
			},
			nil,
		)

		rows := arrowtest.Rows{
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000001).UTC(), types.ColumnNameBuiltinMessage: "log line 1", "env": "dev", "namespace": "loki-dev-001", "traceID": "860e403fcf754312"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000002).UTC(), types.ColumnNameBuiltinMessage: "log line 2", "env": "prod", "namespace": "loki-prod-001", "traceID": "46ce02549441e41c"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000003).UTC(), types.ColumnNameBuiltinMessage: "log line 3", "env": "dev", "namespace": "loki-dev-002", "traceID": "61330481e1e59b18"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000004).UTC(), types.ColumnNameBuiltinMessage: "log line 4", "env": "prod", "namespace": "loki-prod-001", "traceID": "40e50221e284b9d2"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000005).UTC(), types.ColumnNameBuiltinMessage: "log line 5", "env": "dev", "namespace": "loki-dev-002", "traceID": "0cf883f112ad239b"},
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
	mdTypeString := datatype.ColumnMetadata(types.ColumnTypeAmbiguous, datatype.Loki.String)
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newVectorResultBuilder()
		md, _ := metadata.NewContext(t.Context())
		require.NotNil(t, builder.Build(stats.Result{}, md).Data)
	})

	t.Run("successful conversion of vector data", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
				{Name: types.ColumnNameGeneratedValue, Type: arrow.PrimitiveTypes.Int64, Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Integer)},
				{Name: "instance", Type: arrow.BinaryTypes.String, Metadata: mdTypeString},
				{Name: "job", Type: arrow.BinaryTypes.String, Metadata: mdTypeString},
			},
			nil,
		)

		rows := arrowtest.Rows{
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000000).UTC(), types.ColumnNameGeneratedValue: int64(42), "instance": "localhost:9090", "job": "prometheus"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000000).UTC(), types.ColumnNameGeneratedValue: int64(23), "instance": "localhost:9100", "job": "node-exporter"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000000).UTC(), types.ColumnNameGeneratedValue: int64(15), "instance": "localhost:9100", "job": "prometheus"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newVectorResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 3, builder.Len())

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		vector := result.Data.(promql.Vector)
		require.Equal(t, 3, len(vector))

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
		}
		require.Equal(t, expected, vector)
	})

	// TODO:(ashwanth) also enforce grouping labels are all present?
	t.Run("rows without timestamp or value are ignored", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
				{Name: types.ColumnNameGeneratedValue, Type: arrow.PrimitiveTypes.Int64, Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Integer)},
				{Name: "instance", Type: arrow.BinaryTypes.String, Metadata: mdTypeString},
			},
			nil,
		)

		rows := arrowtest.Rows{
			{types.ColumnNameBuiltinTimestamp: nil, types.ColumnNameGeneratedValue: int64(42), "instance": "localhost:9090"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000000).UTC(), types.ColumnNameGeneratedValue: nil, "instance": "localhost:9100"},
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
	mdTypeString := datatype.ColumnMetadata(types.ColumnTypeAmbiguous, datatype.Loki.String)
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newMatrixResultBuilder()
		md, _ := metadata.NewContext(t.Context())
		require.NotNil(t, builder.Build(stats.Result{}, md).Data)
	})

	t.Run("successful conversion of matrix data", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
				{Name: types.ColumnNameGeneratedValue, Type: arrow.PrimitiveTypes.Int64, Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Integer)},
				{Name: "instance", Type: arrow.BinaryTypes.String, Metadata: mdTypeString},
				{Name: "job", Type: arrow.BinaryTypes.String, Metadata: mdTypeString},
			},
			nil,
		)

		rows := arrowtest.Rows{
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000000).UTC(), types.ColumnNameGeneratedValue: int64(42), "instance": "localhost:9090", "job": "prometheus"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000001000000000).UTC(), types.ColumnNameGeneratedValue: int64(43), "instance": "localhost:9090", "job": "prometheus"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000002000000000).UTC(), types.ColumnNameGeneratedValue: int64(44), "instance": "localhost:9090", "job": "prometheus"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000000000000000).UTC(), types.ColumnNameGeneratedValue: int64(23), "instance": "localhost:9100", "job": "node-exporter"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000001000000000).UTC(), types.ColumnNameGeneratedValue: int64(24), "instance": "localhost:9100", "job": "node-exporter"},
			{types.ColumnNameBuiltinTimestamp: time.Unix(0, 1620000002000000000).UTC(), types.ColumnNameGeneratedValue: int64(25), "instance": "localhost:9100", "job": "node-exporter"},
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
