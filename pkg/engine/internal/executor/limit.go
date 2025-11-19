package executor

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/xcap"
)

func NewLimitPipeline(input Pipeline, skip, fetch uint32, region *xcap.Region) *GenericPipeline {
	// We gradually reduce offsetRemaining and limitRemaining as we process more records, as the
	// offsetRemaining and limitRemaining may cross record boundaries.
	var (
		offsetRemaining = int64(skip)
		limitRemaining  = int64(fetch)
	)

	return newGenericPipelineWithRegion(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		var length int64
		var start, end int64
		var batch arrow.RecordBatch
		var err error

		// We skip yielding zero-length batches while offsetRemainig > 0
		for length == 0 {
			// Stop once we reached the limit
			if limitRemaining <= 0 {
				return nil, EOF
			}

			// Pull the next item from input
			input := inputs[0]
			batch, err = input.Read(ctx)
			if err != nil {
				return nil, err
			}

			// We want to slice batch so it only contains the rows we're looking for
			// accounting for both the limit and offset.
			// We constrain the start and end to be within the bounds of the record.
			start = min(offsetRemaining, batch.NumRows())
			end = min(start+limitRemaining, batch.NumRows())
			length = end - start

			offsetRemaining -= start
			limitRemaining -= length
		}

		if length <= 0 && offsetRemaining <= 0 {
			return nil, EOF
		}

		if batch.NumRows() == 0 {
			return batch, nil
		}

		return batch.NewSlice(start, end), nil
	}, region, input)
}
