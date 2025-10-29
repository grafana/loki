package worker

import (
	"context"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

// nodeSource exposes data for a receiver of a stream as an [executor.Pipeline].
//
// Records are made available by a [nodeSource] calling [nodeSource.Write],
// after which each record can be read by the [nodeSource.Read] method.
type nodeSource struct {
	initOnce  sync.Once
	closeOnce sync.Once

	// streamCount is the number of streams that have been opened on this node
	// source.
	streamCount atomic.Int64

	closed  chan struct{}
	records chan arrow.Record
}

var _ executor.Pipeline = (*nodeSource)(nil)

// Read returns the next record of the node data. Blocks until results are
// available or until the provided ctx is canceled.
func (src *nodeSource) Read(ctx context.Context) (arrow.Record, error) {
	src.lazyInit()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-src.closed:
		return nil, executor.EOF
	case rec := <-src.records:
		return rec, nil
	}
}

func (src *nodeSource) lazyInit() {
	src.initOnce.Do(func() {
		src.closed = make(chan struct{})
		src.records = make(chan arrow.Record)
	})
}

// Write writes a record to the read end of the node source. Write blocks until
// the record has been read or the context is canceled.
func (src *nodeSource) Write(ctx context.Context, rec arrow.Record) error {
	src.lazyInit()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-src.closed:
		return executor.EOF
	case src.records <- rec:
		return nil
	}
}

// Add adds a delta, which may be negative, to the node source's input stream
// counter. If the counter becomes zero, the source is automatically closed. If
// the counter goes negative, Add panics.
func (src *nodeSource) Add(delta int64) {
	src.lazyInit()

	newValue := src.streamCount.Add(delta)
	if newValue == 0 {
		src.Close()
	} else if newValue < 0 {
		panic("negative stream count")
	}
}

// Close closes the source. All future Reads and Write calls will return
// [executor.EOF].
func (src *nodeSource) Close() {
	src.lazyInit()

	src.closeOnce.Do(func() { close(src.closed) })
}
