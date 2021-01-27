package util

import (
	"context"
	"sync"

	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
)

const maxParallel = 1000

var decodeContextPool = sync.Pool{
	New: func() interface{} {
		return chunk.NewDecodeContext()
	},
}

// GetParallelChunks fetches chunks in parallel (up to maxParallel).
func GetParallelChunks(ctx context.Context, chunks []chunk.Chunk, f func(context.Context, *chunk.DecodeContext, chunk.Chunk) (chunk.Chunk, error)) ([]chunk.Chunk, error) {
	log, ctx := spanlogger.New(ctx, "GetParallelChunks")
	defer log.Finish()
	log.LogFields(otlog.Int("chunks requested", len(chunks)))

	queuedChunks := make(chan chunk.Chunk)

	go func() {
		for _, c := range chunks {
			queuedChunks <- c
		}
		close(queuedChunks)
	}()

	processedChunks := make(chan chunk.Chunk)
	errors := make(chan error)

	for i := 0; i < min(maxParallel, len(chunks)); i++ {
		go func() {
			decodeContext := decodeContextPool.Get().(*chunk.DecodeContext)
			for c := range queuedChunks {
				c, err := f(ctx, decodeContext, c)
				if err != nil {
					errors <- err
				} else {
					processedChunks <- c
				}
			}
			decodeContextPool.Put(decodeContext)
		}()
	}

	var result = make([]chunk.Chunk, 0, len(chunks))
	var lastErr error
	for i := 0; i < len(chunks); i++ {
		select {
		case chunk := <-processedChunks:
			result = append(result, chunk)
		case err := <-errors:
			lastErr = err
		}
	}

	log.LogFields(otlog.Int("chunks fetched", len(result)))
	if lastErr != nil {
		log.Error(lastErr)
	}

	// Return any chunks we did receive: a partial result may be useful
	return result, lastErr
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
