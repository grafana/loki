package stats

import (
	"context"
	"testing"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/require"
)

func TestSnapshot(t *testing.T) {
	ctx := NewContext(context.Background())

	GetChunkData(ctx).BytesUncompressed += 10
	GetChunkData(ctx).LinesUncompressed += 20
	GetChunkData(ctx).BytesDecompressed += 40
	GetChunkData(ctx).LinesDecompressed += 20
	GetChunkData(ctx).BytesCompressed += 30
	GetChunkData(ctx).TotalDuplicates += 10

	GetStoreData(ctx).TotalChunksRef += 50
	GetStoreData(ctx).TotalDownloadedChunks += 60
	GetStoreData(ctx).TimeDownloadingChunks += time.Second

	fakeIngesterQuery(ctx)
	fakeIngesterQuery(ctx)

	res := Snapshot(ctx, 2*time.Second)
	expected := Result{
		Ingester: Ingester{
			IngesterData: IngesterData{
				TotalChunksMatched: 200,
				TotalBatches:       50,
				TotalLinesSent:     60,
			},
			ChunkData: ChunkData{
				BytesUncompressed: 10,
				LinesUncompressed: 20,
				BytesDecompressed: 24,
				LinesDecompressed: 40,
				BytesCompressed:   60,
				TotalDuplicates:   2,
			},
			TotalReached: 2,
		},
		Store: Store{
			StoreData: StoreData{
				TotalChunksRef:        50,
				TotalDownloadedChunks: 60,
				TimeDownloadingChunks: time.Second,
			},
			ChunkData: ChunkData{
				BytesUncompressed: 10,
				LinesUncompressed: 20,
				BytesDecompressed: 40,
				LinesDecompressed: 20,
				BytesCompressed:   30,
				TotalDuplicates:   10,
			},
		},
		Summary: Summary{
			ExecTime:                 2 * time.Second,
			BytesProcessedPerSeconds: int64(42),
			LinesProcessedPerSeconds: int64(50),
			TotalBytesProcessed:      int64(84),
			TotalLinesProcessed:      int64(100),
		},
	}
	require.Equal(t, expected, res)
}

func fakeIngesterQuery(ctx context.Context) {
	d, _ := ctx.Value(trailersKey).(*trailerCollector)
	meta := d.addTrailer()

	c, _ := jsoniter.MarshalToString(ChunkData{
		BytesUncompressed: 5,
		LinesUncompressed: 10,
		BytesDecompressed: 12,
		LinesDecompressed: 20,
		BytesCompressed:   30,
		TotalDuplicates:   1,
	})
	meta.Set(chunkDataKey, c)
	i, _ := jsoniter.MarshalToString(IngesterData{
		TotalChunksMatched: 100,
		TotalBatches:       25,
		TotalLinesSent:     30,
	})
	meta.Set(ingesterDataKey, i)
}
