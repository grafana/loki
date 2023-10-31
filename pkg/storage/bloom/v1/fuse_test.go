package v1

import (
	"bytes"
	"context"
	"testing"

	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/stretchr/testify/require"
)

func TestFusedQuerier(t *testing.T) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := 100
	numKeysPerSeries := 10000
	data, _ := mkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			schema: Schema{
				version:  DefaultSchemaVersion,
				encoding: chunkenc.EncSnappy,
			},
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		writer,
	)
	require.Nil(t, err)
	itr := NewSliceIter[SeriesWithBloom](data)
	require.Nil(t, builder.BuildFrom(itr))
	block := NewBlock(reader)
	querier := NewBlockQuerier(block)

	nReqs := 10
	var inputs [][]request
	for i := 0; i < nReqs; i++ {
		ch := make(chan output)
		var reqs []request
		// find 2 series for each
		for j := 0; j < 2; j++ {
			idx := numSeries/nReqs*i + j
			reqs = append(reqs, request{
				fp:       data[idx].Series.Fingerprint,
				chks:     data[idx].Series.Chunks,
				response: ch,
			})
		}
		inputs = append(inputs, reqs)
	}

	var itrs []PeekingIterator[request]
	for _, reqs := range inputs {
		itrs = append(itrs, NewPeekingIter[request](NewSliceIter[request](reqs)))
	}

	resps := make([][]output, nReqs)
	go func() {
		concurrency.ForEachJob(
			context.Background(),
			len(resps),
			len(resps),
			func(_ context.Context, i int) error {
				for v := range inputs[i][0].response {
					resps[i] = append(resps[i], v)
				}
				return nil
			},
		)
	}()

	fused := querier.Fuse(itrs)
	require.Nil(t, fused.Run())

	for i, input := range inputs {
		for j, req := range input {
			resp := resps[i][j]
			require.Equal(
				t,
				output{
					fp:   req.fp,
					chks: req.chks,
				},
				resp,
			)
		}
	}
}
