package dataobjread

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
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

// objectStreams is the decoded identity of the streams one object holds for a query.
//
// One instance is shared by every task planned for that object. newObjectStreams fills it and
// nothing writes to it afterwards, so the concurrent scans need no lock and the planner does
// not copy a label map per section. It does pin one label set per admitted stream until the
// query releases its objects.
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

	// streams is the identity of every stream of this object, shared with its other tasks.
	streams *objectStreams

	columns       []logs.ColumnType
	metadataNames []string
	predicates    []logs.RowPredicate

	start, end time.Time
}

// rowPredicates returns the predicates the reader applies: the half-open window [start, end)
// first, then the pushed-down metadata predicates.
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

// records turns one section read's rows into forwardable records, attaching each stream's
// identity and copying each line.
//
// It fails on a row whose stream the task did not plan to read. The read matches only the
// task's streams, so an unexpected ID means that broke, and dropping the row would under-count
// the query without saying so.
func (t ReadTask) records(rows []logs.Record) ([]LogRecord, error) {
	out := make([]LogRecord, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		stream, ok := t.streams.admittedByID[row.StreamID]
		if !ok {
			return nil, fmt.Errorf("data object %q logs section %d returned unexpected stream ID %d", t.objectPath, t.sectionIdx, row.StreamID)
		}
		out = append(out, LogRecord{
			streamHash:   stream.streamHash,
			streamLabels: stream.streamLabels,
			timestamp:    row.Timestamp.UnixNano(),
			// Copy the line, because DecodeRow reuses one buffer for it across reads.
			//
			// Metadata needs no copy. The symbolizer clones every string it interns and
			// the labels builder allocates its result, so forwarded metadata outlives
			// the reader safely.
			line:     append([]byte(nil), row.Line...),
			metadata: row.Metadata,
		})
	}
	return out, nil
}

// TaskIterator streams read tasks from the planner to the reader. The planner fills it
// from a background goroutine as it resolves objects, and the reader consumes tasks as they
// arrive. Err returns any resolution error once Next has returned false, so a planning failure
// reaches the reader the same way a scan failure does.
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

	errMu sync.Mutex
	err   error
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
	// A resolution error is terminal.
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
	it.errMu.Lock()
	defer it.errMu.Unlock()
	return it.err
}

// setErr keeps err as the planning failure, unless an earlier one already is. A cancellation
// counts: only the reader knows whether it caused one, so only the reader can discount it.
func (it *TaskIterator) setErr(err error) {
	it.errMu.Lock()
	defer it.errMu.Unlock()

	if it.err == nil {
		it.err = err
	}
}

// Abort records err when non-nil, stops the background planner, and waits for it to exit. After
// Abort returns the planner no longer touches the shared objects, so the caller may release
// them. Abort is idempotent and safe to call after a normal drain, where cancel does nothing
// and done is already closed.
//
// Cancelling the planner makes it report a cancellation of its own, which Err then returns.
// Only the caller knows it aborted, so only the caller can tell that apart from a query the
// caller's own context ended.
func (it *TaskIterator) Abort(err error) {
	if err != nil {
		it.setErr(err)
	}

	it.cancel()
	<-it.done
}
