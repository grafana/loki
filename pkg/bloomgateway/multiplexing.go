package bloomgateway

import (
	"context"
	"math/rand"
	"sort"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

const (
	Day = 24 * time.Hour
)

var (
	entropy = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type tokenSettings struct {
	nGramLen int
}

// ChanPipe is a pipe for channels
// It consists of a chan for sending and a chan for receiving.
// Items sent to the chan for sending are forwarded to the chan for reiving.
// In order to clean up the goroutine that does the forwardning
// the chan for sending needs to be closed when finished.
type ChanPipe[T any] struct {
	snd chan T
	rcv chan T
}

// Snd returns the channel for sending to the pipe.
func (ch ChanPipe[T]) Snd() chan<- T {
	return ch.snd
}

// Rcv returns the channel for reiving from the pipe.
func (ch ChanPipe[T]) Rcv() <-chan T {
	return ch.rcv
}

// The sender needs to close the channel once it's done sending to it.
// Otherwise the forwarder in the gorouting never exits.
func (ch ChanPipe[T]) forward(ctx context.Context) {
	for o := range ch.snd {
		if ctx.Err() == nil {
			ch.rcv <- o
		}
	}
}

// NewChanPipe returns a new pipe of channels.
// The sender is responsible for closing the sender channel.
func NewChanPipe[T any](ctx context.Context, n int) ChanPipe[T] {
	ch := ChanPipe[T]{
		snd: make(chan T, n),
		rcv: make(chan T, n),
	}
	go ch.forward(ctx)
	return ch
}

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

	// closed indicates whether the channels of this task are already closed
	// this is needed because copied tasks share the same channels
	// and closing an already closed channel results in a panic
	closed *atomic.Bool

	// ctx is the task's context
	ctx      context.Context
	cancelFn context.CancelFunc
}

// NewTask returns a new Task that can be enqueued to the task queue.
// In addition, it returns a result and an error channel, as well
// as an error if the instantiation fails.
func NewTask(ctx context.Context, tenantID string, req *logproto.FilterChunkRefRequest) (Task, <-chan v1.Output, <-chan error, error) {
	key, err := ulid.New(ulid.Now(), entropy)

	if err != nil {
		return Task{}, nil, nil, err
	}

	ctx, cancelFn := context.WithCancel(ctx)
	errPipe := NewChanPipe[error](ctx, 1)
	resPipe := NewChanPipe[v1.Output](ctx, 1)

	task := Task{
		ID:       key,
		Tenant:   tenantID,
		Request:  req,
		ErrCh:    errPipe.Snd(),
		ResCh:    resPipe.Snd(),
		ctx:      ctx,
		cancelFn: cancelFn,
		closed:   atomic.NewBool(false),
	}

	return task, resPipe.Rcv(), errPipe.Rcv(), nil
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
		ErrCh:  t.ErrCh,
		ResCh:  t.ResCh,
		ctx:    t.ctx,
		closed: t.closed,
	}
}

// Bounds returns the day boundaries of the task
func (t Task) Bounds() (time.Time, time.Time) {
	return getDayTime(t.Request.From), getDayTime(t.Request.Through)
}

func (t Task) Err() error {
	return t.ctx.Err()
}

func (t Task) Cancel() {
	t.cancelFn()
}

func (t Task) Close() {
	// Multiple tasks can share the same ErrCh and ResCh.
	// This is the case when a task initially spans across mutliple days and is
	// split up into multiple tasks each only spanning one respective day.
	if t.closed.CompareAndSwap(false, true) {
		close(t.ErrCh)
		close(t.ResCh)
	}
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

// taskMergeIterator implements v1.Iterator
type taskMergeIterator struct {
	curr      v1.Request
	heap      *v1.HeapIterator[v1.IndexedValue[*logproto.GroupedChunkRefs]]
	tasks     []Task
	day       time.Time
	tokenizer *v1.NGramTokenizer
	err       error
}

func newTaskMergeIterator(day time.Time, tokenizer *v1.NGramTokenizer, tasks ...Task) v1.PeekingIterator[v1.Request] {
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

	it.curr = v1.Request{
		Fp:       model.Fingerprint(group.Value().Fingerprint),
		Chks:     convertToChunkRefs(group.Value().Refs),
		Searches: convertToSearches(task.Request.Filters, it.tokenizer),
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
