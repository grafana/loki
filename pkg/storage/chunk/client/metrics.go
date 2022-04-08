package client

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/storage/chunk"
)

// takes a chunk client and exposes metrics for its operations.
type MetricsChunkClient struct {
	Client Client

	metrics ChunkClientMetrics
}

func NewMetricsChunkClient(client Client, metrics ChunkClientMetrics) MetricsChunkClient {
	return MetricsChunkClient{
		Client:  client,
		metrics: metrics,
	}
}

type ChunkClientMetrics struct {
	chunksPutPerUser         *prometheus.CounterVec
	chunksSizePutPerUser     *prometheus.CounterVec
	chunksFetchedPerUser     *prometheus.CounterVec
	chunksSizeFetchedPerUser *prometheus.CounterVec
}

func NewChunkClientMetrics(reg prometheus.Registerer) ChunkClientMetrics {
	return ChunkClientMetrics{
		chunksPutPerUser: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "chunk_store_stored_chunks_total",
			Help:      "Total stored chunks per user.",
		}, []string{"user"}),
		chunksSizePutPerUser: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "chunk_store_stored_chunk_bytes_total",
			Help:      "Total bytes stored in chunks per user.",
		}, []string{"user"}),
		chunksFetchedPerUser: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "chunk_store_fetched_chunks_total",
			Help:      "Total fetched chunks per user.",
		}, []string{"user"}),
		chunksSizeFetchedPerUser: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "chunk_store_fetched_chunk_bytes_total",
			Help:      "Total bytes fetched in chunks per user.",
		}, []string{"user"}),
	}
}

func (c MetricsChunkClient) Stop() {
	c.Client.Stop()
}

func (c MetricsChunkClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	if err := c.Client.PutChunks(ctx, chunks); err != nil {
		return err
	}

	// For PutChunks, we explicitly encode the userID in the chunk and don't use context.
	userSizes := map[string]int{}
	userCounts := map[string]int{}
	for _, c := range chunks {
		userSizes[c.UserID] += c.Data.Size()
		userCounts[c.UserID]++
	}
	for user, size := range userSizes {
		c.metrics.chunksSizePutPerUser.WithLabelValues(user).Add(float64(size))
	}
	for user, num := range userCounts {
		c.metrics.chunksPutPerUser.WithLabelValues(user).Add(float64(num))
	}

	return nil
}

func (c MetricsChunkClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	chks, err := c.Client.GetChunks(ctx, chunks)
	if err != nil {
		return chks, err
	}

	// For GetChunks, userID is the chunk and we don't need to use context.
	// For now, we just load one user chunks at once, but the interface lets us do it for multiple users.
	userSizes := map[string]int{}
	userCounts := map[string]int{}
	for _, c := range chks {
		userSizes[c.UserID] += c.Data.Size()
		userCounts[c.UserID]++
	}
	for user, size := range userSizes {
		c.metrics.chunksSizeFetchedPerUser.WithLabelValues(user).Add(float64(size))
	}
	for user, num := range userCounts {
		c.metrics.chunksFetchedPerUser.WithLabelValues(user).Add(float64(num))
	}

	return chks, nil
}

func (c MetricsChunkClient) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	return c.Client.DeleteChunk(ctx, userID, chunkID)
}

func (c MetricsChunkClient) IsChunkNotFoundErr(err error) bool {
	return c.Client.IsChunkNotFoundErr(err)
}
