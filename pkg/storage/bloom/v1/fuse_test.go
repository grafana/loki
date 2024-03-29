package v1

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/concurrency"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
)

func keysToBloomTest(keys [][]byte) BloomTest {
	var tokenizer fakeNgramBuilder
	tests := make(BloomTests, 0, len(keys))
	for _, key := range keys {
		tests = append(tests, newStringTest(tokenizer, string(key)))
	}

	return tests
}

func TestFusedQuerier(t *testing.T) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := 1000
	data, keys := MkBasicSeriesWithBlooms(numSeries, 0, 0x0000, 0xffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema: Schema{
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
	_, err = builder.BuildFrom(itr)
	require.NoError(t, err)
	require.False(t, itr.Next())
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, true, DefaultMaxPageSize)

	n := 2
	nReqs := numSeries / n
	var inputs [][]Request
	var resChans []chan Output
	for i := 0; i < nReqs; i++ {
		ch := make(chan Output)
		var reqs []Request
		// find n series for each
		for j := 0; j < n; j++ {
			idx := numSeries/nReqs*i + j
			reqs = append(reqs, Request{
				Fp:       data[idx].Series.Fingerprint,
				Chks:     data[idx].Series.Chunks,
				Response: ch,
				Search:   keysToBloomTest(keys[idx]),
			})
		}
		inputs = append(inputs, reqs)
		resChans = append(resChans, ch)
	}

	var itrs []PeekingIterator[Request]
	for _, reqs := range inputs {
		itrs = append(itrs, NewPeekingIter[Request](NewSliceIter[Request](reqs)))
	}

	resps := make([][]Output, nReqs)
	var g sync.WaitGroup
	g.Add(1)
	go func() {
		require.Nil(t, concurrency.ForEachJob(
			context.Background(),
			len(resChans),
			len(resChans),
			func(_ context.Context, i int) error {
				for v := range resChans[i] {
					resps[i] = append(resps[i], v)
				}
				return nil
			},
		))
		g.Done()
	}()

	fused := querier.Fuse(itrs, log.NewNopLogger())

	require.Nil(t, fused.Run())
	for _, input := range inputs {
		close(input[0].Response)
	}
	g.Wait()

	for i, input := range inputs {
		for j, req := range input {
			resp := resps[i][j]
			require.Equal(
				t,
				Output{
					Fp:       req.Fp,
					Removals: nil,
				},
				resp,
			)
		}
	}
}

func setupBlockForBenchmark(b *testing.B) (*BlockQuerier, [][]Request, []chan Output) {
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := 10000
	numKeysPerSeries := 100
	data, _ := MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema: Schema{
				version:  DefaultSchemaVersion,
				encoding: chunkenc.EncSnappy,
			},
			SeriesPageSize: 256 << 10, // 256k
			BloomPageSize:  1 << 20,   // 1MB
		},
		writer,
	)
	require.Nil(b, err)
	itr := NewSliceIter[SeriesWithBloom](data)
	_, err = builder.BuildFrom(itr)
	require.Nil(b, err)
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, true, DefaultMaxPageSize)

	numRequestChains := 100
	seriesPerRequest := 100
	var requestChains [][]Request
	var responseChans []chan Output
	for i := 0; i < numRequestChains; i++ {
		var reqs []Request
		// ensure they use the same channel
		ch := make(chan Output)
		// evenly spread out the series queried within a single request chain
		// to mimic series distribution across keyspace
		for j := 0; j < seriesPerRequest; j++ {
			// add the chain index (i) for a little jitter
			idx := numSeries*j/seriesPerRequest + i
			if idx >= numSeries {
				idx = numSeries - 1
			}
			reqs = append(reqs, Request{
				Fp:       data[idx].Series.Fingerprint,
				Chks:     data[idx].Series.Chunks,
				Response: ch,
			})
		}
		requestChains = append(requestChains, reqs)
		responseChans = append(responseChans, ch)
	}

	return querier, requestChains, responseChans
}

func BenchmarkBlockQuerying(b *testing.B) {
	b.StopTimer()
	querier, requestChains, responseChans := setupBlockForBenchmark(b)
	// benchmark
	b.StartTimer()

	b.Run("single-pass", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, chain := range requestChains {
				for _, req := range chain {
					_, _ = querier.CheckChunksForSeries(req.Fp, req.Chks, nil)
				}
			}
		}

	})
	b.Run("fused", func(b *testing.B) {
		// spin up some goroutines to consume the responses so they don't block
		go func() {
			require.Nil(b, concurrency.ForEachJob(
				context.Background(),
				len(responseChans), len(responseChans),
				func(_ context.Context, idx int) error {
					// nolint:revive
					for range responseChans[idx] {
					}
					return nil
				},
			))
		}()

		var itrs []PeekingIterator[Request]

		for i := 0; i < b.N; i++ {
			itrs = itrs[:0]
			for _, reqs := range requestChains {
				itrs = append(itrs, NewPeekingIter[Request](NewSliceIter[Request](reqs)))
			}
			fused := querier.Fuse(itrs, log.NewNopLogger())
			_ = fused.Run()
		}
	})

}
