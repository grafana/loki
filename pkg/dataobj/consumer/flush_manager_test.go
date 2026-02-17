package consumer

import (
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFlushManager(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		var (
			now             = time.Now()
			reg             = prometheus.NewRegistry()
			flusher         = &mockFlusher{}
			metastoreKafka  = &mockKafka{}
			metastoreEvents = newMetastoreEvents(1, 10, metastoreKafka)
			committer       = &mockCommitter{}
			flushManager    = newFlushManager(flusher, metastoreEvents, committer, 0, log.NewNopLogger(), reg)
		)
		// Create a builder and append some logs so it can be flushed.
		builder := newTestBuilder(t, reg)
		require.NoError(t, builder.Append("test", logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: now, Line: "test"},
			},
		}))
		require.NoError(t, flushManager.Flush(t.Context(), builder, "test", 1, now))
		// A flush should have occurred, a metastore event emitted, and the correct
		// offset was committed.
		require.Equal(t, 1, flusher.flushes)
		require.Len(t, metastoreKafka.produced, 1)
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
			flushManager    = newFlushManager(flusher, metastoreEvents, committer, 0, log.NewNopLogger(), reg)
		)
		// Create a builder and append some logs so it can be flushed.
		builder := newTestBuilder(t, reg)
		require.NoError(t, builder.Append("test", logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: now, Line: "test"},
			},
		}))
		flushErr := flushManager.Flush(t.Context(), builder, "test", 1, now)
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
}
