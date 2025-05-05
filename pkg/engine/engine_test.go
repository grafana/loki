package engine

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"

	"github.com/grafana/loki/pkg/push"
)

func createRecord(t *testing.T, schema *arrow.Schema, data [][]interface{}) arrow.Record {
	mem := memory.NewGoAllocator()
	builder := array.NewRecordBuilder(mem, schema)
	defer builder.Release()

	for _, row := range data {
		for j, val := range row {
			if val == nil {
				builder.Field(j).AppendNull()
				continue
			}

			switch builder.Field(j).(type) {
			case *array.BooleanBuilder:
				builder.Field(j).(*array.StringBuilder).Append(val.(string))
			case *array.StringBuilder:
				builder.Field(j).(*array.StringBuilder).Append(val.(string))
			case *array.Uint64Builder:
				builder.Field(j).(*array.Uint64Builder).Append(val.(uint64))
			case *array.Int64Builder:
				builder.Field(j).(*array.Int64Builder).Append(val.(int64))
			case *array.Float64Builder:
				builder.Field(j).(*array.Float64Builder).Append(val.(float64))
			default:
				t.Fatal("invalid field type")
			}
		}
	}

	return builder.NewRecord()
}

func TestConvertArrowRecordsToLokiResult(t *testing.T) {
	mdTypeBuiltin := arrow.NewMetadata([]string{types.ColumnTypeMetadataKey}, []string{types.ColumnTypeBuiltin.String()})
	mdTypeLabel := arrow.NewMetadata([]string{types.ColumnTypeMetadataKey}, []string{types.ColumnTypeLabel.String()})
	mdTypeMetadata := arrow.NewMetadata([]string{types.ColumnTypeMetadataKey}, []string{types.ColumnTypeMetadata.String()})

	t.Run("rows without log line, timestamp, or labels are ignored", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.PrimitiveTypes.Uint64, Metadata: mdTypeBuiltin},
				{Name: types.ColumnNameBuiltinLine, Type: arrow.BinaryTypes.String, Metadata: mdTypeBuiltin},
				{Name: "env", Type: arrow.BinaryTypes.String, Metadata: mdTypeLabel},
			},
			nil,
		)

		data := [][]interface{}{
			{uint64(1620000000000000001), nil, "prod"},
			{nil, "log line", "prod"},
			{uint64(1620000000000000003), "log line", nil},
		}

		record := createRecord(t, schema, data)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 0, builder.len(), "expected no entries to be collected")
	})

	t.Run("fields without metadata are ignored", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.PrimitiveTypes.Uint64},
				{Name: types.ColumnNameBuiltinLine, Type: arrow.BinaryTypes.String},
				{Name: "env", Type: arrow.BinaryTypes.String},
			},
			nil,
		)

		data := [][]interface{}{
			{uint64(1620000000000000001), "log line 1", "prod"},
			{uint64(1620000000000000002), "log line 2", "prod"},
			{uint64(1620000000000000003), "log line 3", "prod"},
		}

		record := createRecord(t, schema, data)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 0, builder.len(), "expected no entries to be collected")
	})

	t.Run("successful conversion of labels, log line, timestamp, and structured metadata ", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.PrimitiveTypes.Uint64, Metadata: mdTypeBuiltin},
				{Name: types.ColumnNameBuiltinLine, Type: arrow.BinaryTypes.String, Metadata: mdTypeBuiltin},
				{Name: "env", Type: arrow.BinaryTypes.String, Metadata: mdTypeLabel},
				{Name: "namespace", Type: arrow.BinaryTypes.String, Metadata: mdTypeLabel},
				{Name: "traceID", Type: arrow.BinaryTypes.String, Metadata: mdTypeMetadata},
			},
			nil,
		)

		data := [][]interface{}{
			{uint64(1620000000000000001), "log line 1", "dev", "loki-dev-001", "860e403fcf754312"},
			{uint64(1620000000000000002), "log line 2", "prod", "loki-prod-001", "46ce02549441e41c"},
			{uint64(1620000000000000003), "log line 3", "dev", "loki-dev-002", "61330481e1e59b18"},
			{uint64(1620000000000000004), "log line 4", "prod", "loki-prod-001", "40e50221e284b9d2"},
			{uint64(1620000000000000005), "log line 5", "dev", "loki-dev-002", "0cf883f112ad239b"},
		}

		record := createRecord(t, schema, data)
		defer record.Release()

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 5, builder.len())

		result := builder.build()
		require.Equal(t, 3, result.Data.(logqlmodel.Streams).Len())

		expected := logqlmodel.Streams{
			push.Stream{
				Labels: labels.FromStrings("env", "dev", "namespace", "loki-dev-001").String(),
				Entries: []logproto.Entry{
					{Line: "log line 1", Timestamp: time.Unix(0, 1620000000000000001), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "860e403fcf754312"))},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "prod", "namespace", "loki-prod-001").String(),
				Entries: []logproto.Entry{
					{Line: "log line 2", Timestamp: time.Unix(0, 1620000000000000002), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "46ce02549441e41c"))},
					{Line: "log line 4", Timestamp: time.Unix(0, 1620000000000000004), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "40e50221e284b9d2"))},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "dev", "namespace", "loki-dev-002").String(),
				Entries: []logproto.Entry{
					{Line: "log line 3", Timestamp: time.Unix(0, 1620000000000000003), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "61330481e1e59b18"))},
					{Line: "log line 5", Timestamp: time.Unix(0, 1620000000000000005), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "0cf883f112ad239b"))},
				},
			},
		}
		require.Equal(t, expected, result.Data.(logqlmodel.Streams))
	})
}
