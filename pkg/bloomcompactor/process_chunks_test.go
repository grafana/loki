package bloomcompactor

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/push"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

var (
	userID            = "userID"
	from   model.Time = model.TimeFromUnixNano(0)
	to     model.Time = model.TimeFromUnixNano(0)

	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
)

type TokenizerFunc func(e logproto.Entry) [][]byte

var SpaceTokenizer TokenizerFunc = func(e logproto.Entry) [][]byte {
	// todo verify []byte(e.Line) doesn't do an allocation.
	return bytes.Split([]byte(e.Line), []byte(` `))
}

func createSeriesWithBloom(lbs []labels.Labels) ([]v1.SeriesWithBloom, []model.Fingerprint) {
	var fps []model.Fingerprint
	var bloomsForChunks []v1.SeriesWithBloom

	for i, lb := range lbs {
		fps = append(fps, model.Fingerprint(lb.Hash()))
		bloomsForChunks = append(bloomsForChunks, v1.SeriesWithBloom{
			Series: &v1.Series{
				Fingerprint: fps[i],
			},
			Bloom: &v1.Bloom{
				*filter.NewDefaultScalableBloomFilter(0.01),
			},
		})
	}
	return bloomsForChunks, fps
}

func fillBloom(b v1.SeriesWithBloom, c chunk.Chunk, tokenizer TokenizerFunc) error {
	itr, err := c.Data.(*chunkenc.Facade).LokiChunk().Iterator(
		context.Background(),
		time.Unix(0, 0),
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		log.NewNoopPipeline().ForStream(c.Metric),
	)
	if err != nil {
		return err
	}
	// todo log error of close
	defer itr.Close()
	for itr.Next() {
		for _, t := range tokenizer(itr.Entry()) {
			b.Bloom.Add(t)
		}
	}
	if err := itr.Error(); err != nil {
		return err
	}
	b.Series.Chunks = append(b.Series.Chunks, v1.ChunkRef{
		Start:    c.From,
		End:      c.Through,
		Checksum: c.Checksum,
	})
	return nil
}

func createMemchunks() []*chunkenc.MemChunk {
	var memChunks []*chunkenc.MemChunk = make([]*chunkenc.MemChunk, 0)

	memChunk0 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	memChunk0.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line",
	})

	memChunk1 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	memChunk1.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line for second chunk",
	})

	memChunk2 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	memChunk2.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line for third chunk",
	})

	memChunks = append(memChunks, memChunk0, memChunk1, memChunk2)

	return memChunks
}

/*
Test that chunk data can be stored at a bloom filter and then queried back
Case1: Create one series with one chunk
Case2: Create one series with N chunks**
Case3: Create M series with one chunk per series

**Suspected bug in bloom_querier:
In Case2 test1 CheckChunksForSeries returns all chunks in a series if a single chunk matches search, whereas it should be only matched chunks
Whereas Case3 tests shows that it behaves as expected with unmatched data.
*/

func Test_BuildAndQueryBloomsCase1(t *testing.T) {
	var lbsList []labels.Labels
	lbsList = append(lbsList, labels.FromStrings("foo", "bar"))

	bloomsForChunks, fps := createSeriesWithBloom(lbsList)

	bloomForChunks := bloomsForChunks[0]
	fp := fps[0]
	lbs := lbsList[0]

	var (
		chunks []chunk.Chunk = make([]chunk.Chunk, 1)
		refs   []v1.ChunkRef = make([]v1.ChunkRef, 1)
	)

	memChk := createMemchunks()[0]

	for i := range chunks {
		chunks[i] = chunk.NewChunk(userID, fp, lbs, chunkenc.NewFacade(memChk, testBlockSize, testTargetSize), from, to)
		require.NoError(t, chunks[i].Encode())
		refs[i] = v1.ChunkRef{
			Start:    chunks[i].From,
			End:      chunks[i].Through,
			Checksum: chunks[i].Checksum,
		}
	}
	// that's what we test
	for _, c := range chunks {
		fillBloom(bloomForChunks, c, SpaceTokenizer)
	}

	blockDir := t.TempDir()
	// Write the block to disk
	writer := v1.NewDirectoryBlockWriter(blockDir)
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), writer)
	require.NoError(t, err)
	err = builder.BuildFrom(v1.NewSliceIter([]v1.SeriesWithBloom{bloomForChunks}))
	require.NoError(t, err)

	// read and verify the data.
	querier := v1.NewBlockQuerier(v1.NewBlock(v1.NewDirectoryBlockReader(blockDir)))

	matches, err := querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("line")})
	require.NoError(t, err)
	require.Equal(t, 1, len(matches))

	matches, err = querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("bar")})
	require.NoError(t, err)
	require.Equal(t, 0, len(matches))
}

func Test_BuildAndQueryBloomsCase2(t *testing.T) {
	var lbsList []labels.Labels
	lbsList = append(lbsList, labels.FromStrings("foo", "bar"))

	bloomsForChunks, fps := createSeriesWithBloom(lbsList)

	bloomForChunks := bloomsForChunks[0]
	fp := fps[0]
	lbs := lbsList[0]

	var (
		chunks    []chunk.Chunk        = make([]chunk.Chunk, 3)
		refs      []v1.ChunkRef        = make([]v1.ChunkRef, 3)
		memChunks []*chunkenc.MemChunk = createMemchunks()
	)

	for i := range chunks {
		chunks[i] = chunk.NewChunk(userID,
			fp, lbs, chunkenc.NewFacade(memChunks[i], testBlockSize, testTargetSize), from, to)
		require.NoError(t, chunks[i].Encode())
		refs[i] = v1.ChunkRef{
			Start:    chunks[i].From,
			End:      chunks[i].Through,
			Checksum: chunks[i].Checksum,
		}
	}

	// that's what we test
	for _, c := range chunks {
		fillBloom(bloomForChunks, c, SpaceTokenizer)
	}

	blockDir := t.TempDir()
	// Write the block to disk
	writer := v1.NewDirectoryBlockWriter(blockDir)
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), writer)
	require.NoError(t, err)
	err = builder.BuildFrom(v1.NewSliceIter([]v1.SeriesWithBloom{bloomForChunks}))
	require.NoError(t, err)

	// read and verify the data.
	querier := v1.NewBlockQuerier(v1.NewBlock(v1.NewDirectoryBlockReader(blockDir)))

	// This test demonstrates the problem
	// In a single series if a single chunk matches the search, all chunks from the series are returned by the CheckChunksForSeries.
	// Whereas it should return only the matching chunks.
	matches, err := querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("second")})
	require.NoError(t, err)
	require.Equal(t, 1, len(matches))

	matches, err = querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("line")})
	require.NoError(t, err)
	require.Equal(t, 3, len(matches))

	matches, err = querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("bar")})
	require.NoError(t, err)
	require.Equal(t, 0, len(matches))
}

func Test_BuildAndQueryBloomsCase3(t *testing.T) {
	var lbsList []labels.Labels

	lbsList = append(lbsList,
		labels.FromStrings("app1", "value1"),
		labels.FromStrings("app2", "value2"),
		labels.FromStrings("app3", "value3"))

	bloomsForChunks, fps := createSeriesWithBloom(lbsList)

	var (
		chunks    []chunk.Chunk        = make([]chunk.Chunk, 3)
		refs      []v1.ChunkRef        = make([]v1.ChunkRef, 3)
		memChunks []*chunkenc.MemChunk = createMemchunks()
	)

	for i := range chunks {
		chunks[i] = chunk.NewChunk(userID, fps[i], lbsList[i], chunkenc.NewFacade(memChunks[i], testBlockSize, testTargetSize), from, to)
		require.NoError(t, chunks[i].Encode())
		refs[i] = v1.ChunkRef{
			Start:    chunks[i].From,
			End:      chunks[i].Through,
			Checksum: chunks[i].Checksum,
		}
	}

	// that's what we test
	for i, c := range chunks {
		fillBloom(bloomsForChunks[i], c, SpaceTokenizer)
	}

	blockDir := t.TempDir()
	// Write the block to disk
	writer := v1.NewDirectoryBlockWriter(blockDir)
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), writer)
	require.NoError(t, err)
	err = builder.BuildFrom(v1.NewSliceIter(bloomsForChunks))
	require.NoError(t, err)

	// read and verify the data.
	querier := v1.NewBlockQuerier(v1.NewBlock(v1.NewDirectoryBlockReader(blockDir)))

	// result may be in the other 2 series
	matches, err := querier.CheckChunksForSeries(fps[0], refs, [][]byte{[]byte("second")})
	require.NoError(t, err)
	require.Equal(t, 2, len(matches))

	//present in all
	matches, err = querier.CheckChunksForSeries(fps[0], refs, [][]byte{[]byte("line")})
	require.NoError(t, err)
	require.Equal(t, 3, len(matches))

	// returns matched chunk + other chunks as results may be in the other unmatched 2 series
	matches, err = querier.CheckChunksForSeries(fps[1], refs, [][]byte{[]byte("second")})
	require.NoError(t, err)
	require.Equal(t, 3, len(matches))
}
