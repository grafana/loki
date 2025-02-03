package v1

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/concurrency"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

var BloomPagePool = mempool.New("test", []mempool.Bucket{
	{Size: 16, Capacity: 128 << 10},
	{Size: 16, Capacity: 256 << 10},
	{Size: 16, Capacity: 512 << 10},
}, nil)

type singleKeyTest []byte

// Matches implements BloomTest.
func (s singleKeyTest) Matches(_ labels.Labels, bloom filter.Checker) bool {
	return bloom.Test(s)
}

// MatchesWithPrefixBuf implements BloomTest.
func (s singleKeyTest) MatchesWithPrefixBuf(_ labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	return bloom.Test(append(buf[:prefixLen], s...))
}

// compiler check
var _ BloomTest = singleKeyTest("")

func keysToBloomTest(keys [][]byte) BloomTest {
	tests := make(BloomTests, 0, len(keys))
	for _, key := range keys {
		tests = append(tests, singleKeyTest(key))
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
	data, _ := MkBasicSeriesWithBlooms(numSeries, 0x0000, 0xffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		writer,
	)
	require.Nil(t, err)
	itr := v2.NewSliceIter(data)
	_, err = builder.BuildFrom(itr)
	require.NoError(t, err)
	require.False(t, itr.Next())
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, BloomPagePool, DefaultMaxPageSize)

	n := 500 // series per request
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
				Recorder: NewBloomRecorder(context.Background(), "unknown"),
				Fp:       data[idx].Series.Fingerprint,
				Chks:     data[idx].Series.Chunks,
				Response: ch,
				Search:   singleKeyTest("trace_id"),
			})
		}
		inputs = append(inputs, reqs)
		resChans = append(resChans, ch)
	}

	var itrs []v2.PeekIterator[Request]
	for _, reqs := range inputs {
		itrs = append(itrs, v2.NewPeekIter(v2.NewSliceIter(reqs)))
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
			require.Equal(t, Output{Fp: req.Fp, Removals: nil}, resp)
		}
	}
}

// Successfully query series across multiple pages as well as series that only occupy 1 bloom
func TestFusedQuerier_MultiPage(t *testing.T) {
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
			SeriesPageSize: 100,
			BloomPageSize:  10, // So we force one bloom per page
		},
		writer,
	)
	require.Nil(t, err)

	fp := model.Fingerprint(1)
	chk := ChunkRef{
		From:     0,
		Through:  10,
		Checksum: 0,
	}
	series := Series{
		Fingerprint: fp,
		Chunks:      []ChunkRef{chk},
	}

	buf := prefixForChunkRef(chk)

	b1 := &Bloom{
		*filter.NewScalableBloomFilter(1024, 0.01, 0.8),
	}
	key1, key2 := []byte("foo"), []byte("bar")
	b1.Add(key1)
	b1.Add(append(buf, key1...))

	b2 := &Bloom{
		*filter.NewScalableBloomFilter(1024, 0.01, 0.8),
	}
	b2.Add(key2)
	b2.Add(append(buf, key2...))

	_, err = builder.BuildFrom(v2.NewSliceIter([]SeriesWithBlooms{
		{
			Series: &SeriesWithMeta{
				Series: series,
			},
			Blooms: v2.NewSliceIter([]*Bloom{b1, b2}),
		},
	}))
	require.NoError(t, err)

	block := NewBlock(reader, NewMetrics(nil))

	querier := NewBlockQuerier(block, BloomPagePool, 100<<20) // 100MB too large to interfere

	keys := [][]byte{
		key1,          // found in the first bloom
		key2,          // found in the second bloom
		[]byte("not"), // not found in any bloom
	}

	chans := make([]chan Output, len(keys))
	for i := range chans {
		chans[i] = make(chan Output, 1) // buffered once to not block in test
	}

	req := func(key []byte, ch chan Output) Request {
		return Request{
			Fp:       fp,
			Chks:     []ChunkRef{chk},
			Search:   singleKeyTest(key),
			Response: ch,
			Recorder: NewBloomRecorder(context.Background(), "unknown"),
		}
	}
	var reqs []Request
	for i, key := range keys {
		reqs = append(reqs, req(key, chans[i]))
	}

	fused := querier.Fuse(
		[]v2.PeekIterator[Request]{
			v2.NewPeekIter(v2.NewSliceIter(reqs)),
		},
		log.NewNopLogger(),
	)

	require.NoError(t, fused.Run())

	// assume they're returned in order
	for i := range reqs {
		out := <-chans[i]

		// the last check doesn't match
		if i == len(keys)-1 {
			require.Equal(t, ChunkRefs{chk}, out.Removals)
			continue
		}
		require.Equal(t, ChunkRefs(nil), out.Removals, "on index %d and key %s", i, string(keys[i]))
	}

}

func TestLazyBloomIter_Seek_ResetError(t *testing.T) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	largeSeries := func(i int) bool {
		return i%2 == 0
	}

	numSeries := 4
	data := make([]SeriesWithBlooms, 0, numSeries)

	for i := 0; i < numSeries; i++ {
		var series Series
		series.Fingerprint = model.Fingerprint(i)
		series.Chunks = []ChunkRef{
			{
				From:     0,
				Through:  100,
				Checksum: uint32(i),
			},
		}

		bloom := NewBloom()

		nLines := 10
		// all even series will have a larger bloom (more than 1 filter)
		if largeSeries(i) {
			// Add enough lines to make the bloom page too large and
			// trigger another filter addition
			nLines = 10000
		}

		for j := 0; j < nLines; j++ {
			key := fmt.Sprintf("%04x:%04x", i, j)
			bloom.Add([]byte(key))
		}

		data = append(data, SeriesWithBlooms{
			Series: &SeriesWithMeta{
				Series: series,
			},
			Blooms: v2.NewSliceIter([]*Bloom{bloom}),
		})
	}

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
			SeriesPageSize: 100,
			BloomPageSize:  10, // So we force one series per page
		},
		writer,
	)
	require.Nil(t, err)
	itr := v2.NewSliceIter(data)
	_, err = builder.BuildFrom(itr)
	require.NoError(t, err)
	require.False(t, itr.Next())
	block := NewBlock(reader, NewMetrics(nil))

	querier := NewBlockQuerier(block, BloomPagePool, 1000)

	for fp := model.Fingerprint(0); fp < model.Fingerprint(numSeries); fp++ {
		err := querier.Seek(fp)
		require.NoError(t, err)

		require.True(t, querier.Next())
		series := querier.At()

		// earlier test only has 1 bloom offset per series
		require.Equal(t, 1, len(series.Offsets))
		require.Equal(t, fp, series.Fingerprint)

		//
		seekable := true
		if large := largeSeries(int(fp)); large {
			seekable = false
		}

		if !seekable {
			require.True(t, querier.blooms.LoadOffset(series.Offsets[0]))
			continue
		}

		for _, offset := range series.Offsets {
			require.False(t, querier.blooms.LoadOffset(offset))
			require.True(t, querier.blooms.Next())
			require.NoError(t, querier.blooms.Err())
		}

	}
}

func TestFusedQuerier_SkipsEmptyBlooms(t *testing.T) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		writer,
	)
	require.Nil(t, err)

	data := SeriesWithBlooms{
		Series: &SeriesWithMeta{
			Series: Series{
				Fingerprint: 0,
				Chunks: []ChunkRef{
					{
						From:     0,
						Through:  10,
						Checksum: 0x1234,
					},
				},
			},
		},
		Blooms: v2.NewSliceIter([]*Bloom{NewBloom()}),
	}

	itr := v2.NewSliceIter([]SeriesWithBlooms{data})
	_, err = builder.BuildFrom(itr)
	require.NoError(t, err)
	require.False(t, itr.Next())
	block := NewBlock(reader, NewMetrics(nil))
	ch := make(chan Output, 1)
	req := Request{
		Fp:       data.Series.Fingerprint,
		Chks:     data.Series.Chunks,
		Search:   keysToBloomTest([][]byte{[]byte("foobar")}),
		Response: ch,
		Recorder: NewBloomRecorder(context.Background(), "unknown"),
	}
	err = NewBlockQuerier(block, BloomPagePool, DefaultMaxPageSize).Fuse(
		[]v2.PeekIterator[Request]{
			v2.NewPeekIter(v2.NewSliceIter([]Request{req})),
		},
		log.NewNopLogger(),
	).Run()
	require.NoError(t, err)
	x := <-ch
	require.Equal(t, 0, len(x.Removals))
}

func setupBlockForBenchmark(b *testing.B) (*BlockQuerier, [][]Request, []chan Output) {
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := 10000
	data, _ := MkBasicSeriesWithBlooms(numSeries, 0, 0xffffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
			SeriesPageSize: 256 << 10, // 256k
			BloomPageSize:  1 << 20,   // 1MB
		},
		writer,
	)
	require.Nil(b, err)
	itr := v2.NewSliceIter(data)
	_, err = builder.BuildFrom(itr)
	require.Nil(b, err)
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, BloomPagePool, DefaultMaxPageSize)

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
				Recorder: NewBloomRecorder(context.Background(), "unknown"),
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

		var itrs []v2.PeekIterator[Request]

		for i := 0; i < b.N; i++ {
			itrs = itrs[:0]
			for _, reqs := range requestChains {
				itrs = append(itrs, v2.NewPeekIter(v2.NewSliceIter(reqs)))
			}
			fused := querier.Fuse(itrs, log.NewNopLogger())
			_ = fused.Run()
		}
	})

}
