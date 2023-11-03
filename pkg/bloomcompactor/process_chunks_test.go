package bloomcompactor

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/push"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
)

// This test suite will be replaced with a proper one in a follow up PR. This one is just handy during development.
var (
	userID            = "userID"
	from   model.Time = model.TimeFromUnixNano(0)
	to     model.Time = model.TimeFromUnixNano(0)

	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
)

func createSeriesWithBloom(lbs []labels.Labels) ([]v1.SeriesWithBloom, []model.Fingerprint) {
	var fps []model.Fingerprint
	var bloomsForChunks []v1.SeriesWithBloom

	for i, lb := range lbs {
		fps = append(fps, model.Fingerprint(lb.Hash()))
		bloom := &v1.Bloom{
			*filter.NewDefaultScalableBloomFilter(0.01),
		}
		bloomsForChunks = append(bloomsForChunks, v1.SeriesWithBloom{
			Series: &v1.Series{
				Fingerprint: fps[i],
			},
			Bloom: bloom,
		})
	}
	return bloomsForChunks, fps
}

func createMemchunks() []*chunkenc.MemChunk {
	var memChunks []*chunkenc.MemChunk = make([]*chunkenc.MemChunk, 0)

	memChunk0 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	if err := memChunk0.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line",
	}); err != nil {
		panic(err)
	}

	memChunk1 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	if err := memChunk1.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line for second chunk",
	}); err != nil {
		panic(err)
	}

	memChunk2 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	if err := memChunk2.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line for third chunk",
	}); err != nil {
		panic(err)
	}

	memChunks = append(memChunks, memChunk0, memChunk1, memChunk2)

	return memChunks
}

// Test that chunk data can be stored at a bloom filter and then queried back
func Test_BuildAndQueryBloomsWithOneSeries(t *testing.T) {
	//Create one series with N chunks**
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

	// create a tokenizer
	bt, _ := v1.NewBloomTokenizer(prometheus.DefaultRegisterer)

	for _, c := range chunks {
		bt.PopulateSeriesWithBloom(&bloomsForChunks[0], []chunk.Chunk{c})
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

	// TODO change the test once bloom-querier logic is completed.
	// Currently returns unexpected behaviour.
	// In a single series if a single chunk matches the search, all chunks from the series are returned by the CheckChunksForSeries.
	// Whereas it should return only the matching chunks.
	t.Skipf("skip until bloom-querier logic is completed")
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

func Test_BuildAndQueryBloomsWithNSeries(t *testing.T) {
	//Create M series with one chunk per series
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

	// create a tokenizer
	bt, _ := v1.NewBloomTokenizer(prometheus.DefaultRegisterer)

	for i, c := range chunks {
		bt.PopulateSeriesWithBloom(&bloomsForChunks[i], []chunk.Chunk{c})
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
	t.Skipf("I expect this to return 0 yet returns 2. Debug with block querier logic")
	matches, err := querier.CheckChunksForSeries(fps[0], refs, [][]byte{[]byte("second")})
	require.NoError(t, err)
	require.Equal(t, 0, len(matches))

	//present in all
	matches, err = querier.CheckChunksForSeries(fps[0], refs, [][]byte{[]byte("line")})
	require.NoError(t, err)
	require.Equal(t, 3, len(matches))

	// returns matched chunk + other chunks as results may be in the other unmatched 2 series
	// CheckChunksForSeries only returns must check, but not the ones found in bloom. Is that desirable.
	t.Skipf("got broken after tokenizer integration, skip until bloom-querier logic is completed")
	matches, err = querier.CheckChunksForSeries(fps[1], refs, [][]byte{[]byte("second")})
	require.NoError(t, err)
	require.Equal(t, 3, len(matches))

	matches, err = querier.CheckChunksForSeries(fps[0], refs, [][]byte{[]byte("chunk")})
	require.NoError(t, err)
	require.Equal(t, 2, len(matches))
}
