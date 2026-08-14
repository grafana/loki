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

func TestStripShard(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{__stream_shard__="0", app="a"}`, `{app="a"}`},                   // first
		{`{a="1", __stream_shard__="0", b="2"}`, `{a="1", b="2"}`},         // middle
		{`{a="1", __stream_shard__="0"}`, `{a="1"}`},                       // last
		{`{a="1"}`, `{a="1"}`},                                             // no shard label
	} {
		require.Equal(t, tc.want, stripShard(tc.in), tc.in)
	}
	// All shards of one logical stream must collapse to the same key.
	require.Equal(t, stripShard(`{__stream_shard__="0", app="a"}`), stripShard(`{__stream_shard__="7", app="a"}`))
}

func TestShardTimeTracker_PerStreamTop(t *testing.T) {
	tr := shardTrackerFromContext(withShardTimeTracker(context.Background()))
	// stream a: 2 shards, 1s + 2s = 3s; stream b: 1 shard, 5s.
	tr.observe(`{__stream_shard__="0", app="a"}`, time.Second)
	tr.observe(`{__stream_shard__="1", app="a"}`, 2*time.Second)
	tr.observe(`{__stream_shard__="0", app="b"}`, 5*time.Second)
	// an unsharded stream must not appear.
	tr.observe(`{app="c"}`, 9*time.Second)

	top := tr.perStreamTop(0)
	require.Len(t, top, 2)
	// sorted by sum desc: b (5s) then a (3s).
	require.Equal(t, `{app="b"}`, top[0].Stream)
	require.Equal(t, int64(5*time.Second), top[0].SumDurationNanos)
	require.Equal(t, int64(5*time.Second), top[0].MaxDurationNanos)
	require.Equal(t, int64(1), top[0].Shards)
	require.Equal(t, `{app="a"}`, top[1].Stream)
	require.Equal(t, int64(3*time.Second), top[1].SumDurationNanos)
	require.Equal(t, int64(2), top[1].Shards)

	// top-k truncation keeps the heaviest.
	require.Len(t, tr.perStreamTop(1), 1)
	require.Equal(t, `{app="b"}`, tr.perStreamTop(1)[0].Stream)
}

func TestRecordMetrics_FrontendUnshardedEstimate(t *testing.T) {
	now := time.Now()
	params := LiteralParams{
		queryString: `{app=~".+"}`,
		direction:   logproto.BACKWARD,
		end:         now,
		start:       now.Add(-1 * time.Hour),
		limit:       1000,
		step:        time.Minute,
	}
	sharded := stats.Result{ShardedStreams: []stats.ShardedStream{
		{Stream: `{app="a"}`, SumDurationNanos: int64(3 * time.Second), MaxDurationNanos: int64(1 * time.Second), Shards: 3},
		{Stream: `{app="b"}`, SumDurationNanos: int64(10 * time.Second), MaxDurationNanos: int64(4 * time.Second), Shards: 5},
	}}

	// Frontend context: estimate is logged, dominated by stream b (10-4=6s > 3-1=2s).
	buf := bytes.NewBufferString("")
	logger := log.NewLogfmtLogger(buf)
	frontendCtx := WithComponentContext(context.Background(), "frontend")
	RecordRangeAndInstantQueryMetrics(frontendCtx, logger, params, "200", sharded, logqlmodel.Streams{})
	out := buf.String()
	require.Contains(t, out, "unsharded_added_estimate=6s")
	require.Contains(t, out, "unsharded_critical_shards=5")
	require.Contains(t, out, "sharded_total_duration=13s")
	require.Contains(t, out, `unsharded_critical_stream="{app=\"b\"}"`)

	// Non-frontend context: no estimate (only meaningful once merged at the frontend).
	buf.Reset()
	RecordRangeAndInstantQueryMetrics(context.Background(), logger, params, "200", sharded, logqlmodel.Streams{})
	require.NotContains(t, buf.String(), "unsharded_added_estimate")

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
