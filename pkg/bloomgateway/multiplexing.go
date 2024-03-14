package bloomgateway

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
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

type wrappedError struct {
	mu  sync.Mutex
	err error
}

func (e *wrappedError) Error() string {
	return e.err.Error()
}

func (e *wrappedError) Set(err error) {
	e.mu.Lock()
	e.err = err
	e.mu.Unlock()
}

// Task is the data structure that is enqueued to the internal queue and dequeued by query workers
type Task struct {
	// ID is a lexcographically sortable unique identifier of the task
	ID ulid.ULID
	// Tenant is the tenant ID
	Tenant string

	// channel to write partial responses to
	resCh chan v1.Output
	// channel to notify listener that the task is done
	done chan struct{}

	// the last error of the task
	// needs to be a pointer so multiple copies of the task can modify its value
	err *wrappedError
	// the responses received from the block queriers
	responses []v1.Output

	// series of the original request
	series []*logproto.GroupedChunkRefs
	// filters of the original request
	filters []syntax.LineFilterExpr
	// from..through date of the task's chunks
	interval bloomshipper.Interval
	// the context from the request
	ctx context.Context

	// TODO(chaudum): Investigate how to remove that.
	table config.DayTime

	// log enqueue time so we can observe the time spent in the queue
	enqueueTime time.Time
}

// NewTask returns a new Task that can be enqueued to the task queue.
// In addition, it returns a result and an error channel, as well
// as an error if the instantiation fails.
func NewTask(ctx context.Context, tenantID string, refs seriesWithInterval, filters []syntax.LineFilterExpr) (Task, error) {
	key, err := ulid.New(ulid.Now(), entropy)
	if err != nil {
		return Task{}, err
	}

	task := Task{
		ID:       key,
		Tenant:   tenantID,
		err:      new(wrappedError),
		resCh:    make(chan v1.Output),
		filters:  filters,
		series:   refs.series,
		interval: refs.interval,
		table:    refs.day,
		ctx:      ctx,
		done:     make(chan struct{}),
	}
	return task, nil
}

// Bounds implements Bounded
// see pkg/storage/stores/shipper/indexshipper/tsdb.Bounded
func (t Task) Bounds() (model.Time, model.Time) {
	return t.interval.Start, t.interval.End
}

func (t Task) Done() <-chan struct{} {
	return t.done
}

func (t Task) Err() error {
	return t.err.err
}

func (t Task) Close() {
	close(t.resCh)
	close(t.done)
}

func (t Task) CloseWithError(err error) {
	t.err.Set(err)
	t.Close()
}

// Copy returns a copy of the existing task but with a new slice of grouped chunk refs
func (t Task) Copy(series []*logproto.GroupedChunkRefs) Task {
	// do not copy ID to distinguish it as copied task
	return Task{
		Tenant:   t.Tenant,
		err:      t.err,
		resCh:    t.resCh,
		filters:  t.filters,
		series:   series,
		interval: t.interval,
		table:    t.table,
		ctx:      t.ctx,
		done:     make(chan struct{}),
	}
}

func (t Task) RequestIter(tokenizer *v1.NGramTokenizer) v1.Iterator[v1.Request] {
	return &requestIterator{
		series:  v1.NewSliceIter(t.series),
		search:  v1.FiltersToBloomTest(tokenizer, t.filters...),
		channel: t.resCh,
		curr:    v1.Request{},
	}
}

var _ v1.Iterator[v1.Request] = &requestIterator{}

type requestIterator struct {
	series  v1.Iterator[*logproto.GroupedChunkRefs]
	search  v1.BloomTest
	channel chan<- v1.Output
	curr    v1.Request
}

// At implements v1.Iterator.
func (it *requestIterator) At() v1.Request {
	return it.curr
}

// Err implements v1.Iterator.
func (it *requestIterator) Err() error {
	return nil
}

// Next implements v1.Iterator.
func (it *requestIterator) Next() bool {
	ok := it.series.Next()
	if !ok {
		return false
	}
	group := it.series.At()
	it.curr = v1.Request{
		Fp:       model.Fingerprint(group.Fingerprint),
		Chks:     convertToChunkRefs(group.Refs),
		Search:   it.search,
		Response: it.channel,
	}
	return true
}
