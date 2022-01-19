package batch

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	promchunk "github.com/grafana/loki/pkg/storage/chunk/encoding"
)

func TestStream(t *testing.T) {
	for i, tc := range []struct {
		input1, input2 []promchunk.Batch
		output         batchStream
	}{
		{
			input1: []promchunk.Batch{mkBatch(0)},
			output: []promchunk.Batch{mkBatch(0)},
		},

		{
			input1: []promchunk.Batch{mkBatch(0)},
			input2: []promchunk.Batch{mkBatch(0)},
			output: []promchunk.Batch{mkBatch(0)},
		},

		{
			input1: []promchunk.Batch{mkBatch(0)},
			input2: []promchunk.Batch{mkBatch(promchunk.BatchSize)},
			output: []promchunk.Batch{mkBatch(0), mkBatch(promchunk.BatchSize)},
		},

		{
			input1: []promchunk.Batch{mkBatch(0), mkBatch(promchunk.BatchSize)},
			input2: []promchunk.Batch{mkBatch(promchunk.BatchSize / 2), mkBatch(2 * promchunk.BatchSize)},
			output: []promchunk.Batch{mkBatch(0), mkBatch(promchunk.BatchSize), mkBatch(2 * promchunk.BatchSize)},
		},

		{
			input1: []promchunk.Batch{mkBatch(promchunk.BatchSize / 2), mkBatch(3 * promchunk.BatchSize / 2), mkBatch(5 * promchunk.BatchSize / 2)},
			input2: []promchunk.Batch{mkBatch(0), mkBatch(promchunk.BatchSize), mkBatch(3 * promchunk.BatchSize)},
			output: []promchunk.Batch{mkBatch(0), mkBatch(promchunk.BatchSize), mkBatch(2 * promchunk.BatchSize), mkBatch(3 * promchunk.BatchSize)},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			result := make(batchStream, len(tc.input1)+len(tc.input2))
			result = mergeStreams(tc.input1, tc.input2, result, promchunk.BatchSize)
			require.Equal(t, tc.output, result)
		})
	}
}

func mkBatch(from int64) promchunk.Batch {
	var result promchunk.Batch
	for i := int64(0); i < promchunk.BatchSize; i++ {
		result.Timestamps[i] = from + i
		result.Values[i] = float64(from + i)
	}
	result.Length = promchunk.BatchSize
	return result
}
