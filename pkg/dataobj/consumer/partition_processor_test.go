package consumer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	var (
		ctx     = t.Context()
		clock   = quartz.NewMock(t)
		reg     = prometheus.NewRegistry()
		flusher = &mockFlusher{}
		proc    *partitionProcessor
	)
	proc = newPartitionProcessor(testBuilderCfg, scratch.NewMemory(), 5*time.Minute, 30*time.Minute, "topic", 1, nil, flusher, log.NewNopLogger(), reg)
	proc.clock = clock

	// Since no records have been pushed, the first append time should be zero,
	// and no flush should have occurred.
	require.True(t, proc.firstAppendTime.IsZero())
	require.Equal(t, 0, flusher.flushes)

	// Process a record containing some log lines. No flush should occur because
	// the builder has not reached the maximum age.
	proc.processRecord(ctx, newTestRecord(t, "tenant1", clock.Now()))

	// The first append time should be set to the current time, but no flush
	// should have occurred.
	require.Equal(t, clock.Now(), proc.firstAppendTime)
	require.Equal(t, clock.Now(), proc.lastModified)
	require.Equal(t, 0, flusher.flushes)

	// Advance the clock past the maximum age. A flush should occur, and the
	// the log lines from the record should be appended to the next data
	// object, not the one that was just flushed.
	clock.Advance(31 * time.Minute)
	proc.processRecord(ctx, newTestRecord(t, "tenant1", clock.Now()))

	// The last flushed time should be updated to the current time, and so should
	// the first append time to reflect the start of the new data object.
	require.Equal(t, clock.Now(), proc.firstAppendTime)
	require.Equal(t, clock.Now(), proc.lastModified)
	require.NotEqual(t, proc.builder.GetEstimatedSize(), 0)
	require.Equal(t, 1, flusher.flushes)

	// Advance the clock one last time and push some more logs. No flush should
	// occur because the next builder has not reached the maximum age.
	expectedLastFlushed := clock.Now()
	clock.Advance(time.Minute)
	proc.processRecord(ctx, newTestRecord(t, "tenant1", clock.Now()))
	require.Equal(t, expectedLastFlushed, proc.firstAppendTime)
	require.Equal(t, clock.Now(), proc.lastModified)
	require.Equal(t, 1, flusher.flushes)

	// Check the metrics.
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP loki_dataobj_consumer_flushes_total Total number of data objects flushed.
# TYPE loki_dataobj_consumer_flushes_total counter
loki_dataobj_consumer_flushes_total{partition="1",reason="max_age",topic="topic"} 1
`), "loki_dataobj_consumer_flushes_total"))
}

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	var (
		ctx     = t.Context()
		clock   = quartz.NewMock(t)
		reg     = prometheus.NewRegistry()
		flusher = &mockFlusher{}
		proc    *partitionProcessor
	)
	proc = newPartitionProcessor(testBuilderCfg, scratch.NewMemory(), 5*time.Minute, 30*time.Minute, "topic", 1, nil, flusher, log.NewNopLogger(), reg)
	proc.clock = clock

	// The builder is uninitialized, which means its size is also zero. No flush
	// should occur.
	flushed, err := proc.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.Equal(t, 0, flusher.flushes)

	// Advance the clock past the idle flush time. No flush should occur because
	// the builder is still unitialized.
	clock.Advance(6 * time.Minute)
	flushed, err = proc.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.Equal(t, 0, flusher.flushes)

	// Initialize the builder. However, no flush should occur because the builder
	// is still empty.
	require.NoError(t, proc.initBuilder())
	flushed, err = proc.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.Equal(t, 0, flusher.flushes)

	// Process a record containing some log lines. No flush should occur because
	// when log lines are appended to the builder it resets the idle timeout.
	proc.processRecord(ctx, newTestRecord(t, "tenant1", clock.Now()))
	require.False(t, proc.lastModified.IsZero())
	flushed, err = proc.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.Equal(t, 0, flusher.flushes)

	// Advance the clock past the idle timeout. A flush should occur.
	clock.Advance(6 * time.Minute)
	flushed, err = proc.idleFlush(ctx)
	require.NoError(t, err)
	require.True(t, flushed)
	require.Equal(t, 1, flusher.flushes)

	// Check the metrics.
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP loki_dataobj_consumer_flushes_total Total number of data objects flushed.
# TYPE loki_dataobj_consumer_flushes_total counter
loki_dataobj_consumer_flushes_total{partition="1",reason="idle",topic="topic"} 1
`), "loki_dataobj_consumer_flushes_total"))
}

// failureFlusher is a special flusher that always fails.
type failureFlusher struct{}

func (f *failureFlusher) FlushAsync(_ context.Context, _ builder, _ time.Time, _ int64, done func(error)) {
	done(errors.New("failed to flush"))
}

func TestPartitionProcessor_Flush(t *testing.T) {
	var (
		ctx         = t.Context()
		clock       = quartz.NewMock(t)
		reg         = prometheus.NewRegistry()
		mockFlusher = &mockFlusher{}
		_           = &failureFlusher{}
		proc        *partitionProcessor
	)
	proc = newPartitionProcessor(testBuilderCfg, scratch.NewMemory(), 5*time.Minute, 30*time.Minute, "topic", 1, nil, mockFlusher, log.NewNopLogger(), reg)
	proc.clock = clock

	// No flush should have occurred.
	require.Equal(t, 0, mockFlusher.flushes)

	// Process a record containing some log lines. No flush should occur.
	rec1 := newTestRecord(t, "tenant", clock.Now())
	proc.processRecord(ctx, rec1)
	require.Equal(t, clock.Now(), proc.firstAppendTime)
	require.Equal(t, clock.Now(), proc.lastModified)
	require.Equal(t, rec1, proc.lastRecord)

	// Advance the clock and force a flush.
	clock.Advance(time.Second)
	require.NoError(t, proc.flush(ctx))
	require.Equal(t, 1, mockFlusher.flushes)
	// The following fields should be reset at the end of every flush.
	require.True(t, proc.firstAppendTime.IsZero())
	require.True(t, proc.earliestRecordTime.IsZero())
	require.True(t, proc.lastModified.IsZero())
	require.Nil(t, proc.lastRecord)

	// Process another record containing some log lines. No flush should occur.
	proc.flusher = &failureFlusher{}
	rec2 := newTestRecord(t, "tenant", clock.Now())
	proc.processRecord(ctx, rec2)
	require.Equal(t, clock.Now(), proc.firstAppendTime)
	require.Equal(t, clock.Now(), proc.lastModified)
	require.Equal(t, rec2, proc.lastRecord)

	// Advance the clock and force a flush. This flush should fail.
	clock.Advance(time.Second)
	require.EqualError(t, proc.flush(ctx), "failed to flush")
	require.Equal(t, 1, mockFlusher.flushes)
	// Despite the failure, the following fields should still be reset.
	require.True(t, proc.firstAppendTime.IsZero())
	require.True(t, proc.earliestRecordTime.IsZero())
	require.True(t, proc.lastModified.IsZero())
	require.Nil(t, proc.lastRecord)
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
