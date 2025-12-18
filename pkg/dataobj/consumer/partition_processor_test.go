package consumer

import (
	"strings"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

var testBuilderConfig = logsobj.BuilderConfig{
	BuilderBaseConfig: logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10,
		TargetObjectSize:        1 << 22, // 4 MiB
		TargetSectionSize:       1 << 22, // 4 MiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	},
}

func TestPartitionProcessor_Flush(t *testing.T) {
	t.Run("reset happens after flush", func(t *testing.T) {
		ctx := t.Context()
		clock := quartz.NewMock(t)
		p := newTestPartitionProcessor(t, clock)

		// All timestamps should be zero.
		require.True(t, p.lastFlushed.IsZero())
		require.True(t, p.lastModified.IsZero())

		// Push a stream.
		now := clock.Now()
		s := logproto.Stream{
			Labels: `{service="test"}`,
			Entries: []push.Entry{{
				Timestamp: now,
				Line:      "abc",
			}},
		}
		b, err := s.Marshal()
		require.NoError(t, err)
		p.processRecord(ctx, partition.Record{
			TenantID:  "test-tenant",
			Content:   b,
			Timestamp: now,
		})

		// No flush should have occurred, we will flush ourselves instead.
		require.True(t, p.lastFlushed.IsZero())

		// The last modified timestamp should be the time of the last append.
		require.Equal(t, now, p.lastModified)

		// Flush the data object. The last modified time should also be reset.
		require.NoError(t, p.flush(ctx))
		require.Equal(t, now, p.lastFlushed)
		require.True(t, p.lastModified.IsZero())
	})

	t.Run("has emitted metastore event", func(t *testing.T) {
		ctx := t.Context()
		clock := quartz.NewMock(t)
		p := newTestPartitionProcessor(t, clock)

		client := &mockKafka{}
		p.eventsProducerClient = client
		p.partition = 23

		// All timestamps should be zero.
		require.True(t, p.lastFlushed.IsZero())
		require.True(t, p.lastModified.IsZero())

		// Push a stream.
		now := clock.Now()
		s := logproto.Stream{
			Labels: `{service="test"}`,
			Entries: []push.Entry{{
				Timestamp: now,
				Line:      "abc",
			}},
		}
		b, err := s.Marshal()
		require.NoError(t, err)
		p.processRecord(ctx, partition.Record{
			TenantID:  "test-tenant",
			Content:   b,
			Timestamp: now,
		})

		// No flush should have occurred, we will flush ourselves instead.
		require.True(t, p.lastFlushed.IsZero())

		// Flush the data object. The last modified time should also be reset.
		require.NoError(t, p.flush(ctx))

		// Check that the metastore event was emitted.
		require.Len(t, client.produced, 1)
		// Partition should be the processor's partition divided by the partition ratio, in integer division.
		require.Equal(t, int32(2), client.produced[0].Partition)
	})
}

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	ctx := t.Context()
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)
	p.idleFlushTimeout = 60 * time.Minute
	// Use a mock committer as we don't have a real Kafka client that can
	// commit offsets.
	committer := &mockCommitter{}
	p.committer = committer

	// Last flush time should be initialized to the zero time.
	require.True(t, p.lastFlushed.IsZero())

	// Should not flush when builder is un-initialized.
	flushed, err := p.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlushed.IsZero())

	// Should not flush if no records have been consumed.
	require.NoError(t, p.initBuilder())
	flushed, err = p.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlushed.IsZero())

	// Should not flush if idle timeout is not reached.
	s := logproto.Stream{
		Labels: `{service="test"}`,
		Entries: []push.Entry{{
			Timestamp: time.Now().UTC(),
			Line:      "abc",
		}},
	}
	b, err := s.Marshal()
	require.NoError(t, err)
	p.processRecord(ctx, partition.Record{
		TenantID:  "test-tenant",
		Content:   b,
		Timestamp: clock.Now(),
	})
	// A modification should have happened.
	require.False(t, p.lastModified.IsZero())
	// But the idle timeout should not have been reached.
	flushed, err = p.idleFlush(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlushed.IsZero())

	// Advance the clock. The idle timeout should have been reached.
	clock.Advance((60 * time.Minute) + 1)
	flushed, err = p.idleFlush(ctx)
	require.NoError(t, err)
	require.True(t, flushed)
	require.Equal(t, clock.Now(), p.lastFlushed)
}

func TestPartitionProcessor_OffsetsCommitted(t *testing.T) {
	t.Run("when builder is full", func(t *testing.T) {
		ctx := t.Context()
		clock := quartz.NewMock(t)
		p := newTestPartitionProcessor(t, clock)
		// Use our own builder instead of initBuilder.
		builder, err := logsobj.NewBuilder(testBuilderConfig, scratch.NewMemory())
		require.NoError(t, err)
		wrappedBuilder := &mockBuilder{builder: builder}
		p.builderOnce.Do(func() { p.builder = wrappedBuilder })
		// Use a mock committer so we can assert when offsets are committed.
		committer := &mockCommitter{}
		p.committer = committer

		// Push a record.
		now1 := clock.Now()
		s := logproto.Stream{
			Labels: `{service="test"}`,
			Entries: []push.Entry{{
				Timestamp: now1,
				Line:      strings.Repeat("a", 1024),
			}},
		}
		b, err := s.Marshal()
		require.NoError(t, err)
		p.processRecord(ctx, partition.Record{
			TenantID:  "test-tenant",
			Content:   b,
			Timestamp: now1,
			Offset:    1,
		})

		// No flush should have occurred and no offsets should be committed.
		require.True(t, p.lastFlushed.IsZero())
		require.Nil(t, committer.offsets)

		// Mark the builder as full.
		wrappedBuilder.nextErr = logsobj.ErrBuilderFull

		// Append another record.
		clock.Advance(time.Minute)
		now2 := clock.Now()
		p.processRecord(ctx, partition.Record{
			TenantID:  "test-tenant",
			Content:   b,
			Timestamp: now2,
			Offset:    2,
		})

		// A flush should have occurred and offsets should be committed.
		require.Equal(t, now2, p.lastFlushed)
		require.Len(t, committer.offsets, 1)
		// The offset committed should be the offset of the first record, as that
		// was the record that was flushed.
		require.Equal(t, int64(1), committer.offsets[0])
	})

	t.Run("when idle timeout is exceeded", func(t *testing.T) {
		ctx := t.Context()
		clock := quartz.NewMock(t)
		p := newTestPartitionProcessor(t, clock)
		p.idleFlushTimeout = 60 * time.Minute
		// Use a mock committer so we can assert when offsets are committed.
		committer := &mockCommitter{}
		p.committer = committer

		// Push a record.
		now1 := clock.Now()
		s := logproto.Stream{
			Labels: `{service="test"}`,
			Entries: []push.Entry{{
				Timestamp: now1,
				Line:      strings.Repeat("a", 1024),
			}},
		}
		b, err := s.Marshal()
		require.NoError(t, err)
		p.processRecord(ctx, partition.Record{
			TenantID:  "test-tenant",
			Content:   b,
			Timestamp: now1,
			Offset:    1,
		})

		// No flush should have occurred and no offsets should be committed.
		require.True(t, p.lastFlushed.IsZero())
		require.Nil(t, committer.offsets)

		// Advance the clock past the idle timeout.
		clock.Advance(61 * time.Minute)
		now2 := clock.Now()
		flushed, err := p.idleFlush(ctx)
		require.NoError(t, err)
		require.True(t, flushed)

		// A flush should have occurred and offsets should be committed.
		require.Equal(t, now2, p.lastFlushed)
		require.Len(t, committer.offsets, 1)

		// The offset committed should be the offset of the first record, as that
		// was the record that was flushed.
		require.Equal(t, int64(1), committer.offsets[0])
	})
}

func TestPartitionProcessor_ProcessRecord(t *testing.T) {
	ctx := t.Context()
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)

	// The builder is initialized to nil until the first record.
	require.Nil(t, p.builder)
	require.True(t, p.lastModified.IsZero())

	// Push a record.
	s := logproto.Stream{
		Labels: `{service="test"}`,
		Entries: []push.Entry{{
			Timestamp: time.Now().UTC(),
			Line:      "abc",
		}},
	}
	b, err := s.Marshal()
	require.NoError(t, err)
	p.processRecord(ctx, partition.Record{
		TenantID:  "test-tenant",
		Content:   b,
		Timestamp: clock.Now(),
	})

	// The builder should be initialized and last modified timestamp updated.
	require.NotNil(t, p.builder)
	require.Equal(t, clock.Now(), p.lastModified)
}

func newTestPartitionProcessor(t *testing.T, clock quartz.Clock) *partitionProcessor {
	t.Helper()
	p := newPartitionProcessor(
		&mockCommitter{},
		testBuilderConfig,
		uploader.Config{},
		metastore.Config{
			PartitionRatio: 10,
		},
		newMockBucket(),
		nil,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
		60*time.Minute,
		nil,
		"test-topic",
		1,
	)
	p.clock = clock
	p.eventsProducerClient = &mockKafka{}
	require.NotZero(t, p.partition)
	return p
}
