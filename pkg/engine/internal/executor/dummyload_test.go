package executor

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestNewDummyLoadPipeline_Schema(t *testing.T) {
	t.Run("default labels", func(t *testing.T) {
		node := &physical.DummyLoad{NumBatches: 1, BatchSize: 1}
		p := newDummyLoadPipeline(node)

		schema := p.schema
		// First two fields are timestamp and message.
		require.Equal(t, semconv.ColumnIdentTimestamp.FQN(), schema.Field(0).Name)
		require.Equal(t, semconv.ColumnIdentMessage.FQN(), schema.Field(1).Name)

		// Remaining fields are label columns.
		require.Equal(t, 2+len(physical.DefaultDummyLoadLabels), schema.NumFields())
		for i, lbl := range physical.DefaultDummyLoadLabels {
			expected := semconv.FQN(lbl, types.ColumnTypeLabel, types.Loki.String)
			require.Equal(t, expected, schema.Field(2+i).Name)
		}
	})

	t.Run("custom labels", func(t *testing.T) {
		labels := []string{"env", "region"}
		node := &physical.DummyLoad{NumBatches: 1, BatchSize: 1, Labels: labels}
		p := newDummyLoadPipeline(node)

		schema := p.schema
		require.Equal(t, 2+len(labels), schema.NumFields())
		for i, lbl := range labels {
			expected := semconv.FQN(lbl, types.ColumnTypeLabel, types.Loki.String)
			require.Equal(t, expected, schema.Field(2+i).Name)
		}
	})
}

func TestDummyLoadPipeline_ReadBeforeOpen(t *testing.T) {
	node := &physical.DummyLoad{NumBatches: 1, BatchSize: 1}
	p := newDummyLoadPipeline(node)
	defer p.Close()

	_, err := p.Read(t.Context())
	require.ErrorIs(t, err, errPipelineNotOpen)
}

func TestDummyLoadPipeline_EmitsBatches(t *testing.T) {
	const numBatches = 3
	const batchSize = 5

	node := &physical.DummyLoad{NumBatches: numBatches, BatchSize: batchSize}
	p := newDummyLoadPipeline(node)
	defer p.Close()

	require.NoError(t, p.Open(t.Context()))

	for i := 0; i < numBatches; i++ {
		rec, err := p.Read(t.Context())
		require.NoError(t, err)
		require.NotNil(t, rec)
		require.Equal(t, int64(batchSize), rec.NumRows())
		rec.Release()
	}

	// After all batches are exhausted, Read should return EOF.
	_, err := p.Read(t.Context())
	require.ErrorIs(t, err, EOF)
}

func TestDummyLoadPipeline_BatchSizeDefaultsToOne(t *testing.T) {
	for _, size := range []int{0, -1} {
		node := &physical.DummyLoad{NumBatches: 1, BatchSize: size}
		p := newDummyLoadPipeline(node)
		defer p.Close()

		require.NoError(t, p.Open(t.Context()))
		rec, err := p.Read(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(1), rec.NumRows())
		rec.Release()
	}
}

func TestDummyLoadPipeline_ZeroBatches(t *testing.T) {
	node := &physical.DummyLoad{NumBatches: 0, BatchSize: 10}
	p := newDummyLoadPipeline(node)
	defer p.Close()

	require.NoError(t, p.Open(t.Context()))
	_, err := p.Read(t.Context())
	require.ErrorIs(t, err, EOF)
}

func TestDummyLoadPipeline_BatchColumns(t *testing.T) {
	labels := []string{"cluster", "level"}
	node := &physical.DummyLoad{NumBatches: 1, BatchSize: 10, Labels: labels}
	p := newDummyLoadPipeline(node)
	defer p.Close()

	require.NoError(t, p.Open(t.Context()))
	rec, err := p.Read(t.Context())
	require.NoError(t, err)
	defer rec.Release()

	// timestamp column: all values should be non-zero and increasing.
	require.Equal(t, arrow.TIMESTAMP, rec.Column(0).DataType().ID())
	require.Equal(t, int64(10), rec.NumRows())

	// message column: all values should be non-empty strings.
	require.Equal(t, arrow.STRING, rec.Column(1).DataType().ID())

	// label columns.
	for i := range labels {
		require.Equal(t, arrow.STRING, rec.Column(2+i).DataType().ID())
	}
}

func TestDummyLoadPipeline_ContextCancelDuringSleep(t *testing.T) {
	node := &physical.DummyLoad{
		NumBatches:    10,
		BatchSize:     1,
		SleepPerBatch: 10 * time.Second,
	}
	p := newDummyLoadPipeline(node)
	defer p.Close()

	require.NoError(t, p.Open(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	_, err := p.Read(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDummyLabelValue(t *testing.T) {
	t.Run("known label with pool", func(t *testing.T) {
		for label, pool := range dummyLabelValues {
			if len(pool) == 0 {
				continue
			}
			val := dummyLabelValue(label)
			require.Contains(t, pool, val, "label %q: value %q not in pool", label, val)
		}
	})

	t.Run("pod is generated per-row", func(t *testing.T) {
		val := dummyLabelValue("pod")
		require.Regexp(t, `^pod-[0-9a-f]{4}$`, val)
	})

	t.Run("bytes_processed is a numeric string", func(t *testing.T) {
		val := dummyLabelValue("bytes_processed")
		require.Regexp(t, `^\d+$`, val)
	})

	t.Run("unknown label uses label-hex format", func(t *testing.T) {
		val := dummyLabelValue("myunknownlabel")
		require.Regexp(t, `^myunknownlabel-[0-9a-f]{4}$`, val)
	})
}
