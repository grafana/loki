package ingester

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	loki_runtime "github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/constants"
	utilwal "github.com/grafana/loki/v3/pkg/util/wal"
	"github.com/grafana/loki/v3/pkg/validation"
)

const definitionTestLabels = `{app="wal-retention"}`

func newDefinitionTestIngester(t *testing.T, dir string) (*Ingester, func()) {
	t.Helper()
	cfg := defaultIngesterTestConfigWithWAL(t, dir)
	cfg.WAL.CheckpointDuration = time.Hour
	cfg.RetainPeriod = 0
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	i, err := New(cfg, client.Config{}, &mockStore{chunks: map[string][]chunk.Chunk{}}, limits,
		loki_runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki,
		log.NewNopLogger(), nil, mockReadRingWithOneActiveIngester(), nil)
	require.NoError(t, err)
	var once sync.Once
	stop := func() { once.Do(func() { require.NoError(t, i.wal.Stop()) }) }
	t.Cleanup(stop)
	return i, stop
}

func definitionTestPush(t *testing.T, i *Ingester, n int) *stream {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), "wal-retention-test")
	_, err := i.Push(ctx, definitionTestRequest(n))
	require.NoError(t, err)
	inst, err := i.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	s, ok := inst.streams.Load(definitionTestLabels)
	require.True(t, ok)
	return s
}

func definitionTestRequest(n int) *logproto.PushRequest {
	return &logproto.PushRequest{Streams: []logproto.Stream{{
		Labels:  definitionTestLabels,
		Entries: []logproto.Entry{{Timestamp: time.Unix(1700000000+int64(n), 0), Line: fmt.Sprintf("entry-%d", n)}},
	}}}
}

func definitionTestFlush(t *testing.T, i *Ingester, s *stream) {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), "wal-retention-test")
	cs := make([]*chunkDesc, len(s.chunks))
	for j := range s.chunks {
		cs[j] = &s.chunks[j]
	}
	require.NoError(t, i.flushChunks(ctx, s.fp, s.labels, cs, &s.chunkMtx))
	inst, err := i.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	i.removeFlushedChunks(inst, s, true)
	require.Empty(t, s.chunks)
	_, ok := inst.streams.LoadByFP(s.fp)
	require.False(t, ok, "the flushed stream must be removed from memory")
}

func definitionTestRecover(t *testing.T, dir string) *Ingester {
	t.Helper()
	i, _ := newDefinitionTestIngester(t, dir)
	r := newIngesterRecoverer(i)
	reader, closer, err := newCheckpointReader(dir, log.NewNopLogger())
	require.NoError(t, err)
	require.NoError(t, RecoverCheckpoint(reader, r))
	require.NoError(t, closer.Close())
	walReader, walCloser, err := utilwal.NewWalReader(dir, -1)
	require.NoError(t, err)
	// Replay fails with "stream (...) not set during WAL replay" if an entry
	// refers to a stream that neither the checkpoint nor the segments define.
	err = RecoverWAL(context.Background(), walReader, r)
	require.NoError(t, walCloser.Close())
	require.NoError(t, err)
	r.Close()
	return i
}

// Entries written after the checkpoint's WAL rotation are flushed, and their
// stream removed, before the checkpoint reaches the stream. The checkpoint must
// still keep the stream's definition and counter for the post-rotation records.
func TestWALDefinitionsSurviveFlushDuringCheckpoint(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		t.Run(fmt.Sprintf("recreate=%t", recreate), func(t *testing.T) {
			dir := t.TempDir()
			i, stop := newDefinitionTestIngester(t, dir)
			s := definitionTestPush(t, i, 1)
			w := i.wal.(*walWrapper)
			writer := w.checkpointWriter()
			cp := NewCheckpointer(time.Millisecond, w.seriesIter, writer, i.metrics, make(chan struct{}))
			iter, noop, err := cp.prepare()
			require.NoError(t, err)
			require.False(t, noop)
			definitionTestPush(t, i, 2)
			definitionTestFlush(t, i, s)
			if recreate {
				fresh := definitionTestPush(t, i, 3)
				require.NotSame(t, s, fresh)
				require.Equal(t, int64(3), fresh.entryCt)
			}
			require.True(t, iter.Next(), "a flushed stream must still have a checkpoint definition")
			require.Empty(t, iter.Stream().Chunks)
			require.Equal(t, int64(2), iter.Stream().EntryCt)
			require.NoError(t, writer.Write(iter.Stream()))
			require.False(t, iter.Next())
			require.NoError(t, writer.Close(false))
			iter.forgetRetired()
			stop()

			recovered := definitionTestRecover(t, dir)
			inst, err := recovered.GetOrCreateInstance("wal-retention-test")
			require.NoError(t, err)
			if recreate {
				got, ok := inst.streams.Load(definitionTestLabels)
				require.True(t, ok)
				require.Equal(t, int64(3), got.entryCt)
				require.Len(t, got.chunks, 1)
				require.Equal(t, 1, got.chunks[0].chunk.Size(), "only the unflushed new entry should replay")
			} else {
				require.Zero(t, inst.streams.Len(), "flushed entries must not be resurrected")
				fresh := definitionTestPush(t, recovered, 3)
				require.Equal(t, int64(3), fresh.entryCt)
			}
		})
	}
}

// failingCheckpointWriter fails one stage of a checkpoint, aborting the real one.
type failingCheckpointWriter struct {
	*WALCheckpointWriter
	failAdvance, failWrite, failClose bool
}

var errInjectedCheckpoint = errors.New("injected checkpoint failure")

func (w *failingCheckpointWriter) Advance() (bool, error) {
	if w.failAdvance {
		return false, errInjectedCheckpoint
	}
	return w.WALCheckpointWriter.Advance()
}

func (w *failingCheckpointWriter) Write(s *Series) error {
	if w.failWrite {
		return errInjectedCheckpoint
	}
	return w.WALCheckpointWriter.Write(s)
}

func (w *failingCheckpointWriter) Close(abort bool) error {
	if w.failClose && !abort {
		_ = w.WALCheckpointWriter.Close(true)
		return errInjectedCheckpoint
	}
	return w.WALCheckpointWriter.Close(abort)
}

func TestWALDefinitionsSurviveFailedCheckpoints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writer failingCheckpointWriter
	}{
		{name: "advance", writer: failingCheckpointWriter{failAdvance: true}},
		{name: "write", writer: failingCheckpointWriter{failWrite: true}},
		{name: "close", writer: failingCheckpointWriter{failClose: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := newDefinitionTestIngester(t, t.TempDir())
			s := definitionTestPush(t, i, 1)
			// A second stream gives the failing checkpoint something to write.
			ctx := user.InjectOrgID(context.Background(), "wal-retention-test")
			_, err := i.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{{
				Labels:  `{app="wal-retention-other"}`,
				Entries: []logproto.Entry{{Timestamp: time.Unix(1700000000, 0), Line: "other"}},
			}}})
			require.NoError(t, err)
			definitionTestFlush(t, i, s)
			inst, err := i.GetOrCreateInstance("wal-retention-test")
			require.NoError(t, err)

			w := i.wal.(*walWrapper)
			failing := tc.writer
			failing.WALCheckpointWriter = w.checkpointWriter()
			require.ErrorIs(t, NewCheckpointer(time.Millisecond, w.seriesIter, &failing, i.metrics, make(chan struct{})).PerformCheckpoint(), errInjectedCheckpoint)
			_, exists := inst.retiredWALStreams.Load(s.fp)
			require.True(t, exists, "a failed checkpoint must not release definitions")

			require.NoError(t, NewCheckpointer(time.Millisecond, w.seriesIter, w.checkpointWriter(), i.metrics, make(chan struct{})).PerformCheckpoint())
			_, exists = inst.retiredWALStreams.Load(s.fp)
			require.False(t, exists, "a successful checkpoint releases superseded definitions")
		})
	}
}

func TestWALDefinitionRecreatedDuringCheckpointIsNotExpired(t *testing.T) {
	dir := t.TempDir()
	i, stop := newDefinitionTestIngester(t, dir)
	s := definitionTestPush(t, i, 1)
	definitionTestFlush(t, i, s)
	w := i.wal.(*walWrapper)
	writer := w.checkpointWriter()
	cp := NewCheckpointer(time.Millisecond, w.seriesIter, writer, i.metrics, make(chan struct{}))
	iter, _, err := cp.prepare()
	require.NoError(t, err)
	require.False(t, iter.Next())
	s = definitionTestPush(t, i, 2)
	definitionTestFlush(t, i, s)
	require.NoError(t, writer.Close(false))
	iter.forgetRetired()
	inst, err := i.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	_, exists := inst.retiredWALStreams.Load(s.fp)
	require.True(t, exists, "a newer retirement is still referenced by the post-boundary WAL")
	s = definitionTestPush(t, i, 3)
	require.Equal(t, int64(3), s.entryCt)
	stop()
	recovered := definitionTestRecover(t, dir)
	inst, err = recovered.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	s, exists = inst.streams.Load(definitionTestLabels)
	require.True(t, exists)
	require.Equal(t, int64(3), s.entryCt, "replay must retain persisted counters when the prefix has expired")
	require.Equal(t, 2, s.chunks[0].chunk.Size())
}

type blockedDefinitionWAL struct {
	WAL
	entered chan struct{}
	release chan struct{}
	blocked atomic.Bool
}

func (w *blockedDefinitionWAL) Log(record *wal.Record) error {
	if w.blocked.CompareAndSwap(false, true) {
		close(w.entered)
		<-w.release
	}
	return w.WAL.Log(record)
}

func TestWALCheckpointWaitsForInflightPush(t *testing.T) {
	i, _ := newDefinitionTestIngester(t, t.TempDir())
	inst, err := i.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	w := i.wal.(*walWrapper)
	blocked := &blockedDefinitionWAL{WAL: w, entered: make(chan struct{}), release: make(chan struct{})}
	inst.wal = blocked
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(blocked.release) }) })
	pushDone := make(chan error, 1)
	go func() { pushDone <- inst.Push(context.Background(), definitionTestRequest(1)) }()
	<-blocked.entered
	cp := NewCheckpointer(time.Millisecond, w.seriesIter, w.checkpointWriter(), i.metrics, make(chan struct{}))
	checkpointDone := make(chan error, 1)
	go func() { checkpointDone <- cp.PerformCheckpoint() }()
	select {
	case err := <-checkpointDone:
		t.Fatalf("checkpoint passed an in-flight WAL write: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release.Do(func() { close(blocked.release) })
	require.NoError(t, <-pushDone)
	require.NoError(t, <-checkpointDone)
}

// A checkpointed stream is flushed, removed and then receives a new entry that is
// only in the WAL. The returning stream must continue its counter, or replay drops
// the new entry as a duplicate of the checkpoint's higher counter.
func TestWALRecreatedStreamEntriesSurviveReplay(t *testing.T) {
	dir := t.TempDir()
	i, stop := newDefinitionTestIngester(t, dir)
	definitionTestPush(t, i, 1)
	s := definitionTestPush(t, i, 2)
	w := i.wal.(*walWrapper)
	cp := NewCheckpointer(time.Millisecond, w.seriesIter, w.checkpointWriter(), i.metrics, make(chan struct{}))
	require.NoError(t, cp.PerformCheckpoint())
	definitionTestFlush(t, i, s)
	fresh := definitionTestPush(t, i, 3)
	require.Equal(t, int64(3), fresh.entryCt)
	stop()

	recovered := definitionTestRecover(t, dir)
	inst, err := recovered.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	got, ok := inst.streams.Load(definitionTestLabels)
	require.True(t, ok)
	require.Equal(t, int64(3), got.entryCt)
	require.Len(t, got.chunks, 1)
	// The checkpoint's two entries plus the entry that is only in the WAL.
	require.Equal(t, 3, got.chunks[0].chunk.Size(), "the unflushed entry must be recovered")
}

// definitionTestPushOther keeps a second stream in memory, so checkpoints serialize streams.
func definitionTestPushOther(t *testing.T, i *Ingester) {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), "wal-retention-test")
	_, err := i.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{{
		Labels:  `{app="wal-retention-other"}`,
		Entries: []logproto.Entry{{Timestamp: time.Unix(1700000000, 0), Line: "other"}},
	}}})
	require.NoError(t, err)
}

// Close still reports success when deleting the superseded segments fails. Retired
// metadata must survive until those segments are gone, even when the same writer's
// previous checkpoint did delete its segments.
func TestWALDefinitionsKeptWhileSupersededSegmentsRemain(t *testing.T) {
	i, _ := newDefinitionTestIngester(t, t.TempDir())
	definitionTestPushOther(t, i)
	inst, err := i.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	w := i.wal.(*walWrapper)
	writer := w.checkpointWriter()
	cp := NewCheckpointer(time.Millisecond, w.seriesIter, writer, i.metrics, make(chan struct{}))

	first := definitionTestPush(t, i, 1)
	definitionTestFlush(t, i, first)
	require.NoError(t, cp.PerformCheckpoint())
	_, exists := inst.retiredWALStreams.Load(first.fp)
	require.False(t, exists, "a checkpoint that deletes its segments releases the definition")

	second := definitionTestPush(t, i, 2)
	definitionTestFlush(t, i, second)
	writer.truncate = func(int) error { return errInjectedCheckpoint }
	require.NoError(t, cp.PerformCheckpoint())
	_, exists = inst.retiredWALStreams.Load(second.fp)
	require.True(t, exists, "superseded segments are still on disk")

	writer.truncate = nil
	require.NoError(t, cp.PerformCheckpoint())
	_, exists = inst.retiredWALStreams.Load(second.fp)
	require.False(t, exists, "the next checkpoint deletes the leftover segments and releases the definition")
}

// After a failed truncation the stale segments are replayed on restart. A stream that
// returns in the meantime must continue its counter, or replay drops its new entries as
// duplicates of the stale records.
func TestWALRecreatedStreamSurvivesFailedTruncation(t *testing.T) {
	dir := t.TempDir()
	i, stop := newDefinitionTestIngester(t, dir)
	definitionTestPushOther(t, i)
	definitionTestPush(t, i, 1)
	s := definitionTestPush(t, i, 2)
	definitionTestFlush(t, i, s)

	w := i.wal.(*walWrapper)
	writer := w.checkpointWriter()
	writer.truncate = func(int) error { return errInjectedCheckpoint }
	require.NoError(t, NewCheckpointer(time.Millisecond, w.seriesIter, writer, i.metrics, make(chan struct{})).PerformCheckpoint())

	fresh := definitionTestPush(t, i, 3)
	require.Equal(t, int64(3), fresh.entryCt)
	stop()

	recovered := definitionTestRecover(t, dir)
	inst, err := recovered.GetOrCreateInstance("wal-retention-test")
	require.NoError(t, err)
	got, ok := inst.streams.Load(definitionTestLabels)
	require.True(t, ok)
	require.Equal(t, int64(3), got.entryCt)
	// The stale segment's two entries plus the entry written after the checkpoint.
	require.Equal(t, 3, got.chunks[0].chunk.Size(), "the returning stream's entry must be recovered")
}
