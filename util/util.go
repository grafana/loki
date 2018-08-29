package util

import (
	"bytes"
	"context"
	"strings"

	"github.com/weaveworks/cortex/pkg/chunk"
)

// DoSingleQuery is the interface for indexes that don't support batching yet.
type DoSingleQuery func(
	ctx context.Context, query chunk.IndexQuery,
	callback func(chunk.ReadBatch) bool,
) error

// DoParallelQueries translates between our interface for query batching,
// and indexes that don't yet support batching.
func DoParallelQueries(
	ctx context.Context, doSingleQuery DoSingleQuery, queries []chunk.IndexQuery,
	callback func(chunk.IndexQuery, chunk.ReadBatch) bool,
) error {
	incomingErrors := make(chan error)
	for _, query := range queries {
		go func(query chunk.IndexQuery) {
			incomingErrors <- doSingleQuery(ctx, query, func(r chunk.ReadBatch) bool {
				return callback(query, r)
			})
		}(query)
	}
	var lastErr error
	for i := 0; i < len(queries); i++ {
		err := <-incomingErrors
		if err != nil {

			lastErr = err
		}
	}
	return lastErr
}

type Callback func(chunk.IndexQuery, chunk.ReadBatch) bool

type readBatch []cell

func (b readBatch) Len() int                { return len(b) }
func (b readBatch) RangeValue(i int) []byte { return b[i].column }
func (b readBatch) Value(i int) []byte      { return b[i].value }

type cell struct {
	column []byte
	value  []byte
}

func QueryFilter(callback Callback) Callback {
	return func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		finalBatch := make(readBatch, 0, batch.Len())
		for i := 0; i < batch.Len(); i++ {
			rangeValue, value := batch.RangeValue(i), batch.Value(i)

			if len(query.RangeValuePrefix) != 0 && !strings.HasPrefix(string(rangeValue), string(query.RangeValuePrefix)) {
				continue
			}
			if len(query.RangeValueStart) != 0 && string(rangeValue) < string(query.RangeValueStart) {
				continue
			}
			if len(query.ValueEqual) != 0 && !bytes.Equal(value, query.ValueEqual) {
				continue
			}

			finalBatch = append(finalBatch, cell{column: rangeValue, value: value})
		}
		return callback(query, finalBatch)
	}
}
