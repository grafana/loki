package consumer

import (
	"context"
	"strings"
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

func TestPartitionProcessor_Flush(t *testing.T) {
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)
	// Wrap the TOC writer to record all of the entries.
	tocWriter, ok := p.metastoreTocWriter.(*metastore.TableOfContentsWriter)
	require.True(t, ok)
	recordingTocWriter := &recordingTocWriter{TableOfContentsWriter: tocWriter}
	p.metastoreTocWriter = recordingTocWriter

	// Push two streams, one minute apart.
	now1 := clock.Now()
	s1 := logproto.Stream{
		Labels: `{service="test"}`,
		Entries: []push.Entry{{
			Timestamp: now1,
			Line:      "abc",
		}},
	}
	b1, err := s1.Marshal()
	require.NoError(t, err)
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b1,
		Timestamp: now1,
	})

	// Push the second stream, one minute later.
	clock.Advance(time.Minute)
	now2 := clock.Now()
	s2 := s1
	s2.Entries[0].Timestamp = now2
	b2, err := s2.Marshal()
	require.NoError(t, err)
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b2,
		Timestamp: now2,
	})

	// No flush should have occurred, we will flush ourselves instead.
	require.True(t, p.lastFlush.IsZero())

	// Get the time range. We will use this to check that the metastore has
	// the correct time range.
	minTime, maxTime := p.builder.TimeRange()
	require.Equal(t, now1, minTime)
	require.Equal(t, now2, maxTime)

	// Flush the data object.
	require.NoError(t, p.flush())
	require.Equal(t, now2, p.lastFlush)

	// Flush should produce two uploads, the data object and the metastore
	// object.
	bucket, ok := p.bucket.(*mockBucket)
	require.True(t, ok)
	require.Len(t, bucket.uploads, 2)

	// Check that the expected entries were written to the metastore.
	require.Len(t, recordingTocWriter.entries, 1)
	actual := recordingTocWriter.entries[0]
	require.Equal(t, minTime, actual.MinTimestamp)
	require.Equal(t, maxTime, actual.MaxTimestamp)
}

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)
	p.idleFlushTimeout = 60 * time.Minute
	// Use a mock committer as we don't have a real Kafka client that can
	// commit offsets.
	committer := &mockCommitter{}
	p.committer = committer

	// Last flush time should be initialized to the zero time.
	require.True(t, p.lastFlush.IsZero())

	// Should not flush when builder is un-initialized.
	flushed, err := p.idleFlush()
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlush.IsZero())

	// Should not flush if no records have been consumed.
	require.NoError(t, p.initBuilder())
	flushed, err = p.idleFlush()
	require.NoError(t, err)
	require.False(t, flushed)
	require.True(t, p.lastFlush.IsZero())

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
	require.True(t, p.lastFlush.IsZero())

	// Advance the clock. The idle timeout should have been reached.
	clock.Advance((60 * time.Minute) + 1)
	flushed, err = p.idleFlush()
	require.NoError(t, err)
	require.True(t, flushed)
	require.Equal(t, clock.Now(), p.lastFlush)
}

func TestPartitionProcessor_ProcessRecord(t *testing.T) {
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
	p.processRecord(&kgo.Record{
		Key:       []byte("test-tenant"),
		Value:     b,
		Timestamp: clock.Now(),
	})

	// The builder should be initialized and last modified timestamp updated.
	require.NotNil(t, p.builder)
	require.Equal(t, clock.Now(), p.lastModified)
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
		require.True(t, p.lastFlush.IsZero())
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
		require.Equal(t, now2, p.lastFlush)
		require.Len(t, committer.records, 1)
		// The offset committed should be the offset of the first record, as that
		// was the record that was flushed.
		require.Equal(t, int64(1), committer.records[0].Offset)
	})

	t.Run("when idle timeout is exceeded", func(t *testing.T) {
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
		p.processRecord(&kgo.Record{
			Key:       []byte("test-tenant"),
			Value:     b,
			Timestamp: now1,
			Offset:    1,
		})

		// No flush should have occurred and no offsets should be committed.
		require.True(t, p.lastFlush.IsZero())
		require.Nil(t, committer.records)

		// Advance the clock past the idle timeout.
		clock.Advance(61 * time.Minute)
		now2 := clock.Now()
		flushed, err := p.idleFlush()
		require.NoError(t, err)
		require.True(t, flushed)

		// A flush should have occurred and offsets should be committed.
		require.Equal(t, now2, p.lastFlush)
		require.Len(t, committer.records, 1)

		// The offset committed should be the offset of the first record, as that
		// was the record that was flushed.
		require.Equal(t, int64(1), committer.records[0].Offset)
	})
}

func newTestPartitionProcessor(_ *testing.T, clock quartz.Clock) *partitionProcessor {
	p := newPartitionProcessor(
		context.Background(),
		&kgo.Client{},
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
		60*time.Minute,
		nil,
	)
	p.clock = clock
	return p
}
