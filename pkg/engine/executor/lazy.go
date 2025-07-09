package executor

import (
	"context"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
)

// lazyPipeline lazily fetches the next record from an underlying pipeline in
// the background.
type lazyPipeline struct {
	ctx    context.Context
	cancel context.CancelFunc
	inner  Pipeline

	once     sync.Once
	states   chan state
	curState state
}

var _ Pipeline = (*lazyPipeline)(nil)

func newLazyPipeline(ctx context.Context, inner Pipeline) *lazyPipeline {
	ctx, cancel := context.WithCancel(ctx)
	return &lazyPipeline{
		ctx:    ctx,
		cancel: cancel,
		inner:  inner,

		states: make(chan state, 1),
	}
}

func (lp *lazyPipeline) Read() error {
	lp.once.Do(func() { go lp.loop() })

	select {
	case <-lp.ctx.Done():
		return lp.ctx.Err()
	case s := <-lp.states:
		lp.curState = s
		return lp.curState.err
	}
}

func (lp *lazyPipeline) loop() {
	writeState := func(s state) {
		select {
		case <-lp.ctx.Done():
			return // give up
		case lp.states <- s:
		}
	}

	for {
		err := lp.inner.Read()
		if err != nil {
			writeState(failureState(err))
			return
		}

		batch, err := lp.inner.Value()
		writeState(newState(batch, err))

		if err != nil {
			return // Stop after the inner pipeline fails.
		}
	}
}

func (lp *lazyPipeline) Value() (arrow.Record, error) {
	return lp.curState.batch, lp.curState.err
}

func (lp *lazyPipeline) Close() {
	lp.inner.Close()
}

func (lp *lazyPipeline) Inputs() []Pipeline { return lp.inner.Inputs() }

func (lp *lazyPipeline) Transport() Transport { return lp.inner.Transport() }
