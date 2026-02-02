package consumer

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
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

func TestPartitionProcessor_BuilderMaxAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx             = t.Context()
			reg             = prometheus.NewRegistry()
			flusher         = &mockFlusher{}
			metastoreEvents = newMetastoreEvents(1, 10, &mockKafka{})
			committer       = &mockCommitter{}
			proc            = newProcessor(newTestBuilder(t, reg), nil, flusher, metastoreEvents, committer, 1, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)

		// Since no records have been pushed, the first append time should be zero,
		// and no flush should have occurred.
		require.True(t, proc.firstAppend.IsZero())
		require.Equal(t, 0, flusher.flushes)

		// Process a record containing some log lines. No flush should occur because
		// the builder has not reached the maximum age.
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))

		// The first append time should be set to the current time, but no flush
		// should have occurred.
		require.Equal(t, time.Now(), proc.firstAppend)
		require.Equal(t, time.Now(), proc.lastAppend)
		require.Equal(t, 0, flusher.flushes)

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
		require.Equal(t, 1, flusher.flushes)

		// Advance time one last time and push some more logs. No flush should
		// occur because the next builder has not reached the maximum age.
		expectedLastFlushed := time.Now()
		time.Sleep(time.Minute)
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))
		require.Equal(t, expectedLastFlushed, proc.firstAppend)
		require.Equal(t, time.Now(), proc.lastAppend)
		require.Equal(t, 1, flusher.flushes)
	})
}

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			ctx             = t.Context()
			reg             = prometheus.NewRegistry()
			flusher         = &mockFlusher{}
			metastoreEvents = newMetastoreEvents(1, 10, &mockKafka{})
			committer       = &mockCommitter{}
			proc            = newProcessor(newTestBuilder(t, reg), nil, flusher, metastoreEvents, committer, 1, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
		)

		// The idle flush timeout has not been exceeded, no flush should occur.
		flushed, err := proc.idleFlush(ctx)
		require.NoError(t, err)
		require.False(t, flushed)
		require.Equal(t, 0, flusher.flushes)

		// Advance time past the idle flush time. No flush should occur because
		// the builder is empty.
		time.Sleep(6 * time.Minute)
		flushed, err = proc.idleFlush(ctx)
		require.NoError(t, err)
		require.False(t, flushed)
		require.Equal(t, 0, flusher.flushes)

		// Process a record containing some log lines. No flush should occur because
		// when log lines are appended to the builder it resets the idle timeout.
		require.NoError(t, proc.processRecord(ctx, newTestRecord(t, "tenant1", time.Now())))
		require.False(t, proc.lastAppend.IsZero())
		flushed, err = proc.idleFlush(ctx)
		require.NoError(t, err)
		require.False(t, flushed)
		require.Equal(t, 0, flusher.flushes)

		// Advance time past the idle timeout. A flush should occur.
		time.Sleep(6 * time.Minute)
		flushed, err = proc.idleFlush(ctx)
		require.NoError(t, err)
		require.True(t, flushed)
		require.Equal(t, 1, flusher.flushes)
	})
}

// failureFlusher is a special flusher that always fails.
type failureFlusher struct{}

func (f *failureFlusher) Flush(_ context.Context, _ builder, _ string) (string, error) {
	return "", errors.New("failed to flush")
}

func TestPartitionProcessor_Flush(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx             = t.Context()
				reg             = prometheus.NewRegistry()
				flusher         = &mockFlusher{}
				metastoreKafka  = &mockKafka{}
				metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
				committer       = &mockCommitter{}
				proc            = newProcessor(newTestBuilder(t, reg), nil, flusher, metastoreEvents, committer, 1, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)

			// No flush should have occurred.
			require.Equal(t, 0, flusher.flushes)

			// Process a record containing some log lines. No flush should occur.
			rec1 := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec1))
			require.Equal(t, time.Now(), proc.firstAppend)
			require.Equal(t, time.Now(), proc.lastAppend)
			require.Equal(t, rec1.Offset, proc.lastOffset)

			// Advance time and force a flush.
			time.Sleep(time.Second)
			require.NoError(t, proc.flush(ctx, "test"))
			require.Equal(t, 1, flusher.flushes)

			// A metastore event should have been emitted and offsets committed.
			require.Len(t, metastoreKafka.produced, 1)
			require.Len(t, committer.offsets, 1)

			// The following fields should be reset at the end of every flush.
			require.True(t, proc.firstAppend.IsZero())
			require.True(t, proc.earliestRecordTime.IsZero())
			require.True(t, proc.lastAppend.IsZero())
		})
	})

	t.Run("should fail", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				ctx             = t.Context()
				reg             = prometheus.NewRegistry()
				flusher         = &failureFlusher{}
				metastoreKafka  = &mockKafka{}
				metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
				committer       = &mockCommitter{}
				proc            = newProcessor(newTestBuilder(t, reg), nil, flusher, metastoreEvents, committer, 1, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), reg)
			)

			// Process a record containing some log lines. No flush should occur.
			rec := newTestRecord(t, "tenant", time.Now())
			require.NoError(t, proc.processRecord(ctx, rec))
			require.Equal(t, time.Now(), proc.firstAppend)
			require.Equal(t, time.Now(), proc.lastAppend)
			require.Equal(t, rec.Offset, proc.lastOffset)

			// Advance time and force a flush. This flush should fail.
			time.Sleep(time.Second)
			require.EqualError(t, proc.flush(ctx, "forced"), "failed to flush data object: failed to flush")

			// No metastore event should be emitted and no offsets committed.
			require.Len(t, metastoreKafka.produced, 0)
			require.Len(t, committer.offsets, 0)

			// Despite the failure, the following fields should still be reset.
			require.True(t, proc.firstAppend.IsZero())
			require.True(t, proc.earliestRecordTime.IsZero())
			require.True(t, proc.lastAppend.IsZero())
		})
	})
}

// newTestBuilder returns a new logsobj.Builder with registered metrics.
func newTestBuilder(t *testing.T, reg prometheus.Registerer) *logsobj.Builder {
	b, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory())
	require.NoError(t, err)
	require.NoError(t, b.RegisterMetrics(reg))
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
