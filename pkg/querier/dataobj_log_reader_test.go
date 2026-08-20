package querier

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/goleak"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// newTestLogReaderBatches builds a dataObjLogReader that yields the given batches then reports err,
// without opening a real data object. The batches are pre-buffered and the channel is closed, so no
// scan goroutine runs and Next walks the buffer before reporting EOF.
func newTestLogReaderBatches(batches [][]dataObjLogRecord, err error) *dataObjLogReader {
	ch := make(chan []dataObjLogRecord, len(batches))
	for _, b := range batches {
		ch <- b
	}
	close(ch)

	stopped := make(chan struct{})
	close(stopped)

	return &dataObjLogReader{
		cache:       newDataObjCache(nil, "test"),
		nextBatches: ch,
		stopped:     stopped,
		cancel:      func() {},
		err:         err,
	}
}

// TestDataObjLogReader_MultiObject drives the planner + log reader over two objects where one stream
// spans both, and asserts every matching line is yielded exactly once with the right fingerprint and
// timestamp. Order is not asserted (the reader is unordered). Run under -race to exercise the
// concurrent per-section fan-in.
func TestDataObjLogReader_MultiObject(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)

	a := labels.FromStrings("job", "t", "app", "a")
	b := labels.FromStrings("job", "t", "app", "b")
	c := labels.FromStrings("job", "t", "app", "c")

	at := func(sec int64) time.Time { return time.Unix(sec, 0) }

	bucket := objstore.NewInMemBucket()
	// Stream a spans both objects; b only in obj1; c only in obj2.
	ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{
		{
			{Labels: a.String(), Entries: []push.Entry{{Timestamp: at(1), Line: "a1"}, {Timestamp: at(3), Line: "a3"}}},
			{Labels: b.String(), Entries: []push.Entry{{Timestamp: at(2), Line: "b2"}}},
		},
		{
			{Labels: a.String(), Entries: []push.Entry{{Timestamp: at(5), Line: "a5"}}},
			{Labels: c.String(), Entries: []push.Entry{{Timestamp: at(6), Line: "c6"}, {Timestamp: at(7), Line: "c7"}}},
		},
	})

	cache := newDataObjCache(bucket, dataObjTestTenant)
	expr, err := syntax.ParseSampleExpr(`count_over_time({job="t"}[1h])`)
	require.NoError(t, err)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "job", "t")}
	tasks := newDataObjReadPlanner(metastoreSectionsResolver{ms: ms}, cache, false, false).plan(ctx, at(0), at(100), matchers, nil, expr)

	// A small batch size forces multiple batches per section, exercising the batch boundary.
	reader := newDataObjAbortReader(newDataObjLogReader(ctx, cache, tasks, defaultMaxConcurrency, 2, nil), tasks)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	type key struct {
		fp uint64
		ts int64
	}
	got := map[key]int{}
	for reader.Next() {
		r := reader.At()
		got[key{r.fingerprint, r.timestamp}]++
	}
	require.NoError(t, reader.Err())

	fp := labels.StableHash
	want := map[key]int{
		{fp(a), at(1).UnixNano()}: 1,
		{fp(a), at(3).UnixNano()}: 1,
		{fp(a), at(5).UnixNano()}: 1,
		{fp(b), at(2).UnixNano()}: 1,
		{fp(c), at(6).UnixNano()}: 1,
		{fp(c), at(7).UnixNano()}: 1,
	}
	require.Equal(t, want, got, "every matching line must be yielded exactly once, with its fingerprint")
}

// TestDataObjLogReader_ConcurrentSections reads one object split into many logs sections with the
// default concurrency, so several section-read goroutines share and lazily open sections on the same
// cached openObject at once. Run under -race: without a lock on openObject the shared logsSec map races
// (a fatal concurrent map write in production). It also asserts every record is yielded exactly once.
func TestDataObjLogReader_ConcurrentSections(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)

	at := func(sec int64) time.Time { return time.Unix(sec, 0) }
	apps := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	objStreams := make([]logproto.Stream, 0, len(apps))
	for i, app := range apps {
		lbls := labels.FromStrings("job", "t", "app", app)
		objStreams = append(objStreams, logproto.Stream{
			Labels:  lbls.String(),
			Entries: []push.Entry{{Timestamp: at(int64(i + 1)), Line: app}},
		})
	}

	bucket := objstore.NewInMemBucket()
	// A section size of 1 byte forces one logs section per stream, so the single object splits into
	// several concurrently-read sections.
	ms := newTestDataObjMetastore(ctx, t, bucket, 1, [][]logproto.Stream{objStreams})

	cache := newDataObjCache(bucket, dataObjTestTenant)
	expr, err := syntax.ParseSampleExpr(`count_over_time({job="t"}[1h])`)
	require.NoError(t, err)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "job", "t")}
	// Drain the plan first for the section-count assertions, then re-wrap it as a slice iterator so the
	// reader consumes every task concurrently against the one shared openObject (the race under test).
	tasks := drainTaskIterator(t, newDataObjReadPlanner(metastoreSectionsResolver{ms: ms}, cache, false, false).plan(ctx, at(0), at(100), matchers, nil, expr))
	require.Greater(t, len(tasks), 1, "a tiny section size must split the object into several sections")
	for _, task := range tasks {
		require.Equal(t, tasks[0].object, task.object, "every task must read the same object")
	}

	sliceIt := newSliceTaskIterator(tasks)
	reader := newDataObjAbortReader(newDataObjLogReader(ctx, cache, sliceIt, defaultMaxConcurrency, defaultReadBatchSize, nil), sliceIt)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	// A count_over_time projects stream_id + timestamp (not the message), so assert on the per-stream
	// timestamps rather than the line: each stream's distinct timestamp must appear exactly once.
	got := map[int64]int{}
	for reader.Next() {
		got[reader.At().timestamp]++
	}
	require.NoError(t, reader.Err())

	want := map[int64]int{}
	for i := range apps {
		want[at(int64(i+1)).UnixNano()] = 1
	}
	require.Equal(t, want, got, "every record must be yielded exactly once across the concurrent sections")
}

func TestDataObjLogReader_Batches(t *testing.T) {
	a := labels.FromStrings("app", "a")
	rec := func(ts int64) dataObjLogRecord { return testLogRecord(1, a, ts, "line") }

	tests := map[string]struct {
		batches [][]dataObjLogRecord
		want    []int64 // record timestamps, in emission order
	}{
		"no batches": {
			batches: nil,
			want:    nil,
		},
		"single batch": {
			batches: [][]dataObjLogRecord{{rec(1), rec(2)}},
			want:    []int64{1, 2},
		},
		"multiple batches walk in order": {
			batches: [][]dataObjLogRecord{{rec(1)}, {rec(2), rec(3)}},
			want:    []int64{1, 2, 3},
		},
		"empty batches are skipped": {
			batches: [][]dataObjLogRecord{{}, {rec(1)}, {}, {rec(2)}, {}},
			want:    []int64{1, 2},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestLogReaderBatches(tc.batches, nil)
			var got []int64
			for reader.Next() {
				got = append(got, reader.At().timestamp)
			}
			require.Equal(t, tc.want, got)
			require.NoError(t, reader.Err())
		})
	}
}

func TestDataObjLogReader_Err(t *testing.T) {
	wantErr := errors.New("scan failed")
	batch := []dataObjLogRecord{testLogRecord(1, labels.FromStrings("app", "a"), 1, "line")}
	reader := newTestLogReaderBatches([][]dataObjLogRecord{batch}, wantErr)

	var n int
	for reader.Next() {
		n++
	}
	require.Zero(t, n, "a recorded error stops iteration without draining the queued batch")
	require.ErrorIs(t, reader.Err(), wantErr)
}

// TestDataObjLogReader_UnexpectedStreamID drives a real section scan whose task knows only one of the
// two streams it reads, and asserts the unexpected stream surfaces as an error through reader.Err()
// rather than being silently dropped.
func TestDataObjLogReader_UnexpectedStreamID(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)
	a := labels.FromStrings("app", "a")
	b := labels.FromStrings("app", "b")

	bucket := objstore.NewInMemBucket()
	ids := buildDataObject(ctx, t, bucket, "obj1", []logproto.Stream{
		{Labels: a.String(), Entries: []push.Entry{{Timestamp: time.Unix(1, 0), Line: "a1"}}},
		{Labels: b.String(), Entries: []push.Entry{{Timestamp: time.Unix(2, 0), Line: "b2"}}},
	})
	require.Len(t, ids, 2)

	// The task reads both streams (MatchStreams) but records the fingerprint of only the first.
	task := dataObjReadTask{
		object:           "obj1",
		section:          0,
		streamIDs:        []streamID{streamID(ids[0]), streamID(ids[1])},
		fingerprints:     map[streamID]uint64{streamID(ids[0]): 1},
		labels:           map[streamID]labels.Labels{streamID(ids[0]): a},
		projectedColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
		start:            time.Unix(0, 0),
		end:              time.Unix(100, 0),
	}

	it := newSliceTaskIterator([]dataObjReadTask{task})
	reader := newDataObjAbortReader(newDataObjLogReader(ctx, newDataObjCache(bucket, dataObjTestTenant), it, 1, defaultReadBatchSize, nil), it)
	// Close surfaces the scan error, which this test expects and asserts via reader.Err() below.
	t.Cleanup(func() { _ = reader.Close() })

	for reader.Next() {
		_ = reader.At()
	}
	require.ErrorContains(t, reader.Err(), "unexpected stream ID")
}

// newSliceTaskIterator returns a dataObjTaskIterator that yields the given tasks then ends, with no
// planning error (a test sets one with setErr if needed). It stands in for plan when a test supplies
// tasks directly.
func newSliceTaskIterator(tasks []dataObjReadTask) *dataObjTaskIterator {
	ch := make(chan dataObjReadTask, len(tasks))
	for _, t := range tasks {
		ch <- t
	}
	close(ch)
	it := newDataObjTaskIterator(ch, func() {})
	close(it.done) // no planner goroutine, so Abort's wait returns immediately
	return it
}

// drainTaskIterator collects every task the iterator yields and asserts it reported no error.
func drainTaskIterator(t *testing.T, it *dataObjTaskIterator) []dataObjReadTask {
	t.Helper()
	var tasks []dataObjReadTask
	for it.Next() {
		tasks = append(tasks, it.At())
	}
	require.NoError(t, it.Err())
	return tasks
}

// TestDataObjTaskIterator checks the iterator yields its tasks in order and surfaces a preset error.
func TestDataObjTaskIterator(t *testing.T) {
	a := dataObjReadTask{object: "a"}
	b := dataObjReadTask{object: "b"}

	it := newSliceTaskIterator([]dataObjReadTask{a, b})
	require.True(t, it.Next())
	require.Equal(t, "a", it.At().object)
	require.True(t, it.Next())
	require.Equal(t, "b", it.At().object)
	require.False(t, it.Next())
	require.NoError(t, it.Err())

	wantErr := errors.New("boom")
	errIt := newSliceTaskIterator(nil)
	errIt.setErr(wantErr)
	require.False(t, errIt.Next())
	require.ErrorIs(t, errIt.Err(), wantErr)

	// A set error stops iteration at once, even with tasks still buffered.
	bufferedErrIt := newSliceTaskIterator([]dataObjReadTask{a, b})
	bufferedErrIt.setErr(wantErr)
	require.False(t, bufferedErrIt.Next(), "a set error must stop iteration without yielding buffered tasks")
	require.ErrorIs(t, bufferedErrIt.Err(), wantErr)

	require.Panics(t, func() { newDataObjTaskIterator(nil, func() {}) }, "a nil tasks channel is a programming error")
}

// TestDataObjTaskIterator_AbortCancelsBlockedPlanner checks Abort against a planner blocked on a full
// buffer (modeled with an unbuffered channel and no consumer): Abort must cancel the planner so its send
// unblocks, and then wait for it to exit. Without the cancel or the send's ctx.Done branch, Abort hangs;
// without the wait, it would return before the planner released its resources.
func TestDataObjTaskIterator_AbortCancelsBlockedPlanner(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan dataObjReadTask) // unbuffered: with no consumer, the producer blocks on send
	it := newDataObjTaskIterator(ch, cancel)

	blocked := make(chan struct{})
	go func() {
		defer close(it.done)
		close(blocked)
		select {
		case ch <- dataObjReadTask{}:
		case <-ctx.Done():
		}
	}()
	<-blocked

	returned := make(chan struct{})
	go func() {
		it.Abort(errors.New("stop"))
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("Abort did not return: it must cancel a blocked planner and wait for it to exit")
	}

	select {
	case <-it.done:
	default:
		t.Fatal("Abort returned before the planner exited; it must wait on done")
	}
	require.ErrorContains(t, it.Err(), "stop")
}

// TestDataObjLogReader_PlanningError checks a resolution error carried by the task iterator surfaces
// through the reader's Err, so a planning failure fails the query rather than returning an empty result.
func TestDataObjLogReader_PlanningError(t *testing.T) {
	wantErr := errors.New("resolving data object sections: boom")

	it := newSliceTaskIterator(nil)
	it.setErr(wantErr)

	reader := newDataObjAbortReader(newDataObjLogReader(context.Background(), newDataObjCache(objstore.NewInMemBucket(), dataObjTestTenant), it, 1, 1, nil), it)
	t.Cleanup(func() { _ = reader.Close() })

	var n int
	for reader.Next() {
		n++
	}
	require.Zero(t, n, "a planning error yields no records")
	require.ErrorIs(t, reader.Err(), wantErr)
}

// TestDataObjLogReader_CloseBeforeDrain closes the reader after reading a single record, while a scan is
// still in flight, and asserts Close returns promptly (no deadlock) and leaks no goroutines. This is the
// production early-exit path (instant queries, LIMIT, client cancel).
func TestDataObjLogReader_CloseBeforeDrain(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)
	at := func(sec int64) time.Time { return time.Unix(sec, 0) }

	objStreams := make([]logproto.Stream, 0, 32)
	for i := 0; i < 32; i++ {
		app := fmt.Sprintf("app-%02d", i)
		objStreams = append(objStreams, logproto.Stream{
			Labels:  labels.FromStrings("job", "t", "app", app).String(),
			Entries: []push.Entry{{Timestamp: at(int64(i + 1)), Line: app}},
		})
	}
	bucket := objstore.NewInMemBucket()
	ms := newTestDataObjMetastore(ctx, t, bucket, 1, [][]logproto.Stream{objStreams}) // one section per stream

	cache := newDataObjCache(bucket, dataObjTestTenant)
	expr, err := syntax.ParseSampleExpr(`count_over_time({job="t"}[1h])`)
	require.NoError(t, err)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "job", "t")}
	tasks := newDataObjReadPlanner(metastoreSectionsResolver{ms: ms}, cache, false, false).plan(ctx, at(0), at(100), matchers, nil, expr)
	// maxConcurrency=1, batchSize=1: after the consumer reads one record and stops, the running scan
	// blocks on the batch channel, so Close must cancel it to unwind.
	reader := newDataObjAbortReader(newDataObjLogReader(ctx, cache, tasks, 1, 1, nil), tasks)

	require.True(t, reader.Next(), "the reader yields at least one record")

	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()
	select {
	case <-closed:
	case <-time.After(30 * time.Second):
		t.Fatal("Close did not return; a reader or planner goroutine is stuck")
	}
}

// TestDataObjLogReader_ResolutionErrorStopsPlanner drives a live planner over the real metastore whose
// resolved object was deleted from the bucket, so opening it fails. It asserts the error surfaces through
// the reader and that the planner goroutine does not leak (the abort reader stops it).
func TestDataObjLogReader_ResolutionErrorStopsPlanner(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)
	a := labels.FromStrings("app", "a")
	at := func(sec int64) time.Time { return time.Unix(sec, 0) }

	bucket := objstore.NewInMemBucket()
	ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{{
		{Labels: a.String(), Entries: []push.Entry{{Timestamp: at(1), Line: "a1"}}},
	}})

	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "a")}
	resp, err := ms.Sections(ctx, metastore.SectionsRequest{Start: at(0), End: at(100), Matchers: matchers})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Sections)

	// Delete the resolved data object; its index (which the metastore reads) is untouched, so resolution
	// starts and then fails opening the object while the planner goroutine is live.
	for _, sec := range resp.Sections {
		require.NoError(t, bucket.Delete(ctx, sec.ObjectPath))
	}

	cache := newDataObjCache(bucket, dataObjTestTenant)
	expr, err := syntax.ParseSampleExpr(`count_over_time({app="a"}[1h])`)
	require.NoError(t, err)
	tasks := newDataObjReadPlanner(metastoreSectionsResolver{ms: ms}, cache, false, false).plan(ctx, at(0), at(100), matchers, nil, expr)
	reader := newDataObjAbortReader(newDataObjLogReader(ctx, cache, tasks, 1, defaultReadBatchSize, nil), tasks)

	var n int
	for reader.Next() {
		n++
	}
	require.Zero(t, n, "no records when the object cannot be opened")
	require.Error(t, reader.Err(), "the deleted object must surface as a resolution error")
	_ = reader.Close() // returns the resolution error asserted above
}

// TestDataObjLogReader_EmptyResult drives a plan that yields no tasks and no error, asserting the reader
// yields nothing, reports no error, and closes cleanly — the path that replaced the removed
// NoopSampleIterator special case for a query that matches no sections.
func TestDataObjLogReader_EmptyResult(t *testing.T) {
	it := newSliceTaskIterator(nil)
	reader := newDataObjAbortReader(newDataObjLogReader(context.Background(), newDataObjCache(objstore.NewInMemBucket(), dataObjTestTenant), it, 1, 1, nil), it)

	var n int
	for reader.Next() {
		n++
	}
	require.Zero(t, n, "an empty plan yields no records")
	require.NoError(t, reader.Err())
	require.NoError(t, reader.Close())
}
