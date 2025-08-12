package consumer

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

var testBuilderConfig = logsobj.BuilderConfig{
	TargetPageSize:          2048,
	TargetObjectSize:        1 << 22, // 4 MiB
	TargetSectionSize:       1 << 22, // 4 MiB
	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,
}

func TestPartitionProcessor_IdleTimeout(t *testing.T) {
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)
	p.idleTimeout = 60 * time.Minute

	// Last flush time should be initialized to the zero time.
	require.True(t, p.lastFlushed.IsZero())

	// Should not flush when builder is un-initialized.
	flushed, err := p.idleFlush()
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlushed.IsZero())

	// Should not flush if no records have been consumed.
	require.NoError(t, p.initBuilder())
	flushed, err = p.idleFlush()
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
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b,
		Timestamp: clock.Now(),
	})
	// A modification should have happened.
	require.False(t, p.lastModified.IsZero())
	// But the idle timeout should not have been reached.
	flushed, err = p.idleFlush()
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlushed.IsZero())

	// Advance the clock. The idle timeout should have been reached.
	clock.Advance((60 * time.Minute) + 1)
	flushed, err = p.idleFlush()
	require.NoError(t, err)
	require.True(t, flushed)
	require.Equal(t, clock.Now(), p.lastFlushed)
}

func TestPartitionProcessor_FlushTimeout(t *testing.T) {
	clock := quartz.NewMock(t)
	// Set up the test.
	p := newTestPartitionProcessor(t, clock)

	// The builder should be empty.
	require.NoError(t, p.initBuilder())
	require.Equal(t, p.builder.GetEstimatedSize(), 0)
	require.True(t, p.lastFlushed.IsZero())

	stream := logproto.Stream{
		Labels: `{cluster="test",app="foo"}`,
		Entries: []push.Entry{{
			Timestamp: time.Now().UTC(),
			Line:      "a",
		}},
	}
	streamBytes, err := stream.Marshal()
	require.NoError(t, err)

	// Push a record. It should be added to the current builder.
	p.processRecord(&kgo.Record{
		Value:     streamBytes,
		Key:       []byte("test-tenant"),
		Timestamp: clock.Now(),
	})
	size1 := p.builder.GetEstimatedSize()
	require.Greater(t, size1, 0)
	require.True(t, p.lastFlushed.IsZero())

	// Advance the clock and push another record. It should be added to the
	// same builder.
	clock.Advance(59 * time.Minute)
	p.processRecord(&kgo.Record{
		Value:     streamBytes,
		Key:       []byte("test-tenant"),
		Timestamp: clock.Now(),
	})
	size2 := p.builder.GetEstimatedSize()
	require.Greater(t, size2, size1)
	require.True(t, p.lastFlushed.IsZero())

	// Advance the clock up to the flush interval.
	clock.Advance(time.Minute)
	p.processRecord(&kgo.Record{
		Value:     streamBytes,
		Key:       []byte("test-tenant"),
		Timestamp: clock.Now(),
	})
	size3 := p.builder.GetEstimatedSize()
	require.Greater(t, size3, size2)
	require.True(t, p.lastFlushed.IsZero())

	// Advance the clock past the flush interval.
	clock.Advance(time.Second)
	p.processRecord(&kgo.Record{
		Value:     streamBytes,
		Key:       []byte("test-tenant"),
		Timestamp: clock.Now(),
	})
	size4 := p.builder.GetEstimatedSize()
	require.Less(t, size4, size3)
	require.Equal(t, clock.Now(), p.lastFlushed)
}

func TestPartitionProcessor_ProcessRecord(t *testing.T) {
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)

	// The builder is initialized to nil until the first record.
	require.Nil(t, p.builder)
	require.True(t, p.firstModified.IsZero())
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
	now1 := clock.Now()
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b,
		Timestamp: now1,
	})

	// The builder should be initialized and last modified timestamp updated.
	require.NotNil(t, p.builder)
	require.Greater(t, p.builder.GetEstimatedSize(), 0)
	require.Equal(t, now1, p.firstModified)
	require.Equal(t, now1, p.lastModified)

	// Advance the clock and push another record. Just the last modified
	// time should be updated.
	clock.Advance(time.Minute)
	now2 := clock.Now()
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b,
		Timestamp: now2,
	})
	require.Greater(t, p.builder.GetEstimatedSize(), 0)
	require.Equal(t, now1, p.firstModified)
	require.Equal(t, now2, p.lastModified)
}

func TestPartitionProcessor_Flush(t *testing.T) {
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)

	// The builder is initialized to nil until the first record.
	require.Nil(t, p.builder)
	require.True(t, p.firstModified.IsZero())
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
	now1 := clock.Now()
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b,
		Timestamp: now1,
	})
	require.Equal(t, now1, p.firstModified)
	require.Equal(t, now1, p.lastModified)
	require.Greater(t, p.builder.GetEstimatedSize(), 0)

	// Flush the builder.
	require.NoError(t, p.flush())
	// It all should have been reset.
	require.True(t, p.firstModified.IsZero())
	require.True(t, p.lastModified.IsZero())

}

func TestPartitionProcessor_OffsetsCommitted(t *testing.T) {
	t.Run("when builder is full", func(t *testing.T) {
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
		p.processRecord(&kgo.Record{
			Key:       []byte("test-tenant"),
			Value:     b,
			Timestamp: now1,
			Offset:    1,
		})

		// No flush should have occurred and no offsets should be committed.
		require.True(t, p.lastFlushed.IsZero())
		require.Nil(t, committer.records)

		// Mark the builder as full.
		wrappedBuilder.nextErr = logsobj.ErrBuilderFull

		// Append another record.
		clock.Advance(time.Minute)
		now2 := clock.Now()
		p.processRecord(&kgo.Record{
			Key:       []byte("test-tenant"),
			Value:     b,
			Timestamp: now2,
			Offset:    2,
		})

		// A flush should have occurred and offsets should be committed.
		require.Equal(t, now2, p.lastFlushed)
		require.Len(t, committer.records, 1)
		// The offset committed should be the offset of the first record, as that
		// was the record that was flushed.
		require.Equal(t, int64(1), committer.records[0].Offset)
	})

	t.Run("when idle timeout is exceeded", func(t *testing.T) {
		clock := quartz.NewMock(t)
		p := newTestPartitionProcessor(t, clock)
		p.idleTimeout = 60 * time.Minute
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
		p.processRecord(&kgo.Record{
			Key:       []byte("test-tenant"),
			Value:     b,
			Timestamp: now1,
			Offset:    1,
		})

		// No flush should have occurred and no offsets should be committed.
		require.True(t, p.lastFlushed.IsZero())
		require.Nil(t, committer.records)

		// Advance the clock past the idle timeout.
		clock.Advance(61 * time.Minute)
		now2 := clock.Now()
		flushed, err := p.idleFlush()
		require.NoError(t, err)
		require.True(t, flushed)

		// A flush should have occurred and offsets should be committed.
		require.Equal(t, now2, p.lastFlushed)
		require.Len(t, committer.records, 1)

		// The offset committed should be the offset of the first record, as that
		// was the record that was flushed.
		require.Equal(t, int64(1), committer.records[0].Offset)
	})
}

func newTestPartitionProcessor(_ *testing.T, clock quartz.Clock) *partitionProcessor {
	p := newPartitionProcessor(
		context.Background(),
		&mockCommitter{},
		testBuilderConfig,
		uploader.Config{},
		metastore.Config{},
		newMockBucket(),
		nil,
		"test-tenant",
		0,
		"test-topic",
		0,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
		&sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
		60*time.Minute,
		60*time.Minute,
		nil,
	)
	p.clock = clock
	return p
}
