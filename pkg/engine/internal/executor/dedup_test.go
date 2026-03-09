package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	tsFQN  = semconv.ColumnIdentTimestamp.FQN()
	msgFQN = semconv.ColumnIdentMessage.FQN()
)

func dedupSchema(extraLabels ...string) *arrow.Schema {
	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
	}
	for _, lbl := range extraLabels {
		fields = append(fields, semconv.FieldFromIdent(
			semconv.NewIdentifier(lbl, types.ColumnTypeLabel, types.Loki.String), true,
		))
	}
	return arrow.NewSchema(fields, nil)
}

func ts(ns int64) time.Time { return time.Unix(0, ns).UTC() }

func lblFQN(name string) string {
	return semconv.NewIdentifier(name, types.ColumnTypeLabel, types.Loki.String).FQN()
}

func TestDedupPipeline_RemovesDuplicateRows(t *testing.T) {
	schema := dedupSchema()

	input := NewArrowtestPipeline(schema, arrowtest.Rows{
		{tsFQN: ts(1000), msgFQN: "hello"},
		{tsFQN: ts(2000), msgFQN: "world"},
		{tsFQN: ts(1000), msgFQN: "hello"}, // duplicate
		{tsFQN: ts(3000), msgFQN: "foo"},
	})

	pipeline := newDedupPipeline(input)
	ctx := context.Background()
	require.NoError(t, pipeline.Open(ctx))
	defer pipeline.Close()

	batch, err := pipeline.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), batch.NumRows())

	_, err = pipeline.Read(ctx)
	require.True(t, errors.Is(err, EOF))
}

func TestDedupPipeline_PreservesUniqueRows(t *testing.T) {
	schema := dedupSchema()

	input := NewArrowtestPipeline(schema, arrowtest.Rows{
		{tsFQN: ts(1000), msgFQN: "a"},
		{tsFQN: ts(2000), msgFQN: "b"},
		{tsFQN: ts(3000), msgFQN: "c"},
	})

	pipeline := newDedupPipeline(input)
	ctx := context.Background()
	require.NoError(t, pipeline.Open(ctx))
	defer pipeline.Close()

	batch, err := pipeline.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), batch.NumRows())
}

func TestDedupPipeline_DuplicatesAcrossBatches(t *testing.T) {
	schema := dedupSchema()

	input := NewArrowtestPipeline(schema,
		arrowtest.Rows{
			{tsFQN: ts(1000), msgFQN: "hello"},
			{tsFQN: ts(2000), msgFQN: "world"},
		},
		arrowtest.Rows{
			{tsFQN: ts(1000), msgFQN: "hello"}, // dup from batch 1
			{tsFQN: ts(3000), msgFQN: "new"},
		},
	)

	pipeline := newDedupPipeline(input)
	ctx := context.Background()
	require.NoError(t, pipeline.Open(ctx))
	defer pipeline.Close()

	var totalRows int64
	for {
		batch, err := pipeline.Read(ctx)
		if errors.Is(err, EOF) {
			break
		}
		require.NoError(t, err)
		totalRows += batch.NumRows()
	}
	require.Equal(t, int64(3), totalRows)
}

func TestDedupPipeline_SameTimestampDifferentMessage(t *testing.T) {
	schema := dedupSchema()

	input := NewArrowtestPipeline(schema, arrowtest.Rows{
		{tsFQN: ts(1000), msgFQN: "msg-a"},
		{tsFQN: ts(1000), msgFQN: "msg-b"},
	})

	pipeline := newDedupPipeline(input)
	ctx := context.Background()
	require.NoError(t, pipeline.Open(ctx))
	defer pipeline.Close()

	batch, err := pipeline.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), batch.NumRows(), "same ts but different message should both be kept")
}

func TestDedupPipeline_IncludesLabelsInKey(t *testing.T) {
	schema := dedupSchema("env")
	envFQN := lblFQN("env")

	input := NewArrowtestPipeline(schema, arrowtest.Rows{
		{tsFQN: ts(1000), msgFQN: "msg", envFQN: "prod"},
		{tsFQN: ts(1000), msgFQN: "msg", envFQN: "staging"},
		{tsFQN: ts(1000), msgFQN: "msg", envFQN: "prod"}, // dup
	})

	pipeline := newDedupPipeline(input)
	ctx := context.Background()
	require.NoError(t, pipeline.Open(ctx))
	defer pipeline.Close()

	batch, err := pipeline.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), batch.NumRows(), "same ts+msg but different labels should be kept")
}

func TestDedupPipeline_EmptyBatch(t *testing.T) {
	schema := dedupSchema()

	input := NewArrowtestPipeline(schema, arrowtest.Rows{})

	pipeline := newDedupPipeline(input)
	ctx := context.Background()
	require.NoError(t, pipeline.Open(ctx))
	defer pipeline.Close()

	batch, err := pipeline.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), batch.NumRows())
}

func BenchmarkDedupPipeline(b *testing.B) {
	schema := dedupSchema("env")
	envFQN := lblFQN("env")

	for _, tc := range []struct {
		name    string
		numRows int
		dupRate float64
	}{
		{"1k_no_dups", 1000, 0},
		{"1k_50pct_dups", 1000, 0.5},
		{"10k_10pct_dups", 10000, 0.1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			rows := make(arrowtest.Rows, tc.numRows)
			dupThreshold := int(tc.dupRate * 100)
			for i := range tc.numRows {
				isDup := i > 0 && (i%100) < dupThreshold
				t := int64(i)
				if isDup {
					t = int64(i - 1)
				}
				rows[i] = map[string]any{
					tsFQN:  ts(t),
					msgFQN: "msg",
					envFQN: "prod",
				}
			}

			b.ResetTimer()
			for range b.N {
				p := newDedupPipeline(NewArrowtestPipeline(schema, rows))
				ctx := context.Background()
				_ = p.Open(ctx)
				for {
					_, err := p.Read(ctx)
					if errors.Is(err, EOF) {
						break
					}
				}
				p.Close()
			}
		})
	}
}
