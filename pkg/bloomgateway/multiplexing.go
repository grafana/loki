package bloomgateway

import (
	"sort"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

const (
	Day = 24 * time.Hour
)

// Task is the data structure that is enqueued to the internal queue and dequeued by query workers
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
// In addition, it returns a result and an error channel, as well
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

// Copy returns a copy of the existing task but with a new slice of chunks
func (t Task) Copy(refs []*logproto.GroupedChunkRefs) Task {
	return Task{
		ID:     t.ID,
		Tenant: t.Tenant,
		Request: &logproto.FilterChunkRefRequest{
			From:    t.Request.From,
			Through: t.Request.Through,
			Filters: t.Request.Filters,
			Refs:    refs,
		},
		ErrCh: t.ErrCh,
		ResCh: t.ResCh,
	}
}

// Bounds returns the day boundaries of the task
func (t Task) Bounds() (time.Time, time.Time) {
	return getDayTime(t.Request.From), getDayTime(t.Request.Through)
}

func (t Task) ChunkIterForDay(day time.Time) v1.Iterator[*logproto.GroupedChunkRefs] {
	cf := filterGroupedChunkRefsByDay{day: day}
	return &FilterIter[*logproto.GroupedChunkRefs]{
		iter:      v1.NewSliceIter(t.Request.Refs),
		matches:   cf.contains,
		transform: cf.filter,
	}
}

type filterGroupedChunkRefsByDay struct {
	day time.Time
}

func (cf filterGroupedChunkRefsByDay) contains(a *logproto.GroupedChunkRefs) bool {
	from, through := getFromThrough(a.Refs)
	if from.Time().After(cf.day.Add(Day)) || through.Time().Before(cf.day) {
		return false
	}
	return true
}

func (cf filterGroupedChunkRefsByDay) filter(a *logproto.GroupedChunkRefs) *logproto.GroupedChunkRefs {
	minTs, maxTs := getFromThrough(a.Refs)

	// in most cases, all chunks are within day range
	if minTs.Time().Compare(cf.day) >= 0 && maxTs.Time().Before(cf.day.Add(Day)) {
		return a
	}

	// case where certain chunks are outside of day range
	// using binary search to get min and max index of chunks that fall into the day range
	min := sort.Search(len(a.Refs), func(i int) bool {
		start := a.Refs[i].From.Time()
		end := a.Refs[i].Through.Time()
		return start.Compare(cf.day) >= 0 || end.Compare(cf.day) >= 0
	})

	max := sort.Search(len(a.Refs), func(i int) bool {
		start := a.Refs[i].From.Time()
		return start.Compare(cf.day.Add(Day)) > 0
	})

	return &logproto.GroupedChunkRefs{
		Tenant:      a.Tenant,
		Fingerprint: a.Fingerprint,
		Refs:        a.Refs[min:max],
	}
}

type Predicate[T any] func(a T) bool
type Transform[T any] func(a T) T

type FilterIter[T any] struct {
	iter      v1.Iterator[T]
	matches   Predicate[T]
	transform Transform[T]
	cache     T
	zero      T // zero value of the return type of Next()
}

func (it *FilterIter[T]) Next() bool {
	next := it.iter.Next()
	if !next {
		it.cache = it.zero
		return false
	}
	for next && !it.matches(it.iter.At()) {
		next = it.iter.Next()
		if !next {
			it.cache = it.zero
			return false
		}
	}
	it.cache = it.transform(it.iter.At())
	return true
}

func (it *FilterIter[T]) At() T {
	return it.cache
}

func (it *FilterIter[T]) Err() error {
	return nil
}

// FilterRequest extends v1.Request with an error channel
type FilterRequest struct {
	v1.Request
	Error chan<- error
}

// taskMergeIterator implements v1.Iterator
type taskMergeIterator struct {
	curr  FilterRequest
	heap  *v1.HeapIterator[v1.IndexedValue[*logproto.GroupedChunkRefs]]
	tasks []Task
	day   time.Time
	err   error
}

func newTaskMergeIterator(day time.Time, tasks ...Task) v1.PeekingIterator[v1.Request] {
	it := &taskMergeIterator{
		tasks: tasks,
		curr:  FilterRequest{},
		day:   day,
	}
	it.init()
	return v1.NewPeekingIter[v1.Request](it)
}

func (it *taskMergeIterator) init() {
	sequences := make([]v1.PeekingIterator[v1.IndexedValue[*logproto.GroupedChunkRefs]], 0, len(it.tasks))
	for i := range it.tasks {
		iter := v1.NewIterWithIndex(it.tasks[i].ChunkIterForDay(it.day), i)
		sequences = append(sequences, v1.NewPeekingIter(iter))
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

	it.curr.Fp = model.Fingerprint(group.Value().Fingerprint)
	it.curr.Chks = convertToChunkRefs(group.Value().Refs)
	it.curr.Searches = convertToSearches(task.Request.Filters)
	it.curr.Response = task.ResCh
	it.curr.Error = task.ErrCh
	return true
}

func (it *taskMergeIterator) At() v1.Request {
	return it.curr.Request
}

func (it *taskMergeIterator) Err() error {
	return it.err
}
