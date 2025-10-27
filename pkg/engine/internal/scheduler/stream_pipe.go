package scheduler

import (
	"context"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

// A streamPipe connects data for a stream to a local listener (via
// [executor.Pipeline]). Stream data is received by the scheduler, and enqueued
// for reading by calls to [streamPipe.Write].
//
// streamPipes are used for final task results, for which the reading end is the
// scheduler rather than another task.
type streamPipe struct {
	closeOnce sync.Once

	closed  chan struct{}
	results chan arrow.Record
}

var _ executor.Pipeline = (*streamPipe)(nil)

// newStreamPipe creates a new streamPipe.
func newStreamPipe() *streamPipe {
	return &streamPipe{
		closed:  make(chan struct{}),
		results: make(chan arrow.Record),
	}
}

// Read returns the next record of the stream data. Blocks until results are
// available or until the provided ctx is canceled.
func (pipe *streamPipe) Read(ctx context.Context) (arrow.Record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pipe.closed:
		return nil, executor.EOF
	case rec := <-pipe.results:
		return rec, nil
	}
}

// Write writes a record to the read end of the pipe. Write blocks until the
// record has been read or the context is canceled.
func (pipe *streamPipe) Write(ctx context.Context, rec arrow.Record) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pipe.closed:
		return executor.EOF
	case pipe.results <- rec:
		return nil
	}
}

func (pipe *streamPipe) Close() {
	pipe.closeOnce.Do(func() {
		close(pipe.closed)
	})
}
