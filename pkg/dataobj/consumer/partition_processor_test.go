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

func TestPartitionProcessor_IdleFlush(t *testing.T) {
	clock := quartz.NewMock(t)
	p := newTestPartitionProcessor(t, clock)
	p.idleFlushTimeout = 60 * time.Minute

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
		&sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
		60*time.Minute,
		nil,
	)
	p.clock = clock
	return p
}
