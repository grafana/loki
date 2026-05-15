package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
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

// drainCachedSources decodes pre-fetched cache buffers from srcs and writes
// the resulting records to input. Decode errors are propagated via
// [nodeSource.Fail]. When all buffers are exhausted, the goroutine's reference
// on input is released with [nodeSource.Add](-1).
//
// drainCachedSources is intended to run in a separate goroutine. The caller
// must have already called input.Add(1) before spawning.
func drainCachedSources(ctx context.Context, input *nodeSource, srcs workflow.CachedSources, logger log.Logger) {
	defer input.Add(-1)

	var (
		totalBatches int64
		totalRows    int64
		totalBytes   int64
	)

	for _, buf := range srcs {
		totalBytes += int64(len(buf))

		dec, err := executor.NewCacheEntryDecoder(buf)
		if err != nil {
			level.Error(logger).Log("msg", "cached source decode failed", "err", err)
			input.Fail(fmt.Errorf("cached source: decode failed: %w", err))
			return
		}
		for {
			rec, err := dec.Next()
			if errors.Is(err, executor.EOF) {
				break
			}
			if err != nil {
				level.Error(logger).Log("msg", "cached source decode failed", "err", err)
				input.Fail(fmt.Errorf("cached source: decode failed: %w", err))
				return
			}
			if rec == nil {
				continue
			}
			totalBatches++
			totalRows += rec.NumRows()
			if err := input.Write(ctx, rec); err != nil {
				// Context canceled or nodeSource closed — stop quietly.
				level.Error(logger).Log("msg", "cached source write interrupted", "err", err)
				return
			}
		}
	}

	region := xcap.RegionFromContext(ctx)
	region.Record(xcap.TaskCacheBatches.Observe(totalBatches))
	region.Record(xcap.TaskCacheRows.Observe(totalRows))
	region.Record(xcap.TaskCacheBytes.Observe(totalBytes))

	level.Debug(logger).Log(
		"msg", "cached sources exhausted",
		"batches", totalBatches,
		"rows", totalRows,
		"bytes", humanize.Bytes(uint64(totalBytes)),
	)
}
