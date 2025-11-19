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
	t.Run("empty builder returns non-nil result", func(t *testing.T) {
		builder := newStreamsResultBuilder(logproto.BACKWARD)
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

		record := rows.Record(memory.DefaultAllocator, schema)

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newStreamsResultBuilder(logproto.BACKWARD)
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
				colTs.FQN():  nil,
				colMsg.FQN(): "log line 0 (must be skipped)",
				colEnv.FQN(): "dev",
				colNs.FQN():  "loki-dev-001",
				colTid.FQN(): "860e403fcf754312",
			},
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
			{
				colTs.FQN():  time.Unix(0, 1620000000000000006).UTC(),
				colMsg.FQN(): "log line 6",
				colEnv.FQN(): "dev",
				colNs.FQN():  nil,
				colTid.FQN(): "9de325g124ad230b",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newStreamsResultBuilder(logproto.BACKWARD)
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 6, builder.Len())

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		require.Equal(t, 6, result.Data.(logqlmodel.Streams).Len())

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
				Labels: labels.FromStrings("env", "dev", "traceID", "9de325g124ad230b").String(),
				Entries: []logproto.Entry{
					{Line: "log line 6", Timestamp: time.Unix(0, 1620000000000000006), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "9de325g124ad230b")), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
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

	t.Run("multiple records with different streams are accumulated correctly", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, false),
			},
			nil,
		)

		// First record: prod and dev streams
		rows1 := arrowtest.Rows{
			{
				colTs.FQN():  time.Unix(0, 1620000000000000001).UTC(),
				colMsg.FQN(): "log line 1",
				colEnv.FQN(): "prod",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000002).UTC(),
				colMsg.FQN(): "log line 2",
				colEnv.FQN(): "dev",
			},
		}
		record1 := rows1.Record(memory.DefaultAllocator, schema)
		defer record1.Release()

		// Second record: prod and staging streams
		rows2 := arrowtest.Rows{
			{
				colTs.FQN():  time.Unix(0, 1620000000000000003).UTC(),
				colMsg.FQN(): "log line 3",
				colEnv.FQN(): "prod",
			},
			{
				colTs.FQN():  time.Unix(0, 1620000000000000004).UTC(),
				colMsg.FQN(): "log line 4",
				colEnv.FQN(): "staging",
			},
		}
		record2 := rows2.Record(memory.DefaultAllocator, schema)
		defer record2.Release()

		builder := newStreamsResultBuilder(logproto.FORWARD)

		// Collect first record
		builder.CollectRecord(record1)
		require.Equal(t, 2, builder.Len(), "should have 2 entries after first record")

		// Collect second record
		builder.CollectRecord(record2)
		require.Equal(t, 4, builder.Len(), "should have 4 entries total after second record")

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		streams := result.Data.(logqlmodel.Streams)
		// Note: 3 unique streams (dev, prod, staging), but 4 total entries
		// The prod stream has 2 entries (one from each record)
		require.Equal(t, 3, len(streams), "should have 3 unique streams")

		// Verify stream grouping - prod stream should have entries from both records
		expected := logqlmodel.Streams{
			push.Stream{
				Labels: labels.FromStrings("env", "dev").String(),
				Entries: []logproto.Entry{
					{Line: "log line 2", Timestamp: time.Unix(0, 1620000000000000002), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.Labels{}), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "prod").String(),
				Entries: []logproto.Entry{
					{Line: "log line 1", Timestamp: time.Unix(0, 1620000000000000001), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.Labels{}), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
					{Line: "log line 3", Timestamp: time.Unix(0, 1620000000000000003), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.Labels{}), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("env", "staging").String(),
				Entries: []logproto.Entry{
					{Line: "log line 4", Timestamp: time.Unix(0, 1620000000000000004), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.Labels{}), Parsed: logproto.FromLabelsToLabelAdapters(labels.Labels{})},
				},
			},
		}
		require.Equal(t, expected, streams)
	})

	t.Run("buffer reuse with varying record sizes", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, false),
			},
			nil,
		)

		builder := newStreamsResultBuilder(logproto.BACKWARD)

		// First record: 5 rows (buffer grows to 5)
		rows1 := make(arrowtest.Rows, 5)
		for i := 0; i < 5; i++ {
			rows1[i] = arrowtest.Row{
				colTs.FQN():  time.Unix(0, int64(1620000000000000001+i)).UTC(),
				colMsg.FQN(): "log line",
				colEnv.FQN(): "prod",
			}
		}
		record1 := rows1.Record(memory.DefaultAllocator, schema)
		builder.CollectRecord(record1)
		record1.Release()
		require.Equal(t, 5, builder.Len())
		require.Equal(t, 5, len(builder.rowBuilders), "buffer should have 5 rowBuilders")

		// Second record: 2 rows (buffer shrinks to 2)
		rows2 := make(arrowtest.Rows, 2)
		for i := 0; i < 2; i++ {
			rows2[i] = arrowtest.Row{
				colTs.FQN():  time.Unix(0, int64(1620000000000000010+i)).UTC(),
				colMsg.FQN(): "log line",
				colEnv.FQN(): "dev",
			}
		}
		record2 := rows2.Record(memory.DefaultAllocator, schema)
		builder.CollectRecord(record2)
		record2.Release()
		require.Equal(t, 7, builder.Len())
		require.Equal(t, 2, len(builder.rowBuilders), "buffer should shrink to 2 rowBuilders")

		// Third record: 10 rows (buffer grows to 10)
		rows3 := make(arrowtest.Rows, 10)
		for i := 0; i < 10; i++ {
			rows3[i] = arrowtest.Row{
				colTs.FQN():  time.Unix(0, int64(1620000000000000020+i)).UTC(),
				colMsg.FQN(): "log line",
				colEnv.FQN(): "staging",
			}
		}
		record3 := rows3.Record(memory.DefaultAllocator, schema)
		builder.CollectRecord(record3)
		record3.Release()
		require.Equal(t, 17, builder.Len())
		require.Equal(t, 10, len(builder.rowBuilders), "buffer should grow to 10 rowBuilders")

		// Verify all rowBuilders are properly initialized
		for i := 0; i < len(builder.rowBuilders); i++ {
			require.NotNil(t, builder.rowBuilders[i].lbsBuilder, "lbsBuilder should be initialized")
			require.NotNil(t, builder.rowBuilders[i].metadataBuilder, "metadataBuilder should be initialized")
			require.NotNil(t, builder.rowBuilders[i].parsedBuilder, "parsedBuilder should be initialized")
			require.Equal(t, 0, len(builder.rowBuilders[i].parsedEmptyKeys), "parsedEmptyKeys should be empty")
		}
	})

	t.Run("empty records mixed with valid records", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, false),
			},
			nil,
		)

		builder := newStreamsResultBuilder(logproto.BACKWARD)

		// First record: 3 valid rows
		rows1 := make(arrowtest.Rows, 3)
		for i := 0; i < 3; i++ {
			rows1[i] = arrowtest.Row{
				colTs.FQN():  time.Unix(0, int64(1620000000000000001+i)).UTC(),
				colMsg.FQN(): "log line",
				colEnv.FQN(): "prod",
			}
		}
		record1 := rows1.Record(memory.DefaultAllocator, schema)
		builder.CollectRecord(record1)
		record1.Release()
		require.Equal(t, 3, builder.Len())

		// Second record: empty (0 rows)
		rows2 := arrowtest.Rows{}
		record2 := rows2.Record(memory.DefaultAllocator, schema)
		builder.CollectRecord(record2)
		record2.Release()
		require.Equal(t, 3, builder.Len(), "empty record should not change count")

		// Third record: 2 valid rows
		rows3 := make(arrowtest.Rows, 2)
		for i := 0; i < 2; i++ {
			rows3[i] = arrowtest.Row{
				colTs.FQN():  time.Unix(0, int64(1620000000000000010+i)).UTC(),
				colMsg.FQN(): "log line",
				colEnv.FQN(): "dev",
			}
		}
		record3 := rows3.Record(memory.DefaultAllocator, schema)
		builder.CollectRecord(record3)
		record3.Release()
		require.Equal(t, 5, builder.Len(), "should have 5 total entries")

		// Verify final result
		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		streams := result.Data.(logqlmodel.Streams)
		// Note: 2 unique streams (prod with 3 entries, dev with 2 entries) = 5 total entries
		require.Equal(t, 2, len(streams), "should have 2 unique streams")

		// Verify the streams have the correct number of entries
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 5, totalEntries, "should have 5 total entries across both streams")
	})

	t.Run("parsed empty values are added to the parsed labels", func(t *testing.T) {
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)
		colMetadata := semconv.NewIdentifier("metadata", types.ColumnTypeMetadata, types.Loki.String)
		colParsedA := semconv.NewIdentifier("Aparsed", types.ColumnTypeParsed, types.Loki.String)
		colParsedZ := semconv.NewIdentifier("Zparsed", types.ColumnTypeParsed, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, false),
				semconv.FieldFromIdent(colMetadata, false),
				semconv.FieldFromIdent(colParsedA, false),
				semconv.FieldFromIdent(colParsedZ, false),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colMsg.FQN(): "log line", colEnv.FQN(): "prod", colMetadata.FQN(): "md value", colParsedA.FQN(): "A", colParsedZ.FQN(): "Z"},
			{colTs.FQN(): time.Unix(0, 1620000000000000000).UTC(), colMsg.FQN(): "log line", colEnv.FQN(): "prod", colMetadata.FQN(): "", colParsedA.FQN(): "", colParsedZ.FQN(): ""},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		builder := newStreamsResultBuilder(logproto.BACKWARD)
		builder.CollectRecord(record)
		record.Release()
		require.Equal(t, 2, builder.Len())

		md, _ := metadata.NewContext(t.Context())
		result := builder.Build(stats.Result{}, md)
		streams := result.Data.(logqlmodel.Streams)
		require.Equal(t, 2, len(streams), "should have 2 unique streams")

		expected := logqlmodel.Streams{
			push.Stream{
				Labels: labels.FromStrings("Aparsed", "", "env", "prod", "Zparsed", "").String(),
				Entries: []logproto.Entry{
					{Line: "log line", Timestamp: time.Unix(0, 1620000000000000000), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.Labels{}), Parsed: logproto.FromLabelsToLabelAdapters(labels.FromStrings("Aparsed", "", "Zparsed", ""))},
				},
			},
			push.Stream{
				Labels: labels.FromStrings("Aparsed", "A", "env", "prod", "metadata", "md value", "Zparsed", "Z").String(),
				Entries: []logproto.Entry{
					{Line: "log line", Timestamp: time.Unix(0, 1620000000000000000), StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("metadata", "md value")), Parsed: logproto.FromLabelsToLabelAdapters(labels.FromStrings("Aparsed", "A", "Zparsed", "Z"))},
				},
			},
		}
		require.Equal(t, expected, streams)
	})
}

func TestVectorResultBuilder(t *testing.T) {
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

		record := rows.Record(memory.DefaultAllocator, schema)

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

		record := rows.Record(memory.DefaultAllocator, schema)

		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		builder := newVectorResultBuilder()
		err := collectResult(context.Background(), pipeline, builder)

		require.NoError(t, err)
		require.Equal(t, 0, builder.Len(), "expected no samples to be collected")
	})
}

func TestMatrixResultBuilder(t *testing.T) {
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

		record := rows.Record(memory.DefaultAllocator, schema)

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
