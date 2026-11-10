package ingester

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

// TestFlushMarksLiveChunkDescWhenChunkCutDuringFlush guards against a push that
// cuts a chunk while a flush of that stream is in flight. The cut grows
// stream.chunks, and the flush must still mark the live chunkDesc as flushed,
// otherwise the next sweep flushes the same chunk a second time.
func TestFlushMarksLiveChunkDescWhenChunkCutDuringFlush(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.FlushCheckPeriod = time.Hour // keep the background sweep out of the way
	cfg.RetainPeriod = 0
	store, ing := newTestStore(t, cfg, nil)
	defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

	const userID = "u"
	ctx := user.InjectOrgID(context.Background(), userID)
	lbs := model.LabelSet{"app": "a"}.String()
	push := func(ts time.Time, line string) {
		_, err := ing.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels: lbs, Entries: []logproto.Entry{{Timestamp: ts, Line: line}},
		}}})
		require.NoError(t, err)
	}

	push(time.Unix(0, 1), "1")

	inst, ok := ing.getInstanceByID(userID)
	require.True(t, ok)
	var s *stream
	_ = inst.streams.ForEach(func(st *stream) (bool, error) { s = st; return false, nil })
	s.chunkMtx.RLock()
	t.Logf("before flush: len=%d cap=%d", len(s.chunks), cap(s.chunks))
	s.chunkMtx.RUnlock()

	var puts atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	store.onPut = func(context.Context, []chunk.Chunk) error {
		puts.Inc()
		once.Do(func() { close(started); <-release })
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- ing.flushUserSeries(context.Background(), userID, s.fp, true) }()
	<-started

	// Chunk 0 is closed by collectChunksToFlush, so this push cuts a new chunk.
	push(time.Unix(0, 2), "2")
	close(release)
	require.NoError(t, <-done)

	s.chunkMtx.RLock()
	t.Logf("after flush: len=%d cap=%d chunks[0].flushed.IsZero()=%v closed=%v",
		len(s.chunks), cap(s.chunks), s.chunks[0].flushed.IsZero(), s.chunks[0].closed)
	stale := s.chunks[0].flushed.IsZero()
	s.chunkMtx.RUnlock()

	require.False(t, stale, "flushed chunk's live chunkDesc still has flushed=0")

	// A normal (non-immediate) flush must not re-put the already flushed chunk.
	require.NoError(t, ing.flushUserSeries(context.Background(), userID, s.fp, false))
	require.Equal(t, int32(1), puts.Load(), "flushed chunk was put again")
}
