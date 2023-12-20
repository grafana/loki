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
	bloom, err := buildBloomFromSeries(seriesMeta, fpRate, &mbt, chunks)
	require.NoError(t, err)
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

func TestLazyBloomBuilder(t *testing.T) {
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

	mbt := &mockBloomTokenizer{}
	mcc := &mockChunkClient{}

	it := newLazyBloomBuilder(context.Background(), job, mcc, mbt, fpRate)

	// first seriesMeta has 1 chunks
	require.True(t, it.Next())
	require.Equal(t, 1, mcc.requestCount)
	require.Equal(t, 1, mcc.chunkCount)
	require.Equal(t, fp1, it.At().Series.Fingerprint)

	// first seriesMeta has 2 chunks
	require.True(t, it.Next())
	require.Equal(t, 2, mcc.requestCount)
	require.Equal(t, 3, mcc.chunkCount)
	require.Equal(t, fp2, it.At().Series.Fingerprint)

	// first seriesMeta has 3 chunks
	require.True(t, it.Next())
	require.Equal(t, 3, mcc.requestCount)
	require.Equal(t, 6, mcc.chunkCount)
	require.Equal(t, fp3, it.At().Series.Fingerprint)

	// iterator is done
	require.False(t, it.Next())
	require.Error(t, io.EOF, it.Err())
	require.Equal(t, v1.SeriesWithBloom{}, it.At())
}

type mockBloomTokenizer struct {
	chunks []chunk.Chunk
}

func (mbt *mockBloomTokenizer) PopulateSeriesWithBloom(_ *v1.SeriesWithBloom, c []chunk.Chunk) error {
	mbt.chunks = append(mbt.chunks, c...)
	return nil
}

type mockChunkClient struct {
	requestCount int
	chunkCount   int
}

func (mcc *mockChunkClient) GetChunks(_ context.Context, chks []chunk.Chunk) ([]chunk.Chunk, error) {
	mcc.requestCount++
	mcc.chunkCount += len(chks)
	return nil, nil
}

type mockPersistentBlockBuilder struct {
}

func (pbb *mockPersistentBlockBuilder) BuildFrom(_ v1.Iterator[v1.SeriesWithBloom]) (uint32, error) {
	return 0, nil
}

func (pbb *mockPersistentBlockBuilder) Data() (io.ReadSeekCloser, error) {
	return nil, nil
}
