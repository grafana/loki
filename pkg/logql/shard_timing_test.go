package logql

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/grafana/loki/pkg/push"
)

func testStream(labels string, n int) logproto.Stream {
	entries := make([]logproto.Entry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, push.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      "line",
		})
	}
	return logproto.Stream{Labels: labels, Entries: entries}
}

func TestShardTimeTracker_ReadStreams(t *testing.T) {
	it := iter.NewSortEntryIterator([]iter.EntryIterator{
		iter.NewStreamIterator(testStream(`{app="foo", __stream_shard__="0"}`, 5)),
		iter.NewStreamIterator(testStream(`{app="foo", __stream_shard__="1"}`, 5)),
		iter.NewStreamIterator(testStream(`{app="bar"}`, 5)),
	}, logproto.FORWARD)

	ctx := withShardTimeTracker(context.Background())
	tracker := shardTrackerFromContext(ctx)
	require.NotNil(t, tracker)

	streams, err := readStreams(it, 100, logproto.FORWARD, 0, tracker)
	require.NoError(t, err)
	require.Len(t, streams, 3)

	shardedStreams, unshardedStreams, shardedDur, unshardedDur := tracker.snapshot()
	require.Equal(t, 2, shardedStreams)
	require.Equal(t, 1, unshardedStreams)
	require.Greater(t, shardedDur, time.Duration(0))
	require.Greater(t, unshardedDur, time.Duration(0))
}

func TestShardTimeTracker_NilSafe(t *testing.T) {
	it := iter.NewStreamIterator(testStream(`{app="foo"}`, 2))
	streams, err := readStreams(it, 100, logproto.FORWARD, 0, nil)
	require.NoError(t, err)
	require.Len(t, streams, 1)

	var tracker *shardTimeTracker
	tracker.observe(`{app="foo"}`, time.Second) // must not panic
}

func TestShardTimeTracker_MetricsLine(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.NewLogfmtLogger(buf)

	ctx := withShardTimeTracker(context.Background())
	tracker := shardTrackerFromContext(ctx)
	tracker.observe(`{app="foo", __stream_shard__="1"}`, 250*time.Millisecond)
	tracker.observe(`{app="bar"}`, 50*time.Millisecond)

	now := time.Now()
	RecordRangeAndInstantQueryMetrics(ctx, logger, LiteralParams{
		queryString: `{app=~".+"}`,
		direction:   logproto.BACKWARD,
		end:         now,
		start:       now.Add(-1 * time.Hour),
		limit:       1000,
		step:        time.Minute,
	}, "200", stats.Result{}, logqlmodel.Streams{})

	out := buf.String()
	require.Contains(t, out, "sharded_streams=1")
	require.Contains(t, out, "sharded_streams_duration=250ms")
	require.Contains(t, out, "unsharded_streams=1")
	require.Contains(t, out, "unsharded_streams_duration=50ms")

	// Without a tracker in ctx (e.g. the frontend line), the fields are absent.
	buf.Reset()
	RecordRangeAndInstantQueryMetrics(context.Background(), logger, LiteralParams{
		queryString: `{app=~".+"}`,
		direction:   logproto.BACKWARD,
		end:         now,
		start:       now.Add(-1 * time.Hour),
		limit:       1000,
		step:        time.Minute,
	}, "200", stats.Result{}, logqlmodel.Streams{})
	require.NotContains(t, buf.String(), "sharded_streams")

	util_log.Logger = log.NewNopLogger()
}

func TestShardTimeTracker_LabelsCache(t *testing.T) {
	tracker := shardTrackerFromContext(withShardTimeTracker(context.Background()))

	// Same labels repeatedly: one distinct stream, durations accumulate.
	for i := 0; i < 10; i++ {
		tracker.observe(`{app="foo", __stream_shard__="1"}`, time.Millisecond)
	}
	// Alternate to check the cache doesn't misattribute on label change.
	tracker.observe(`{app="bar"}`, time.Millisecond)
	tracker.observe(`{app="foo", __stream_shard__="1"}`, time.Millisecond)

	shardedStreams, unshardedStreams, shardedDur, unshardedDur := tracker.snapshot()
	require.Equal(t, 1, shardedStreams)
	require.Equal(t, 1, unshardedStreams)
	require.Equal(t, 11*time.Millisecond, shardedDur)
	require.Equal(t, time.Millisecond, unshardedDur)
}
