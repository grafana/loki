package executor

import "github.com/apache/arrow-go/v18/arrow"

func NewLimitPipeline(input Pipeline, skip, fetch uint32) *GenericPipeline {
	// We gradually reduce offsetRemaining and limitRemaining as we process more records, as the
	// offsetRemaining and limitRemaining may cross record boundaries.
	var (
		offsetRemaining = int64(skip)
		limitRemaining  = int64(fetch)
	)

	return newGenericPipeline(Local, func(inputs []Pipeline) state {
		var length int64
		var start, end int64
		var batch arrow.Record

		// We skip yielding zero-length batches while offsetRemainig > 0
		for length == 0 {
			// Stop once we reached the limit
			if limitRemaining <= 0 {
				return Exhausted
			}

			// Pull the next item from downstream
			input := inputs[0]
			err := input.Read()
			if err != nil {
				return failureState(err)
			}
			batch, _ = input.Value()

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
			return Exhausted
		}

		rec := batch.NewSlice(start, end)
		return successState(rec)
	}, input)
}
