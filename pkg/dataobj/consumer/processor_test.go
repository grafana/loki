package consumer

import (
	"context"
	"errors"
	"fmt"
	"math"
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
			builder        = newTestBuilder(t, reg)
			group          = newMockBuilderGroup(builder)
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
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

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			builder        = newTestBuilder(t, reg)
			group          = newMockBuilderGroup(builder)
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
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

// canceledFlushCommitter mimics what the real flushCommitter returns on
// graceful shutdown: a wrapped context.Canceled. The processor is expected to
// return this error rather than panic.
type canceledFlushCommitter struct{}

func (m *canceledFlushCommitter) Flush(_ context.Context, _ []builder, _ string, _ int64) error {
	return fmt.Errorf("failed to flush data object: %w", context.Canceled)
}

// transientErrFlushCommitter returns a non-context.Canceled error. It is used
// to simulate the case where a transient error (e.g., a Kafka network blip)
// surfaces from the flushCommitter's retry loop on its last attempt while the
// context was canceled during the wait, so the returned error does not wrap
// context.Canceled.
type transientErrFlushCommitter struct{}

func (m *transientErrFlushCommitter) Flush(_ context.Context, _ []builder, _ string, _ int64) error {
	return errors.New("transient kafka error")
}

func TestPartitionProcessor_Flush(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				builder        = newTestBuilder(t, reg)
				group          = newMockBuilderGroup(builder)
				flushCommitter = &mockFlushCommitter{}
				proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
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

	t.Run("should panic on non-cancel flushCommitter error", func(t *testing.T) {
		// logsobj.Builder.Flush is not re-entrant: once it consumes the
		// buffered state, a retry cannot reproduce the object. Any
		// flushCommitter error that isn't a graceful shutdown therefore
		// escalates to a panic so the process restarts and Kafka replays
		// from the last committed offset.
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				builder        = newTestBuilder(t, reg)
				group          = newMockBuilderGroup(builder)
				flushCommitter = &failureFlushCommitter{}
				proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
			)

			rec := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec))

			time.Sleep(time.Second)
			require.PanicsWithError(t, "mock error", func() {
				_ = proc.flush(ctx, "forced")
			})
		})
	})

	t.Run("should return on context canceled without panic", func(t *testing.T) {
		// On graceful shutdown the flushCommitter surfaces a wrapped
		// context.Canceled; the processor returns the error so the caller
		// can exit cleanly and leave the offset uncommitted for Kafka
		// replay on restart.
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				builder        = newTestBuilder(t, reg)
				group          = newMockBuilderGroup(builder)
				flushCommitter = &canceledFlushCommitter{}
				proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
			)

			rec := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec))

			time.Sleep(time.Second)
			require.NotPanics(t, func() {
				err := proc.flush(ctx, "forced")
				require.ErrorIs(t, err, context.Canceled)
			})
		})
	})

	t.Run("should not panic when ctx is canceled but flushCommitter error does not wrap context.Canceled", func(t *testing.T) {
		// The emitEvent/commit backoff loops in the real flushCommitter
		// return the last retry attempt's error when the context is
		// canceled. If the last attempt hit a transient error (e.g., a
		// Kafka network blip) while the context was canceled during
		// b.Wait(), the returned error does not wrap context.Canceled.
		// The processor must still detect the shutdown via ctx.Err() and
		// return cleanly rather than panic.
		synctest.Test(t, func(t *testing.T) {
			var (
				rootCtx        = t.Context()
				reg            = prometheus.NewRegistry()
				builder        = newTestBuilder(t, reg)
				group          = newMockBuilderGroup(builder)
				flushCommitter = &transientErrFlushCommitter{}
				proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
			)

			rec := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(rootCtx, rec))

			// Cancel the context before the flush to simulate a graceful
			// shutdown that races with a transient error on the last
			// retry attempt.
			ctx, cancel := context.WithCancel(rootCtx)
			cancel()

			time.Sleep(time.Second)
			require.NotPanics(t, func() {
				err := proc.flush(ctx, "forced")
				require.ErrorIs(t, err, context.Canceled)
			})
		})
	})
}

func TestPartitionProcessor_FlushSplitsAcrossWindows(t *testing.T) {
	// A record that carries entries from two distinct 12h TOC windows must
	// result in two per-window builders being flushed in a single
	// flushCommitter call.
	synctest.Test(t, func(t *testing.T) {
		factory, err := logsobj.NewBuilderFactory(testBuilderCfg, scratch.NewMemory(), logsobj.NewBuilderMetrics())
		require.NoError(t, err)

		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			group          = NewTOCAlignedBuilderGroup(factory, math.MaxInt)
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(group, nil, flushCommitter, 5*time.Minute, 30*time.Minute, IngestModeKafka, log.NewNopLogger(), reg)
		)

		w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
		w2 := w1.Add(metastore.TOCWindowSize)
		rec := newTestRecordWithEntries(t, "tenant", w1, []push.Entry{
			{Timestamp: w1.Add(time.Minute), Line: "window-1"},
			{Timestamp: w2.Add(time.Minute), Line: "window-2"},
		})
		require.NoError(t, proc.processRecord(ctx, rec))
		require.Len(t, group.GetBuilders(), 2, "append must create one builder per TOC window")

		require.NoError(t, proc.flush(ctx, "test"))
		require.Equal(t, 1, flushCommitter.flushes, "flushCommitter is invoked once per flush()")
		require.Equal(t, 2, flushCommitter.lastBuilderCount, "flushCommitter must receive one builder per window")
	})
}

// newTestBuilder returns a new logsobj.Builder with registered metrics.
func newTestBuilder(t *testing.T, reg prometheus.Registerer) *logsobj.Builder {
	metrics := logsobj.NewBuilderMetrics()
	require.NoError(t, metrics.Register(reg))
	b, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), metrics)
	require.NoError(t, err)
	return b
}

// newTestRecord returns a new record containing the stream.
func newTestRecord(t *testing.T, tenant string, now time.Time) *kgo.Record {
	return newTestRecordWithEntries(t, tenant, now, []push.Entry{{
		Timestamp: now,
		Line:      "baz",
	}})
}

// newTestRecordWithEntries returns a new record whose stream has the given
// entries. The record's Kafka timestamp is set to now.
func newTestRecordWithEntries(t *testing.T, tenant string, now time.Time, entries []push.Entry) *kgo.Record {
	rec := kgo.Record{
		Key:       []byte(tenant),
		Timestamp: now,
	}
	stream := logproto.Stream{
		Labels:  `{foo="bar"}`,
		Entries: entries,
	}
	var err error
	rec.Value, err = stream.Marshal()
	require.NoError(t, err)
	return &rec
}
