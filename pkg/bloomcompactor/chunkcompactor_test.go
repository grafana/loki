package bloomcompactor

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/push"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

var (
	userID = "userID"
	fpRate = 0.01

	from = model.Earliest
	to   = model.Latest

	table     = "test_table"
	indexPath = "index_test_table"

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
	seriesMeta := seriesMeta{
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
	// Setup
	logger := log.NewNopLogger()
	label := labels.FromStrings("foo", "bar")
	fp1 := model.Fingerprint(100)
	fp2 := model.Fingerprint(999)
	fp3 := model.Fingerprint(200)

	chunkRef1 := index.ChunkMeta{
		Checksum: 1,
		MinTime:  1,
		MaxTime:  99,
	}

	chunkRef2 := index.ChunkMeta{
		Checksum: 2,
		MinTime:  10,
		MaxTime:  999,
	}

	seriesMetas := []seriesMeta{
		{
			seriesFP:  fp1,
			seriesLbs: label,
			chunkRefs: []index.ChunkMeta{chunkRef1},
		},
		{
			seriesFP:  fp2,
			seriesLbs: label,
			chunkRefs: []index.ChunkMeta{chunkRef1, chunkRef2},
		},
		{
			seriesFP:  fp3,
			seriesLbs: label,
			chunkRefs: []index.ChunkMeta{chunkRef1, chunkRef1, chunkRef2},
		},
	}

	job := NewJob(userID, table, indexPath, seriesMetas)

	mbt := mockBloomTokenizer{}
	mcc := mockChunkClient{}
	pbb := mockPersistentBlockBuilder{}

	// Run Compaction
	compactedBlock, err := compactNewChunks(context.Background(), logger, job, fpRate, &mbt, &mcc, &pbb)

	// Validate Compaction Succeeds
	require.NoError(t, err)
	require.NotNil(t, compactedBlock)

	// Validate Compacted Block has expected data
	require.Equal(t, job.tenantID, compactedBlock.TenantID)
	require.Equal(t, job.tableName, compactedBlock.TableName)
	require.Equal(t, uint64(fp1), compactedBlock.MinFingerprint)
	require.Equal(t, uint64(fp2), compactedBlock.MaxFingerprint)
	require.Equal(t, chunkRef1.MinTime, compactedBlock.StartTimestamp)
	require.Equal(t, chunkRef2.MaxTime, compactedBlock.EndTimestamp)
	require.Equal(t, indexPath, compactedBlock.IndexPath)
}

type mockBloomTokenizer struct {
	chunks []chunk.Chunk
}

func (mbt *mockBloomTokenizer) PopulateSeriesWithBloom(_ *v1.SeriesWithBloom, c []chunk.Chunk) {
	mbt.chunks = c
}

type mockChunkClient struct{}

func (mcc *mockChunkClient) GetChunks(_ context.Context, _ []chunk.Chunk) ([]chunk.Chunk, error) {
	return nil, nil
}

type mockPersistentBlockBuilder struct {
}

func (pbb *mockPersistentBlockBuilder) BuildFrom(_ v1.Iterator[v1.SeriesWithBloom]) (uint32, error) {
	return 0, nil
}

func (pbb *mockPersistentBlockBuilder) Data() (io.ReadCloser, error) {
	return nil, nil
}
