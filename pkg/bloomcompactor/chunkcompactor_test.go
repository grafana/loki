package bloomcompactor

import (
	"context"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/push"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/stretchr/testify/require"
)

var (
	userID = "userID"
	fpRate = 0.01

	from = model.Earliest
	to   = model.Latest

	table     = "test_table"
	indexPath = "index_test_table"
	dst       = "local_path"

	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
)

func createTestChunk(fp model.Fingerprint, lb labels.Labels) chunk.Chunk {
	memChunk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	if err := memChunk.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "this is a log line",
	}); err != nil {
		panic(err)
	}
	c := chunk.NewChunk(userID,
		fp, lb, chunkenc.NewFacade(memChunk, testBlockSize, testTargetSize), from, to)

	return c
}

// Given a seriesMeta and corresponding chunks verify SeriesWithBloom can be built
func TestChunkCompactor_BuildBloomFromSeries(t *testing.T) {
	label := labels.FromStrings("foo", "bar")
	fp := model.Fingerprint(label.Hash())
	seriesMeta := SeriesMeta{
		seriesFP:  fp,
		seriesLbs: label,
	}

	chunks := []chunk.Chunk{createTestChunk(fp, label)}

	mbt := mockBloomTokenizer{}
	bloom := buildBloomFromSeries(seriesMeta, fpRate, &mbt, chunks)
	require.Equal(t, seriesMeta.seriesFP, bloom.Series.Fingerprint)
	require.Equal(t, chunks, mbt.chunks)
}

func TestChunkCompactor_CompactNewChunks(t *testing.T) {
	logger := log.NewNopLogger()
	label := labels.FromStrings("foo", "bar")
	fp1 := model.Fingerprint(100)
	fp2 := model.Fingerprint(200)

	chunkRef := index.ChunkMeta{
		Checksum: 1243,
		MinTime:  1,
		MaxTime:  999,
	}
	seriesMetas := []SeriesMeta{
		{
			seriesFP:  fp1,
			seriesLbs: label,
			chunkRefs: []index.ChunkMeta{chunkRef},
		},
		{
			seriesFP:  fp2,
			seriesLbs: label,
			chunkRefs: []index.ChunkMeta{chunkRef, chunkRef},
		},
	}

	job := Job{
		tableName:   table,
		tenantID:    userID,
		indexPath:   indexPath,
		seriesMetas: seriesMetas,
	}

	mbt := mockBloomTokenizer{}
	mcc := mockChunkClient{}
	compactedBlock, err := CompactNewChunks(context.Background(), logger, job, fpRate, &mbt, &mcc, dst)
	require.NoError(t, err)
	require.NotNil(t, compactedBlock)
}

type mockBloomTokenizer struct {
	chunks []chunk.Chunk
}

func (mbt *mockBloomTokenizer) PopulateSeriesWithBloom(_ *v1.SeriesWithBloom, c []chunk.Chunk) {
	mbt.chunks = c
}

func (mbt *mockBloomTokenizer) GetNGramLength() uint64 {
	return 4
}

func (mbt *mockBloomTokenizer) GetNGramSkip() uint64 {
	return 0
}

type mockChunkClient struct{}

func (mcc *mockChunkClient) GetChunks(_ context.Context, _ []chunk.Chunk) ([]chunk.Chunk, error) {
	return nil, nil
}
