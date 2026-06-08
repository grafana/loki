package consumer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFlushCommitter(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		var (
			now             = time.Now()
			reg             = prometheus.NewRegistry()
			flusher         = &mockFlusher{}
			metastoreKafka  = &mockKafka{}
			metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
			committer       = &mockCommitter{}
			flushCommitter  = newFlushCommitter(flusher, metastoreEvents, committer, 0, log.NewNopLogger(), reg)
		)
		// Create a builder and append some logs so it can be flushed.
		b := newTestBuilder(t, reg)
		require.NoError(t, b.Append("test", logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: now, Line: "test"},
			},
		}, now))
		require.NoError(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
		// A flush should have occurred, a metastore event emitted, and the correct
		// offset was committed.
		require.Equal(t, 1, flusher.flushes)
		require.Len(t, metastoreKafka.produced, 1)
		// The emitted event must carry the builder's earliest record time.
		var event metastore.ObjectWrittenEvent
		require.NoError(t, event.Unmarshal(metastoreKafka.produced[0].Value))
		require.Equal(t, now.Format(time.RFC3339), event.EarliestRecordTime)
		require.Len(t, committer.offsets, 1)
		require.Equal(t, int64(1), committer.offsets[0])
		// Check that the metrics are correct.
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
	# HELP loki_dataobj_consumer_commits_total Total number of commits.
	# TYPE loki_dataobj_consumer_commits_total counter
	loki_dataobj_consumer_commits_total 1
	# HELP loki_dataobj_consumer_commit_failures_total Total number of commit failures.
	# TYPE loki_dataobj_consumer_commit_failures_total counter
	loki_dataobj_consumer_commit_failures_total 0
	`), "loki_dataobj_consumer_commits_total", "loki_dataobj_consumer_commit_failures_total"))
	})

	t.Run("should fail", func(t *testing.T) {
		var (
			now             = time.Now()
			reg             = prometheus.NewRegistry()
			flusher         = &failureFlusher{}
			metastoreKafka  = &mockKafka{}
			metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
			committer       = &mockCommitter{}
			flushCommitter  = newFlushCommitter(flusher, metastoreEvents, committer, 0, log.NewNopLogger(), reg)
		)
		// Create a builder and append some logs so it can be flushed.
		b := newTestBuilder(t, reg)
		require.NoError(t, b.Append("test", logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: now, Line: "test"},
			},
		}, now))
		flushErr := flushCommitter.Flush(t.Context(), []builder{b}, "test", 1)
		require.EqualError(t, flushErr, "failed to flush data object: mock error")
		// Since no flush occurred, no event should be emitted and no offsets
		// should be committed either.
		require.Len(t, metastoreKafka.produced, 0)
		require.Len(t, committer.offsets, 0)
		// Check that the metrics are correct.
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
	# HELP loki_dataobj_consumer_commits_total Total number of commits.
	# TYPE loki_dataobj_consumer_commits_total counter
	loki_dataobj_consumer_commits_total 0
	# HELP loki_dataobj_consumer_commit_failures_total Total number of commit failures.
	# TYPE loki_dataobj_consumer_commit_failures_total counter
	loki_dataobj_consumer_commit_failures_total 0
	`), "loki_dataobj_consumer_commits_total", "loki_dataobj_consumer_commit_failures_total"))
	})

	t.Run("should flush every builder but commit a single offset", func(t *testing.T) {
		var (
			now             = time.Now()
			reg             = prometheus.NewRegistry()
			flusher         = &mockFlusher{}
			metastoreKafka  = &mockKafka{}
			metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
			committer       = &mockCommitter{}
			flushCommitter  = newFlushCommitter(flusher, metastoreEvents, committer, 0, log.NewNopLogger(), reg)
		)
		// Build a slice of builders, mimicking a partition split across windows.
		var builders []builder
		for range 3 {
			b := newTestBuilder(t, prometheus.NewRegistry())
			require.NoError(t, b.Append("test", logproto.Stream{
				Labels:  `{foo="bar"}`,
				Entries: []logproto.Entry{{Timestamp: now, Line: "test"}},
			}, now))
			builders = append(builders, b)
		}

		require.NoError(t, flushCommitter.Flush(t.Context(), builders, "test", 7))
		// Each builder is flushed and emits its own metastore event.
		require.Equal(t, 3, flusher.flushes)
		require.Len(t, metastoreKafka.produced, 3)
		// But only a single offset is committed for the whole flush.
		require.Len(t, committer.offsets, 1)
		require.Equal(t, int64(7), committer.offsets[0])
	})

	t.Run("should capture earliest record time before the builder is reset", func(t *testing.T) {
		var (
			now = time.Now()
			reg = prometheus.NewRegistry()
			// flushingMockFlusher actually flushes (and thereby resets) the
			// builder, which clears its earliest record time.
			flusher         = &flushingMockFlusher{}
			metastoreKafka  = &mockKafka{}
			metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
			committer       = &mockCommitter{}
			flushCommitter  = newFlushCommitter(flusher, metastoreEvents, committer, 0, log.NewNopLogger(), reg)
		)
		b := newTestBuilder(t, reg)
		require.NoError(t, b.Append("test", logproto.Stream{
			Labels:  `{foo="bar"}`,
			Entries: []logproto.Entry{{Timestamp: now, Line: "test"}},
		}, now))

		require.NoError(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
		require.Equal(t, 1, flusher.flushes)
		require.Len(t, metastoreKafka.produced, 1)
		// The emitted event must keep the original earliest record time, even
		// though the builder was reset by the flush.
		var event metastore.ObjectWrittenEvent
		require.NoError(t, event.Unmarshal(metastoreKafka.produced[0].Value))
		require.Equal(t, now.Format(time.RFC3339), event.EarliestRecordTime)
	})
}

// flushingMockFlusher is a flusher that actually flushes the builder it is
// given, resetting the builder's buffered state (including its earliest record
// time) as a real flusher would.
type flushingMockFlusher struct {
	flushes int
}

func (m *flushingMockFlusher) Flush(_ context.Context, b builder, _ string) (string, error) {
	m.flushes++
	_, closer, err := b.Flush()
	if err != nil {
		return "", err
	}
	if closer != nil {
		_ = closer.Close()
	}
	return "", nil
}
