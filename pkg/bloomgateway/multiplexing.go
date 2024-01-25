package bloomgateway

import (
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

const (
	Day = 24 * time.Hour
)

type tokenSettings struct {
	nGramLen int
}

// Task is the data structure that is enqueued to the internal queue and dequeued by query workers
type Task struct {
	// ID is a lexcographically sortable unique identifier of the task
	ID ulid.ULID
	// Tenant is the tenant ID
	Tenant string

	// ErrCh is a send-only channel to write an error to
	ErrCh chan error
	// ResCh is a send-only channel to write partial responses to
	ResCh chan v1.Output

	// series of the original request
	series []*logproto.GroupedChunkRefs
	// filters of the original request
	filters []syntax.LineFilter
	// from..through date of the task's chunks
	bounds model.Interval

	// TODO(chaudum): Investigate how to remove that.
	day model.Time
}

// NewTask returns a new Task that can be enqueued to the task queue.
// In addition, it returns a result and an error channel, as well
// as an error if the instantiation fails.
func NewTask(tenantID string, refs seriesWithBounds, filters []syntax.LineFilter) (Task, error) {
	key, err := ulid.New(ulid.Now(), nil)
	if err != nil {
		return Task{}, err
	}
	errCh := make(chan error, 1)
	resCh := make(chan v1.Output, len(refs.series))

	task := Task{
		ID:      key,
		Tenant:  tenantID,
		ErrCh:   errCh,
		ResCh:   resCh,
		filters: filters,
		series:  refs.series,
		bounds:  refs.bounds,
		day:     refs.day,
	}
	return task, nil
}

func (t Task) Bounds() (model.Time, model.Time) {
	return t.bounds.Start, t.bounds.End
}

// Copy returns a copy of the existing task but with a new slice of grouped chunk refs
func (t Task) Copy(series []*logproto.GroupedChunkRefs) Task {
	return Task{
		ID:      ulid.ULID{}, // create emty ID to distinguish it as copied task
		Tenant:  t.Tenant,
		ErrCh:   t.ErrCh,
		ResCh:   t.ResCh,
		filters: t.filters,
		series:  series,
		bounds:  t.bounds,
		day:     t.day,
	}
}

// taskMergeIterator implements v1.Iterator
type taskMergeIterator struct {
	curr      v1.Request
	heap      *v1.HeapIterator[v1.IndexedValue[*logproto.GroupedChunkRefs]]
	tasks     []Task
	day       model.Time
	tokenizer *v1.NGramTokenizer
	err       error
}

func newTaskMergeIterator(day model.Time, tokenizer *v1.NGramTokenizer, tasks ...Task) v1.PeekingIterator[v1.Request] {
	it := &taskMergeIterator{
		tasks:     tasks,
		curr:      v1.Request{},
		day:       day,
		tokenizer: tokenizer,
	}
	it.init()
	return v1.NewPeekingIter[v1.Request](it)
}

func (it *taskMergeIterator) init() {
	sequences := make([]v1.PeekingIterator[v1.IndexedValue[*logproto.GroupedChunkRefs]], 0, len(it.tasks))
	for i := range it.tasks {
		iter := v1.NewSliceIterWithIndex(it.tasks[i].series, i)
		sequences = append(sequences, iter)
	}
	it.heap = v1.NewHeapIterator(
		func(i, j v1.IndexedValue[*logproto.GroupedChunkRefs]) bool {
			return i.Value().Fingerprint < j.Value().Fingerprint
		},
		sequences...,
	)
	it.err = nil
}

func (it *taskMergeIterator) Next() bool {
	ok := it.heap.Next()
	if !ok {
		return false
	}

	group := it.heap.At()
	task := it.tasks[group.Index()]

	it.curr = v1.Request{
		Fp:       model.Fingerprint(group.Value().Fingerprint),
		Chks:     convertToChunkRefs(group.Value().Refs),
		Searches: convertToSearches(task.filters, it.tokenizer),
		Response: task.ResCh,
	}
	return true
}

func (it *taskMergeIterator) At() v1.Request {
	return it.curr
}

func (it *taskMergeIterator) Err() error {
	return it.err
}
