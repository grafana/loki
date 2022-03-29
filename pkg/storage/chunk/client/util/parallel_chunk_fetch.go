package util

import (
	"context"
	"sync"

	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

var decodeContextPool = sync.Pool{
	New: func() interface{} {
		return encoding.NewDecodeContext()
	},
}

// GetParallelChunks fetches chunks in parallel (up to maxParallel).
func GetParallelChunks(ctx context.Context, maxParallel int, chunks []encoding.Chunk, f func(context.Context, *encoding.DecodeContext, encoding.Chunk) (encoding.Chunk, error)) ([]encoding.Chunk, error) {
	log, ctx := spanlogger.New(ctx, "GetParallelChunks")
	defer log.Finish()
	log.LogFields(otlog.Int("requested", len(chunks)))

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	queuedChunks := make(chan encoding.Chunk)

	go func() {
		for _, c := range chunks {
			queuedChunks <- c
		}
		close(queuedChunks)
	}()

	processedChunks := make(chan encoding.Chunk)
	errors := make(chan error)

	for i := 0; i < min(maxParallel, len(chunks)); i++ {
		go func() {
			decodeContext := decodeContextPool.Get().(*encoding.DecodeContext)
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

	result := make([]encoding.Chunk, 0, len(chunks))
	var lastErr error
	for i := 0; i < len(chunks); i++ {
		select {
		case chunk := <-processedChunks:
			result = append(result, chunk)
		case err := <-errors:
			lastErr = err
		}
	}

	log.LogFields(otlog.Int("fetched", len(result)))
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
