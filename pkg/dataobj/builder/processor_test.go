package builder

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

var (
	// A builder configuration to be used in tests.
	testBuilderCfg = logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10,
		TargetObjectSize:        1 << 22, // 4 MiB
		TargetSectionSize:       1 << 22, // 4 MiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
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
		startTestFlushLoop(ctx, t, proc)

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
		synctest.Wait()
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
		startTestFlushLoop(ctx, t, proc)

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
		synctest.Wait()
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
		startTestFlushLoop(ctx, t, proc)

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
		synctest.Wait()
		require.Equal(t, 1, flushCommitter.flushes)
	})
}

// failureFlusher is a special flusher that always fails.
type failureFlusher struct{}

func (f *failureFlusher) Flush(_ context.Context, _ builder, _ string) (*dataobj.Object, io.Closer, string, error) {
	return nil, nil, "", errors.New("mock error")
}

type failureFlushCommitter struct{}

func (m *failureFlushCommitter) Flush(_ context.Context, _ []builder, _ string, _ int64) error {
	return errors.New("mock error")
}

// fixedErrFlushCommitter always returns the configured error. It is used to
// verify which flush failures stop the processor with an error.
// If waitForCancel is true, Flush returns only after ctx is canceled.
type fixedErrFlushCommitter struct {
	err           error
	waitForCancel bool
}

func (m *fixedErrFlushCommitter) Flush(ctx context.Context, _ []builder, _ string, _ int64) error {
	if m.waitForCancel {
		<-ctx.Done()
	}
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
			startTestFlushLoop(ctx, t, proc)

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
			synctest.Wait()
			require.Equal(t, 1, flushCommitter.flushes)

			// flush resets these fields when it sends the flush request.
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
			stop := startTestFlushLoop(ctx, t, proc)

			// Process a record containing some log lines. No flush should occur.
			rec := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec))
			require.Equal(t, time.Now(), proc.firstAppend)
			require.Equal(t, time.Now(), proc.lastAppend)
			require.Equal(t, rec.Offset, proc.lastOffset)

			// Advance time and force a flush. The flush runs in the flush loop
			// and fails. The flusher is not re-entrant, so the flush loop wraps
			// the failure in errUnrecoverableFlush. The partition processor then
			// restarts and reads the records again from Kafka.
			time.Sleep(time.Second)
			require.NoError(t, proc.flush(ctx, "forced"))
			err := stop()
			require.ErrorIs(t, err, errUnrecoverableFlush)
			require.ErrorContains(t, err, "mock error")

			// flush reset these fields when it sent the flush request, before
			// the flush failed.
			require.True(t, proc.firstAppend.IsZero())
			require.True(t, proc.lastAppend.IsZero())
		})
	})

	t.Run("stops the flush loop without an error when a flush fails after shutdown cancels the context", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx, cancel    = context.WithCancel(t.Context())
				reg            = prometheus.NewRegistry()
				m              = newTestMultiBuilder()
				flushCommitter = &fixedErrFlushCommitter{err: errors.New("mock error"), waitForCancel: true}
				proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant", time.Now())))

			require.NoError(t, proc.flush(ctx, "forced"))
			cancel()
			synctest.Wait()

			// The flush loop stopped and ctx is canceled, so a later flush
			// fails with either error.
			require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant", time.Now())))
			err := proc.flush(ctx, "forced")
			require.True(t, errors.Is(err, errFlushLoopStopped) || errors.Is(err, context.Canceled), "unexpected error: %v", err)
			require.NoError(t, stop())
		})
	})

	t.Run("escalates a canceled error while the context is live", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx            = t.Context()
				reg            = prometheus.NewRegistry()
				m              = newTestMultiBuilder()
				flushCommitter = &fixedErrFlushCommitter{err: context.Canceled}
				proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant", time.Now())))

			require.NoError(t, proc.flush(ctx, "forced"))
			err := stop()
			require.ErrorIs(t, err, errUnrecoverableFlush)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorContains(t, err, "forced flush up to offset 0")
		})
	})
}

func TestPartitionProcessor_FlushSplitsAcrossTenantsAndWindows(t *testing.T) {
	// Records whose entries span multiple tenants and TOC windows must be
	// flushed as one builder per tenant and window, while still committing a
	// single Kafka offset.
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx            = t.Context()
			reg            = prometheus.NewRegistry()
			m              = newTestMultiBuilder()
			flushCommitter = &mockFlushCommitter{}
			proc           = newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)
		startTestFlushLoop(ctx, t, proc)

		w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
		w2 := w1.Add(metastore.MetastoreWindowSize)
		w3 := w2.Add(metastore.MetastoreWindowSize)
		require.NoError(t, proc.processRecord(ctx, newTestMultiWindowRecord(t, "tenant-a", []time.Time{w1, w2, w3})))
		rec := newTestMultiWindowRecord(t, "tenant-b", []time.Time{w1, w2})
		rec.Offset = 1
		require.NoError(t, proc.processRecord(ctx, rec))

		// Three windows for tenant-a and two for tenant-b.
		require.Len(t, m.getBuilders(), 5)

		require.NoError(t, proc.flush(ctx, "forced"))
		synctest.Wait()
		require.Equal(t, 1, flushCommitter.flushes)
		require.Equal(t, 5, flushCommitter.lastBuilderCount)
		require.Equal(t, rec.Offset, flushCommitter.lastOffset)

		// flush resets the multi-builder when it sends the flush request.
		require.Empty(t, m.getBuilders())
		require.Equal(t, 0, m.GetEstimatedSize())
	})
}

func TestProcessor_ConsumeDuringFlush(t *testing.T) {
	newProc := func(records chan *kgo.Record) (*processor, *mockMultiBuilder, *gatedFlushCommitter) {
		m := newTestMultiBuilder()
		flushCommitter := newGatedFlushCommitter()
		proc := newProcessor(m, records, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), prometheus.NewRegistry())
		return proc, m, flushCommitter
	}
	// waitRun fails the test if Run does not return within a minute of fake
	// time. The idle flush timeout is longer, so an idle flush cannot hide a
	// Run that does not return.
	waitRun := func(t *testing.T, ran <-chan error) error {
		select {
		case err := <-ran:
			return err
		case <-time.After(time.Minute):
			t.Fatal("Run did not return")
			return nil
		}
	}
	newRecord := func(t *testing.T, offset int64) *kgo.Record {
		rec := newTestRecord(t, "tenant", time.Now())
		rec.Offset = offset
		return rec
	}

	t.Run("appends records while the previous flush runs", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			proc, m, flushCommitter := newProc(nil)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newRecord(t, 1)))
			m.forceFull = true
			require.NoError(t, proc.processRecord(ctx, newRecord(t, 2)))
			synctest.Wait()
			require.Equal(t, []int64{1}, flushCommitter.startedOffsets())

			m.forceFull = false
			require.NoError(t, proc.processRecord(ctx, newRecord(t, 3)))
			require.Equal(t, int64(3), proc.lastOffset)
			require.Len(t, m.getBuilders(), 1)
			require.Equal(t, time.Now(), proc.firstAppend)

			flushCommitter.results <- nil
			require.NoError(t, stop())
		})
	})

	t.Run("starts the next flush only after the previous flush commits", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			proc, m, flushCommitter := newProc(nil)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newRecord(t, 1)))
			m.forceFull = true
			require.NoError(t, proc.processRecord(ctx, newRecord(t, 2)))

			processed := make(chan error)
			go func() { processed <- proc.processRecord(ctx, newRecord(t, 3)) }()
			synctest.Wait()
			require.Equal(t, []int64{1}, flushCommitter.startedOffsets())

			flushCommitter.results <- nil
			require.NoError(t, <-processed)
			synctest.Wait()
			require.Equal(t, []int64{1, 2}, flushCommitter.startedOffsets())

			flushCommitter.results <- nil
			require.NoError(t, stop())
		})
	})

	t.Run("reports the time spent waiting for the previous flush and keeps the waiting builders in the size estimate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			proc, m, flushCommitter := newProc(nil)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newRecord(t, 1)))
			m.forceFull = true
			require.NoError(t, proc.processRecord(ctx, newRecord(t, 2)))
			waitingSize := float64(m.GetEstimatedSize())
			require.Equal(t, waitingSize, testutil.ToFloat64(proc.metrics.sizeEstimate))

			processed := make(chan error)
			go func() { processed <- proc.processRecord(ctx, newRecord(t, 3)) }()
			time.Sleep(10 * time.Second)
			require.Equal(t, waitingSize, testutil.ToFloat64(proc.metrics.sizeEstimate))

			flushCommitter.results <- nil
			require.NoError(t, <-processed)
			require.Equal(t, 10.0, testutil.ToFloat64(proc.metrics.flushWaitSeconds))

			flushCommitter.results <- nil
			require.NoError(t, stop())
		})
	})

	t.Run("rejects the next flush after a flush fails and returns the failure from stopFlushLoop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			proc, m, flushCommitter := newProc(nil)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newRecord(t, 1)))
			m.forceFull = true
			require.NoError(t, proc.processRecord(ctx, newRecord(t, 2)))
			flushCommitter.results <- errors.New("mock error")
			synctest.Wait()

			err := proc.processRecord(ctx, newRecord(t, 3))
			require.ErrorIs(t, err, errFlush)
			require.ErrorIs(t, err, errFlushLoopStopped)
			require.Equal(t, []int64{1}, flushCommitter.startedOffsets())

			err = stop()
			require.ErrorIs(t, err, errUnrecoverableFlush)
			require.ErrorContains(t, err, "mock error")
		})
	})

	t.Run("stops Run when a background flush fails", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			records := make(chan *kgo.Record)
			proc, m, flushCommitter := newProc(records)

			ran := make(chan error)
			go func() { ran <- proc.Run(t.Context()) }()
			records <- newRecord(t, 1)
			synctest.Wait()
			m.forceFull = true
			records <- newRecord(t, 2)
			synctest.Wait()

			flushCommitter.results <- errors.New("mock error")
			err := waitRun(t, ran)
			require.ErrorIs(t, err, errUnrecoverableFlush)
			require.ErrorContains(t, err, "mock error")
		})
	})

	t.Run("waits for the background flush before Run returns", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			records := make(chan *kgo.Record)
			proc, m, flushCommitter := newProc(records)

			ran := make(chan error, 1)
			go func() { ran <- proc.Run(t.Context()) }()
			records <- newRecord(t, 1)
			synctest.Wait()
			m.forceFull = true
			records <- newRecord(t, 2)
			synctest.Wait()

			close(records)
			synctest.Wait()
			require.Empty(t, ran)

			flushCommitter.results <- nil
			require.NoError(t, waitRun(t, ran))
		})
	})

	t.Run("keeps the flushing builders apart from the builders that receive new records", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			proc, m, flushCommitter := newProc(nil)
			stop := startTestFlushLoop(ctx, t, proc)

			require.NoError(t, proc.processRecord(ctx, newRecord(t, 1)))
			m.forceFull = true
			require.NoError(t, proc.processRecord(ctx, newRecord(t, 2)))
			m.forceFull = false
			synctest.Wait()

			flushing := flushCommitter.startedBuilders()[0]
			require.Len(t, flushing, 1)
			flushingSize := flushing[0].GetEstimatedSize()

			// The records have the tenant and window of the flushing builder.
			// They must go to a new builder all the same.
			for offset := int64(3); offset < 10; offset++ {
				require.NoError(t, proc.processRecord(ctx, newRecord(t, offset)))
			}
			require.Equal(t, flushingSize, flushing[0].GetEstimatedSize())
			require.Len(t, m.getBuilders(), 1)
			require.NotSame(t, flushing[0], m.getBuilders()[0])

			flushCommitter.results <- nil
			require.NoError(t, stop())
		})
	})

	t.Run("stops reading records while the next flush waits for the previous one", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			records := make(chan *kgo.Record)
			proc, m, flushCommitter := newProc(records)

			ran := make(chan error, 1)
			go func() { ran <- proc.Run(t.Context()) }()
			records <- newRecord(t, 1)
			synctest.Wait()
			m.forceFull = true
			records <- newRecord(t, 2)
			records <- newRecord(t, 3)
			synctest.Wait()

			select {
			case records <- newRecord(t, 4):
				t.Fatal("Run read a record while the next flush waited for the previous one")
			default:
			}
			require.Equal(t, []int64{1}, flushCommitter.startedOffsets())

			m.forceFull = false
			flushCommitter.results <- nil
			records <- newRecord(t, 4)
			synctest.Wait()
			require.Equal(t, []int64{1, 2}, flushCommitter.startedOffsets())
			require.Equal(t, 1, flushCommitter.maxConcurrentFlushes())

			flushCommitter.results <- nil
			close(records)
			require.NoError(t, waitRun(t, ran))
		})
	})

	t.Run("starts an idle flush only after the previous flush commits", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			records := make(chan *kgo.Record)
			proc, m, flushCommitter := newProc(records)

			ran := make(chan error, 1)
			go func() { ran <- proc.Run(t.Context()) }()
			records <- newRecord(t, 1)
			synctest.Wait()
			m.forceFull = true
			records <- newRecord(t, 2)
			synctest.Wait()
			m.forceFull = false

			// The idle flush timeout is 5 minutes. The first timer fires at
			// exactly 5 minutes, which is not past the timeout, so the idle
			// flush starts at the second timer.
			time.Sleep(11 * time.Minute)
			synctest.Wait()
			require.Equal(t, []int64{1}, flushCommitter.startedOffsets())

			flushCommitter.results <- nil
			synctest.Wait()
			require.Equal(t, []int64{1, 2}, flushCommitter.startedOffsets())
			require.Equal(t, []string{flushReasonBuilderFull, flushReasonIdle}, flushCommitter.startedReasons())

			flushCommitter.results <- nil
			close(records)
			require.NoError(t, waitRun(t, ran))
		})
	})

	t.Run("starts no later flush and counts no failed record when Run stops while a flush waits for the previous one", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			records := make(chan *kgo.Record)
			proc, m, flushCommitter := newProc(records)

			ran := make(chan error, 1)
			go func() { ran <- proc.Run(ctx) }()
			records <- newRecord(t, 1)
			synctest.Wait()
			m.forceFull = true
			records <- newRecord(t, 2)
			records <- newRecord(t, 3)
			synctest.Wait()

			cancel()
			require.NoError(t, waitRun(t, ran))
			require.Equal(t, []int64{1}, flushCommitter.startedOffsets())
			require.Zero(t, testutil.ToFloat64(proc.metrics.recordFailures))
		})
	})

	t.Run("builds the same data objects and indexes as a flush that blocks consumption", func(t *testing.T) {
		records := newTestFlushRecords(t, 3, 300)

		serialObjects, serialOffsets := flushWithRealStack(t, records, true)
		objects, offsets := flushWithRealStack(t, records, false)

		require.NotEmpty(t, serialObjects)
		require.Equal(t, serialOffsets, offsets)
		require.Equal(t, slices.Sorted(maps.Keys(serialObjects)), slices.Sorted(maps.Keys(objects)))
		for name, data := range serialObjects {
			require.Equal(t, data, objects[name], "object %s", name)
		}
	})
}

// newTestFlushRecords returns the records of n flushes, with size records
// for each flush. The offsets increase across flushes. The records belong to
// three tenants and span two ToC windows. The entries of each stream are out
// of order, so the sort has work to do.
func newTestFlushRecords(t *testing.T, n, size int) [][]*kgo.Record {
	base := time.Date(2026, time.October, 1, 6, 0, 0, 0, time.UTC)
	var (
		flushes [][]*kgo.Record
		offset  int64
	)
	for f := range n {
		var records []*kgo.Record
		for i := range size {
			ts := base.Add(time.Duration(f*size+(i*37)%size) * time.Minute)
			stream := logproto.Stream{
				Labels:  fmt.Sprintf(`{app="app-%d", pod="pod-%d"}`, i%4, i%7),
				Entries: []push.Entry{{Timestamp: ts, Line: fmt.Sprintf("level=info flush=%d line=%d", f, i)}},
			}
			value, err := stream.Marshal()
			require.NoError(t, err)
			records = append(records, &kgo.Record{
				Key:       []byte(fmt.Sprintf("tenant-%d", i%3)),
				Value:     value,
				Timestamp: ts,
				Offset:    offset,
			})
			offset++
		}
		flushes = append(flushes, records)
	}
	return flushes
}

// flushWithRealStack appends the records of each flush to a processor, then
// flushes them. The flush uses the production sort, upload, index and ToC
// writer with an in-memory bucket. It returns the contents of the bucket and
// the committed offsets.
//
// If serial is true, it waits for each flush to end before it appends the
// records of the next one, so no append overlaps a flush.
func flushWithRealStack(t *testing.T, flushes [][]*kgo.Record, serial bool) (map[string][]byte, []int64) {
	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
	cfg.LogsobjBuilder.DropDuplicates = true

	var (
		ctx          = t.Context()
		logger       = log.NewNopLogger()
		reg          = prometheus.NewRegistry()
		scratchStore = scratch.NewMemory()
		bucket       = objstore.NewInMemBucket()
		idxBucket    = objstore.NewPrefixedBucket(bucket, "index/")
		committer    = &mockCommitter{}
	)
	factory, err := logsobj.NewBuilderFactory(cfg.LogsobjBuilder, scratchStore, logsobj.NewBuilderMetrics(), logger, nil)
	require.NoError(t, err)
	indexobjMetrics := indexobj.NewBuilderMetrics(reg)
	indexer, err := index.NewSimpleIndexer(cfg.IndexobjBuilder, scratchStore, logger, idxBucket, index.NewIndexerMetrics(reg), indexobjMetrics, index.NewCalculatorMetrics(reg))
	require.NoError(t, err)
	var (
		uploader       = dataobj_uploader.New(dataobj_uploader.Config{SHAPrefixSize: 2}, bucket, logger)
		flusher        = newFlusher(logsobj.NewSorter(factory, reg), uploader, logger, reg)
		tocWriter      = metastore.NewTableOfContentsWriter(idxBucket, metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, logger, metastore.NewTocWriterMetrics(reg), indexobjMetrics)
		flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, logger, reg)
		proc           = newProcessor(NewMultiObjectBuilder(factory, int(cfg.LogsobjBuilder.TargetObjectSize)), nil, flushCommitter, time.Hour, 0, logger, reg)
	)

	proc.startFlushLoop(ctx)
	for _, records := range flushes {
		for _, rec := range records {
			require.NoError(t, proc.processRecord(ctx, rec))
		}
		require.NoError(t, proc.flush(ctx, "test"))
		if serial {
			require.NoError(t, proc.stopFlushLoop())
			proc.startFlushLoop(ctx)
		}
	}
	require.NoError(t, proc.stopFlushLoop())
	return maps.Clone(bucket.Objects()), committer.offsets
}

// startTestFlushLoop starts the flush loop of proc, for a test that does not
// call Run. The returned function stops the loop and returns its error. A
// second call returns the same error. The loop also stops when the test ends.
func startTestFlushLoop(ctx context.Context, t *testing.T, proc *processor) (stop func() error) {
	proc.startFlushLoop(ctx)
	stop = sync.OnceValue(proc.stopFlushLoop)
	t.Cleanup(func() { _ = stop() })
	return stop
}

// newTestBuilder returns a new logsobj.Builder with registered metrics.
func newTestBuilder(t *testing.T, reg prometheus.Registerer) *logsobj.Builder {
	m := logsobj.NewBuilderMetrics()
	require.NoError(t, m.Register(reg))
	b, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), m, log.NewNopLogger(), nil)
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
