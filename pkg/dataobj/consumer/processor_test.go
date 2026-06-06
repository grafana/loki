package consumer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

var (
	// A builder configuration to be used in tests.
	testBuilderCfg = logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2048,
			MaxPageRows:             10,
			TargetObjectSize:        1 << 22, // 4 MiB
			TargetSectionSize:       1 << 22, // 4 MiB
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		},
	}
)

func TestProcessor_BuilderMaxAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			m              = newTestMultiBuilder()
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)

		// Since no records have been pushed, the first append time should be zero,
		// and no flush should have occurred.
		require.True(t, proc.firstAppend.IsZero())
		require.Equal(t, 0, flushCommitter.flushes)

		// Process a record containing some log lines. No flush should occur because
		// the builder has not reached the maximum age.
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))

		// The first append time should be set to the current time, but no flush
		// should have occurred.
		require.Equal(t, time.Now(), proc.firstAppend)
		require.Equal(t, time.Now(), proc.lastAppend)
		require.Equal(t, 0, flushCommitter.flushes)

		// Advance time past the maximum age. A flush should occur, and the
		// the log lines from the record should be appended to the next data
		// object, not the one that was just flushed.
		time.Sleep(31 * time.Minute)
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))

		// The last flushed time should be updated to the current time, and so should
		// the first append time to reflect the start of the new data object.
		require.Equal(t, time.Now(), proc.firstAppend)
		require.Equal(t, time.Now(), proc.lastAppend)
		require.NotEqual(t, proc.builder.GetEstimatedSize(), 0)
		require.Equal(t, 1, flushCommitter.flushes)

		// Advance time one last time and push some more logs. No flush should
		// occur because the next builder has not reached the maximum age.
		expectedLastFlushed := time.Now()
		time.Sleep(time.Minute)
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))
		require.Equal(t, expectedLastFlushed, proc.firstAppend)
		require.Equal(t, time.Now(), proc.lastAppend)
		require.Equal(t, 1, flushCommitter.flushes)
	})
}

func TestProcessor_FlushesWhenBuilderFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			m              = newTestMultiBuilder()
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)

		// While the builder is not full, processing a record should append
		// without flushing.
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant", time.Now())))
		require.Equal(t, 0, flushCommitter.flushes)
		require.Equal(t, time.Now(), proc.firstAppend)

		// Mark the builder as full. The next record should trigger a flush
		// before it is appended, starting a new data object.
		m.forceFull = true
		flushedAt := time.Now()
		time.Sleep(time.Minute)
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant", time.Now())))

		// Exactly one flush should have occurred, and the record should still
		// have been appended afterwards, resetting firstAppend to the current
		// time rather than the time of the first append.
		require.Equal(t, 1, flushCommitter.flushes)
		require.NotEqual(t, flushedAt, proc.firstAppend)
		require.Equal(t, time.Now(), proc.firstAppend)
		require.Equal(t, time.Now(), proc.lastAppend)
	})
}

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			m              = newTestMultiBuilder()
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)

		// The idle flush timeout has not been exceeded, no flush should occur.
		flushed, err := proc.idleFlush(ctx)
		require.NoError(t, err)
		require.False(t, flushed)
		require.Equal(t, 0, flushCommitter.flushes)

		// Advance time past the idle flush time. No flush should occur because
		// the builder is empty.
		time.Sleep(6 * time.Minute)
		flushed, err = proc.idleFlush(ctx)
		require.NoError(t, err)
		require.False(t, flushed)
		require.Equal(t, 0, flushCommitter.flushes)

		// Process a record containing some log lines. No flush should occur because
		// when log lines are appended to the builder it resets the idle timeout.
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))
		require.False(t, proc.lastAppend.IsZero())
		flushed, err = proc.idleFlush(ctx)
		require.NoError(t, err)
		require.False(t, flushed)
		require.Equal(t, 0, flushCommitter.flushes)

		// Advance time past the idle timeout. A flush should occur.
		time.Sleep(6 * time.Minute)
		flushed, err = proc.idleFlush(ctx)
		require.NoError(t, err)
		require.True(t, flushed)
		require.Equal(t, 1, flushCommitter.flushes)
	})
}

// failureFlusher is a special flusher that always fails.
type failureFlusher struct{}

func (f *failureFlusher) Flush(_ context.Context, _ builder, _ string) (string, error) {
	return "", errors.New("mock error")
}

type failureFlushCommitter struct{}

func (m *failureFlushCommitter) Flush(_ context.Context, _ []builder, _ string, _ int64) error {
	return errors.New("mock error")
}

// fixedErrFlushCommitter always returns the configured error. It is used to
// verify how the processor classifies flush failures (recoverable vs. not).
type fixedErrFlushCommitter struct {
	err error
}

func (m *fixedErrFlushCommitter) Flush(_ context.Context, _ []builder, _ string, _ int64) error {
	return m.err
}

func TestPartitionProcessor_Flush(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				m              = newTestMultiBuilder()
				flushCommitter = &mockFlushCommitter{}
				proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)

			// No flush should have occurred.
			require.Equal(t, 0, flushCommitter.flushes)

			// Process a record containing some log lines. No flush should occur.
			rec1 := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec1))
			require.Equal(t, time.Now(), proc.firstAppend)
			require.Equal(t, time.Now(), proc.lastAppend)
			require.Equal(t, rec1.Offset, proc.lastOffset)

			// Advance time and force a flush.
			time.Sleep(time.Second)
			require.NoError(t, proc.flush(ctx, "test"))
			require.Equal(t, 1, flushCommitter.flushes)

			// The following fields should be reset at the end of every flush.
			require.True(t, proc.firstAppend.IsZero())
			require.True(t, proc.lastAppend.IsZero())
		})
	})

	t.Run("should fail", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				m              = newTestMultiBuilder()
				flushCommitter = &failureFlushCommitter{}
				proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)

			// Process a record containing some log lines. No flush should occur.
			rec := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec))
			require.Equal(t, time.Now(), proc.firstAppend)
			require.Equal(t, time.Now(), proc.lastAppend)
			require.Equal(t, rec.Offset, proc.lastOffset)

			// Advance time and force a flush. This flush should fail. Because the
			// flusher is not re-entrant, a non-cancellation failure is escalated
			// to an unrecoverable error so the partition processor restarts and
			// replays from Kafka.
			time.Sleep(time.Second)
			err := proc.flush(ctx, "forced")
			require.ErrorIs(t, err, errUnrecoverableFlush)
			require.ErrorContains(t, err, "mock error")

			// Despite the failure, the following fields should still be reset.
			require.True(t, proc.firstAppend.IsZero())
			require.True(t, proc.lastAppend.IsZero())
		})
	})

	t.Run("should not escalate a canceled flush", func(t *testing.T) {
		// During shutdown the flush context is canceled. A canceled flush must
		// be surfaced as-is rather than escalated to errUnrecoverableFlush, so
		// the processor shuts down gracefully instead of restarting.
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				m              = newTestMultiBuilder()
				flushCommitter = &fixedErrFlushCommitter{err: context.Canceled}
				proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)

			require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant", time.Now())))

			time.Sleep(time.Second)
			err := proc.flush(ctx, "forced")
			require.ErrorIs(t, err, context.Canceled)
			require.NotErrorIs(t, err, errUnrecoverableFlush)
		})
	})
}

func TestPartitionProcessor_FlushSplitsAcrossWindows(t *testing.T) {
	// A single record whose entries span multiple TOC windows must be flushed
	// as one builder per window, while still committing a single Kafka offset.
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			m              = newTestMultiBuilder()
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)

		w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
		w2 := w1.Add(metastore.MetastoreWindowSize)
		w3 := w2.Add(metastore.MetastoreWindowSize)
		rec := newTestMultiWindowRecord(t, "tenant", []time.Time{w1, w2, w3})
		require.NoError(t, proc.processRecord(ctx, rec))

		// Three windows means three per-window builders.
		require.Len(t, m.GetBuilders(), 3)

		require.NoError(t, proc.flush(ctx, "forced"))
		require.Equal(t, 1, flushCommitter.flushes)
		require.Equal(t, 3, flushCommitter.lastBuilderCount)
		require.Equal(t, rec.Offset, flushCommitter.lastOffset)

		// The multi-builder is reset after a successful flush.
		require.Empty(t, m.GetBuilders())
		require.Equal(t, 0, m.GetEstimatedSize())
	})
}

// newTestBuilder returns a new logsobj.Builder with registered metrics.
func newTestBuilder(t *testing.T, reg prometheus.Registerer) *logsobj.Builder {
	m := logsobj.NewBuilderMetrics()
	require.NoError(t, m.Register(reg))
	b, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), m)
	require.NoError(t, err)
	return b
}

// newTestRecord returns a new record containing the stream.
func newTestRecord(t *testing.T, tenant string, now time.Time) *kgo.Record {
	rec := kgo.Record{
		Key:       []byte(tenant),
		Timestamp: now,
	}
	stream := logproto.Stream{
		Labels: `{foo="bar"}`,
		Entries: []push.Entry{{
			Timestamp: now,
			Line:      "baz",
		}},
	}
	var err error
	rec.Value, err = stream.Marshal()
	require.NoError(t, err)
	return &rec
}

// newTestMultiWindowRecord returns a record whose stream contains one entry
// per provided timestamp. It is used to exercise records that span multiple
// TOC windows. The record timestamp is the first window.
func newTestMultiWindowRecord(t *testing.T, tenant string, windows []time.Time) *kgo.Record {
	require.NotEmpty(t, windows)
	rec := kgo.Record{
		Key:       []byte(tenant),
		Timestamp: windows[0],
	}
	stream := logproto.Stream{Labels: `{foo="bar"}`}
	for i, w := range windows {
		stream.Entries = append(stream.Entries, push.Entry{
			Timestamp: w.Add(time.Minute),
			Line:      fmt.Sprintf("line-%d", i),
		})
	}
	var err error
	rec.Value, err = stream.Marshal()
	require.NoError(t, err)
	return &rec
}
