package storage

import (
	"context"

	pkg_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type IngesterQuerier interface {
	GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error)
}

// AsyncStore does querying to both ingesters and chunk store and combines the results after deduping them.
// This should be used when using an async store like boltdb-shipper.
// AsyncStore is meant to be used only in queriers or any other service other than ingesters.
// It should never be used in ingesters otherwise it would start spiraling around doing queries over and over again to other ingesters.
type AsyncStore struct {
	chunk.Store
	ingesterQuerier IngesterQuerier
}

func NewAsyncStore(store chunk.Store, querier IngesterQuerier) *AsyncStore {
	return &AsyncStore{
		Store:           store,
		ingesterQuerier: querier,
	}
}

func (a *AsyncStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	spanLogger := spanlogger.FromContext(ctx)

	storeChunks, fetchers, err := a.Store.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, nil, err
	}

	level.Debug(spanLogger).Log("msg", "got chunk-refs from store")

	ingesterChunks, err := a.ingesterQuerier.GetChunkIDs(ctx, from, through, matchers...)
	if err != nil {
		return nil, nil, err
	}

	level.Debug(spanLogger).Log("ingester-chunks-count", len(ingesterChunks))
	level.Debug(pkg_util.Logger).Log("msg", "got chunk ids from ingester", "count", len(ingesterChunks))

	if len(ingesterChunks) == 0 {
		return storeChunks, fetchers, nil
	}

	return a.mergeIngesterAndStoreChunks(userID, storeChunks, fetchers, ingesterChunks)
}

func (a *AsyncStore) mergeIngesterAndStoreChunks(userID string, storeChunks [][]chunk.Chunk, fetchers []*chunk.Fetcher, ingesterChunkIDs []string) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	ingesterChunkIDs = filterDuplicateChunks(storeChunks, ingesterChunkIDs)
	level.Debug(pkg_util.Logger).Log("msg", "post-filtering ingester chunks", "count", len(ingesterChunkIDs))

	fetcherToChunksGroupIdx := make(map[*chunk.Fetcher]int, len(fetchers))

	for i, fetcher := range fetchers {
		fetcherToChunksGroupIdx[fetcher] = i
	}

	for _, chunkID := range ingesterChunkIDs {
		chk, err := chunk.ParseExternalKey(userID, chunkID)
		if err != nil {
			return nil, nil, err
		}

		fetcher := a.Store.GetChunkFetcher(chk)

		if _, ok := fetcherToChunksGroupIdx[fetcher]; !ok {
			fetchers = append(fetchers, fetcher)
			storeChunks = append(storeChunks, []chunk.Chunk{})
			fetcherToChunksGroupIdx[fetcher] = len(fetchers) - 1
		}
		chunksGroupIdx := fetcherToChunksGroupIdx[fetcher]

		storeChunks[chunksGroupIdx] = append(storeChunks[chunksGroupIdx], chk)
	}

	return storeChunks, fetchers, nil
}

func filterDuplicateChunks(storeChunks [][]chunk.Chunk, ingesterChunkIDs []string) []string {
	filteredChunkIDs := make([]string, 0, len(ingesterChunkIDs))
	seen := make(map[string]struct{}, len(storeChunks))

	for i := range storeChunks {
		for j := range storeChunks[i] {
			seen[storeChunks[i][j].ExternalKey()] = struct{}{}
		}
	}

	for _, chunkID := range ingesterChunkIDs {
		if _, ok := seen[chunkID]; !ok {
			filteredChunkIDs = append(filteredChunkIDs, chunkID)
			seen[chunkID] = struct{}{}
		}
	}

	return filteredChunkIDs
}
