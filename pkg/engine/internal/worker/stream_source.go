package worker

import (
	"context"
	"errors"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// streamSource handles incoming data for a stream, forwarding it to a bound
// [nodeSource] for processing.
type streamSource struct {
	// stateMut ensures that we don't call Bind and Close concurrently.
	stateMut   sync.Mutex
	nodeSource *nodeSource // Node source to forward data to.
	closed     chan struct{}
	bound      chan struct{}

	initOnce  sync.Once
	closeOnce sync.Once
}

// Write forwards a record to the bound [nodeSource]. Write blocks until a
// nodeSource is bound and accepts the write, or the provided context is
// canceled.
func (src *streamSource) Write(ctx context.Context, rec arrow.RecordBatch) error {
	src.lazyInit()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-src.closed:
		return wire.ErrConnClosed
	case <-src.bound:
	}

	return src.nodeSource.Write(ctx, rec)
}

func (src *streamSource) lazyInit() {
	src.initOnce.Do(func() {
		src.closed = make(chan struct{})
		src.bound = make(chan struct{})
	})
}

// Bind binds the streamSource to a nodeSource. Calls to Bind after the first
// will return an error.
func (src *streamSource) Bind(nodeSource *nodeSource) error {
	src.lazyInit()

	src.stateMut.Lock()
	defer src.stateMut.Unlock()

	// If the stream source was closed, don't permit binding.
	select {
	case <-src.closed:
		return wire.ErrConnClosed
	default:
	}

	if src.nodeSource != nil {
		return errors.New("stream already bound")
	}

	nodeSource.Add(1)
	src.nodeSource = nodeSource
	close(src.bound)
	return nil
}

// Close closes the source. All future Reads and Write calls will return
// [executor.EOF].
func (src *streamSource) Close() {
	src.lazyInit()

	src.stateMut.Lock()
	defer src.stateMut.Unlock()

	src.closeOnce.Do(func() {
		if src.nodeSource != nil {
			src.nodeSource.Add(-1) // Remove ourselves from the nodeSource.
		}

		close(src.closed)
	})
}
