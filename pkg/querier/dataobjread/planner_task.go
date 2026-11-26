package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/util/labelpool"
)

const (
	// planBufferSize is how many planned tasks the planner may queue ahead of the reader.
	planBufferSize = 2 * DefaultMaxConcurrency
)

// objectStream is one admitted stream's decoded identity within an object.
type objectStream struct {
	// streamLabels are the log stream's own labels as the object stored them, without anything
	// a query pipeline derives.
	streamLabels labels.Labels

	// streamHash is labels.StableHash of streamLabels, a hash guaranteed not to change over
	// time.
	streamHash uint64
}

// objectStreams is the decoded identity of the streams that one planObject call reads from a
// data object.
//
// The tasks of that call share one instance. newObjectStreams fills it and nothing writes to it
// afterwards, so the concurrent scans need no lock and the planner does not copy a label map per
// section. It does pin one label set per admitted stream while a task of that call is alive.
//
// Two index objects can list sections of one data object. planObject then runs once for each of
// them, and a stream that both list has two copies of its labels.
//
// A dropped stream keeps its ID and loses its labels, which is what bounds that cost for a
// query whose access-control policy denies most of its streams. Its ID is still needed, because
// the planner has to tell a stream a filter dropped from a stream the object does not hold: the
// first is expected, the second is a broken invariant.
type objectStreams struct {
	// admittedByID holds the streams the read returned and the filters kept, by stream ID.
	admittedByID map[int64]objectStream

	// notAdmittedByID holds the IDs of the streams the filters dropped, without their labels.
	// It is disjoint from admittedByID.
	notAdmittedByID map[int64]struct{}
}

// newObjectStreams records the decoded streams, keeping the identity of the ones admit accepts
// and only the ID of the rest. It is the only way to build an objectStreams, so a stream can
// never carry a stream hash without its labels.
func newObjectStreams(decoded map[int64]labels.Labels, admit func(labels.Labels, uint64) bool) *objectStreams {
	streams := &objectStreams{
		admittedByID:    make(map[int64]objectStream, len(decoded)),
		notAdmittedByID: make(map[int64]struct{}),
	}
	for id, streamLabels := range decoded {
		streamHash := labels.StableHash(streamLabels)
		if !admit(streamLabels, streamHash) {
			streams.notAdmittedByID[id] = struct{}{}
			continue
		}
		streams.admittedByID[id] = objectStream{
			streamLabels: streamLabels,
			streamHash:   streamHash,
		}
	}
	return streams
}

func (s *objectStreams) admits(id int64) bool {
	_, ok := s.admittedByID[id]
	return ok
}

// decoded reports whether the streams read returned the stream at all, admitted or not.
func (s *objectStreams) decoded(id int64) bool {
	if s.admits(id) {
		return true
	}
	_, ok := s.notAdmittedByID[id]
	return ok
}

// ReadTask is the plan for reading one logs section.
type ReadTask struct {
	objectPath string

	// sectionIdx is the section's index among the logs sections of every tenant in the object,
	// not only this tenant's, so a tenant's sections are not numbered from zero. That is the
	// numbering the metastore's section descriptors use.
	sectionIdx int

	// streamIDs are the streams to read, after filtering.
	streamIDs []int64

	// streams is shared with the other tasks of the same planObject call.
	streams *objectStreams

	columns       []logs.ColumnType
	metadataNames []string
	predicates    []logs.RowPredicate

	// needReturningLine is true when the returned records should carry the line.
	needReturningLine bool

	start, end time.Time
}

// rowPredicates returns the predicates the reader applies: the half-open window [start, end)
// first, then the pushed-down metadata predicates and the message predicate.
//
// The window is half-open to match the chunk path, which drops a record whose timestamp is
// below start or at or above end. The engine compensates for that by adding a nanosecond to
// its own end, because its range-vector iterator treats the end as inclusive.
func (t ReadTask) rowPredicates() []logs.RowPredicate {
	predicates := make([]logs.RowPredicate, 0, 1+len(t.predicates))
	predicates = append(predicates, logs.TimeRangeRowPredicate{
		StartTime:    t.start,
		EndTime:      t.end,
		IncludeStart: true,
		IncludeEnd:   false,
	})
	return append(predicates, t.predicates...)
}

// forwardedLineBytes returns the number of line bytes that appendRecords copies for the rows of
// batch.
func (t ReadTask) forwardedLineBytes(batch *logs.RecordBatch) int {
	if !t.needReturningLine || batch.Message == nil {
		return 0
	}
	return batch.Message.DataLen()
}

// appendRecords appends one forwardable record per row of batch to dst, attaching each stream's
// identity. It returns the extended slice.
//
// A record's line is a copy of the message in the spare capacity of lineBuf, so it stays valid when
// the arrays of batch are reused. The line is nil when the task does not need it, or when the
// message is null or empty. The metadata labels own their strings, so the records do not alias
// batch.
//
// lineBuf must have room for [ReadTask.forwardedLineBytes] bytes. appendRecords fails when it has
// less, so a line never moves to memory that the caller does not own.
//
// It fails when batch has no stream ID array or no timestamp array. It also fails on a row whose
// stream the task did not plan to read. The read matches only the task's streams, so an unexpected
// ID means that broke, and dropping the row would under-count the query without saying so.
func (t ReadTask) appendRecords(dst []LogRecord, lineBuf []byte, batch *logs.RecordBatch) ([]LogRecord, error) {
	if batch.StreamID == nil || batch.Timestamp == nil {
		return nil, fmt.Errorf("data object %q logs section %d returned a batch without the stream ID or timestamp column", t.objectPath, t.sectionIdx)
	}

	scratch := labelpool.Get()
	defer labelpool.Put(scratch)

	var (
		// A section keeps the rows of one stream together, so remember the last stream and skip the
		// map lookup.
		lastStreamID int64
		lastStream   objectStream
		haveLast     bool
	)
	for i := range batch.Rows {
		streamID := batch.StreamID.Get(i)
		if !haveLast || streamID != lastStreamID {
			stream, ok := t.streams.admittedByID[streamID]
			if !ok {
				return nil, fmt.Errorf("data object %q logs section %d returned unexpected stream ID %d", t.objectPath, t.sectionIdx, streamID)
			}
			lastStreamID, lastStream, haveLast = streamID, stream, true
		}

		var line []byte
		if t.needReturningLine && batch.Message != nil {
			if value := batch.Message.Get(i); len(value) > 0 {
				if free := cap(lineBuf) - len(lineBuf); free < len(value) {
					return nil, fmt.Errorf("line buffer has %d free bytes, but the line of row %d needs %d", free, i, len(value))
				}
				start := len(lineBuf)
				lineBuf = append(lineBuf, value...)
				line = lineBuf[start:len(lineBuf):len(lineBuf)]
			}
		}

		dst = append(dst, LogRecord{
			streamHash:   lastStream.streamHash,
			streamLabels: lastStream.streamLabels,
			timestamp:    batch.Timestamp.Get(i),
			line:         line,
			metadata:     batch.MetadataLabels(i, scratch),
		})
	}
	return dst, nil
}

// TaskIterator streams read tasks from the planner to the reader. The planner fills it from a
// background goroutine as the metastore yields sections, and the reader consumes tasks as they
// arrive. Err returns any metastore or planning error once Next has returned false. The reader
// sees it the same way as a scan failure.
//
// Next, At and Waited are for the single consumer goroutine only. Err, setErr and Abort are
// safe for concurrent use: the reader may Abort while the planner still runs.
type TaskIterator struct {
	tasks <-chan ReadTask
	curr  ReadTask

	// waited accumulates the time each receive took. A receive that does not block still adds
	// a clock read's worth, so it is near zero rather than zero.
	waited time.Duration

	// cancel stops the background planner; done closes once that goroutine has exited.
	cancel context.CancelFunc
	done   chan struct{}

	errStoppedMu sync.Mutex
	err          error
	stopped      bool // the planner was cancelled deliberately, rather than failing
}

// newTaskIterator returns an iterator over tasks whose background planner cancel stops.
// It panics when tasks is nil, which is a programming error Next would otherwise turn into a
// deadlock.
func newTaskIterator(tasks <-chan ReadTask, cancel context.CancelFunc) *TaskIterator {
	if tasks == nil {
		panic("newTaskIterator: tasks channel must not be nil")
	}
	return &TaskIterator{
		tasks:  tasks,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (it *TaskIterator) Next() bool {
	// An error from the metastore or from planning is terminal.
	if it.Err() != nil {
		return false
	}

	start := time.Now()
	task, ok := <-it.tasks
	it.waited += time.Since(start)
	if !ok {
		return false
	}
	it.curr = task
	return true
}

func (it *TaskIterator) At() ReadTask { return it.curr }

// Waited returns how long Next blocked waiting for the planner. Call it once the iterator is
// drained.
func (it *TaskIterator) Waited() time.Duration { return it.waited }

func (it *TaskIterator) Err() error {
	it.errStoppedMu.Lock()
	defer it.errStoppedMu.Unlock()
	return it.err
}

func (it *TaskIterator) setErr(err error) {
	it.errStoppedMu.Lock()
	defer it.errStoppedMu.Unlock()
	if it.stopped && errors.Is(err, context.Canceled) {
		// Abort cancelled the planner, so this is the cancellation it was given rather than a
		// query failure. A cancellation recorded before Abort is kept: the caller's context
		// died on its own and the query did fail.
		return
	}
	if it.err == nil {
		it.err = err
	}
}

// Abort records err when non-nil, stops the background planner, and waits for it to exit. After
// Abort returns the planner no longer touches the shared objects, so the caller may release
// them. Abort is idempotent and safe to call after a normal drain, where cancel does nothing
// and done is already closed.
//
// A nil err means reading finished or stopped early rather than failed, so the cancellation
// this triggers in the planner is not reported. See [TaskIterator.Err].
func (it *TaskIterator) Abort(err error) {
	// Record before stopping. setErr drops a cancellation once stopped is set, and an explicit
	// argument is the caller reporting a failure, so it has to win over that guard.
	if err != nil {
		it.setErr(err)
	}

	it.errStoppedMu.Lock()
	it.stopped = true
	it.errStoppedMu.Unlock()

	it.cancel()
	<-it.done
}
