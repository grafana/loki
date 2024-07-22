package bloomgateway

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/common/model"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

const (
	Day = 24 * time.Hour
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
	// tenant is the tenant ID
	tenant string

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
	// blocks that were resolved on the index gateway and sent with the request
	blocks []bloomshipper.BlockRef
	// from..through date of the task's chunks
	interval bloomshipper.Interval
	// the context from the request
	ctx context.Context

	// TODO(chaudum): Investigate how to remove that.
	table config.DayTime

	// log enqueue time so we can observe the time spent in the queue
	enqueueTime time.Time

	// recorder
	recorder *v1.BloomRecorder
}

func newTask(ctx context.Context, tenantID string, refs seriesWithInterval, filters []syntax.LineFilterExpr, blocks []bloomshipper.BlockRef) Task {
	return Task{
		tenant:   tenantID,
		recorder: v1.NewBloomRecorder(ctx, "task"),
		err:      new(wrappedError),
		resCh:    make(chan v1.Output),
		filters:  filters,
		blocks:   blocks,
		series:   refs.series,
		interval: refs.interval,
		table:    refs.day,
		ctx:      ctx,
		done:     make(chan struct{}),
	}
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
	return Task{
		recorder: t.recorder,
		tenant:   t.tenant,
		err:      t.err,
		resCh:    t.resCh,
		filters:  t.filters,
		blocks:   t.blocks,
		series:   series,
		interval: t.interval,
		table:    t.table,
		ctx:      t.ctx,
		done:     t.done,
	}
}

func (t Task) RequestIter(
	tokenizer *v1.NGramTokenizer,
) iter.Iterator[v1.Request] {
	return &requestIterator{
		recorder: t.recorder,
		series:   iter.NewSliceIter(t.series),
		search:   v1.FiltersToBloomTest(tokenizer, t.filters...),
		channel:  t.resCh,
		curr:     v1.Request{},
	}
}

var _ iter.Iterator[v1.Request] = &requestIterator{}

type requestIterator struct {
	recorder *v1.BloomRecorder
	series   iter.Iterator[*logproto.GroupedChunkRefs]
	search   v1.BloomTest
	channel  chan<- v1.Output
	curr     v1.Request
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
		Recorder: it.recorder,
		Fp:       model.Fingerprint(group.Fingerprint),
		Chks:     convertToChunkRefs(group.Refs),
		Search:   it.search,
		Response: it.channel,
	}
	return true
}
