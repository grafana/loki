package bloomgateway

import (
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/oklog/ulid"
	"github.com/prometheus/common/model"
)

// Task is the data structure that is enqueued to the internal queue and queued by query workers
type Task struct {
	// ID is a lexcographically sortable unique identifier of the task
	ID ulid.ULID
	// Tenant is the tenant ID
	Tenant string
	// Request is the original request
	Request *logproto.FilterChunkRefRequest
	// ErrCh is a send-only channel to write an error to
	ErrCh chan<- error
	// ResCh is a send-only channel to write partial responses to
	ResCh chan<- v1.Output
}

// NewTask returns a new Task that can be enqueued to the task queue.
// As additional arguments, it returns a result and an error channel, as well
// as an error if the instantiation fails.
func NewTask(tenantID string, req *logproto.FilterChunkRefRequest) (Task, chan v1.Output, chan error, error) {
	key, err := ulid.New(ulid.Now(), nil)
	if err != nil {
		return Task{}, nil, nil, err
	}
	errCh := make(chan error, 1)
	resCh := make(chan v1.Output, 1)
	task := Task{
		ID:      key,
		Tenant:  tenantID,
		Request: req,
		ErrCh:   errCh,
		ResCh:   resCh,
	}
	return task, resCh, errCh, nil
}

// WithRequest returns a copy of the task, which holds the provided Request req.
func (t Task) WithRequest(req *logproto.FilterChunkRefRequest) Task {
	return Task{
		ID:      t.ID,
		Tenant:  t.Tenant,
		Request: req,
		ErrCh:   t.ErrCh,
		ResCh:   t.ResCh,
	}
}

// FilterRequest extends v1.Request with an error channel
type FilterRequest struct {
	v1.Request
	Error chan<- error
}

// taskMergeIterator implements v1.Iterator
type taskMergeIterator struct {
	curr  FilterRequest
	heap  *v1.HeapIterator[*logproto.GroupedChunkRefs]
	tasks []Task
}

func newTaskMergeIterator(tasks ...Task) *taskMergeIterator {
	it := &taskMergeIterator{
		tasks: tasks,
		curr:  FilterRequest{},
	}
	it.init()
	return it
}

func (it *taskMergeIterator) init() {
	sequences := make([]v1.PeekingIterator[*logproto.GroupedChunkRefs], 0, len(it.tasks))
	for i := range it.tasks {
		sequences = append(sequences, NewIterWithIndex(i, it.tasks[i].Request.Refs))
	}
	it.heap = v1.NewHeapIterator(
		func(i, j *logproto.GroupedChunkRefs) bool {
			return i.Fingerprint < j.Fingerprint
		},
		sequences...,
	)
}

func (it *taskMergeIterator) Reset() {
	it.init()
}

func (it *taskMergeIterator) Next() bool {
	ok := it.heap.Next()
	if !ok {
		return false
	}

	currIter, ok := it.heap.CurrIter().(*SliceIterWithIndex[*logproto.GroupedChunkRefs])
	if !ok {
		return false
	}
	iterIndex := currIter.Index()

	task := it.tasks[iterIndex]
	group := it.heap.At()

	it.curr.Fp = model.Fingerprint(group.Fingerprint)
	it.curr.Chks = convertToChunkRefs(group.Refs)
	it.curr.Searches = convertToSearches(task.Request.Filters)
	it.curr.Response = task.ResCh
	it.curr.Error = task.ErrCh
	return true
}

func (it *taskMergeIterator) At() FilterRequest {
	return it.curr
}

func (it *taskMergeIterator) Err() error {
	return nil
}
