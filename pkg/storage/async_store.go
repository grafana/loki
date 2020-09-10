package storage

import (
	"context"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type IngesterQuerier interface {
	GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error)
}

// AsyncStore does querying to both ingesters and chunk store and combines the results after deduping them.
// This should be used when using an async store like boltdb-shipper.
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
	chks, fetchers, err := a.Store.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, nil, err
	}

	unindexedChks, err := a.ingesterQuerier.GetChunkIDs(ctx, from, through, matchers...)
	if err != nil {
		return nil, nil, err
	}

	return a.mergeIndexedAndUnindexedChunks(userID, chks, fetchers, unindexedChks)
}

func (a *AsyncStore) mergeIndexedAndUnindexedChunks(userID string, indexedChunks [][]chunk.Chunk, fetchers []*chunk.Fetcher, unindexedChunkIDs []string) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	unindexedChunkIDs = filterDuplicateUnindexedChunks(indexedChunks, unindexedChunkIDs)
	fetcherToChunksGroupIdx := make(map[*chunk.Fetcher]int, len(fetchers))

	for i, fetcher := range fetchers {
		fetcherToChunksGroupIdx[fetcher] = i
	}

	for _, chunkID := range unindexedChunkIDs {
		chk, err := chunk.ParseExternalKey(userID, chunkID)
		if err != nil {
			return nil, nil, err
		}

		fetcher := a.Store.GetChunkFetcher(chk)

		if _, ok := fetcherToChunksGroupIdx[fetcher]; !ok {
			fetchers = append(fetchers, fetcher)
			fetcherToChunksGroupIdx[fetcher] = len(fetchers) - 1
		}
		chunksGroupIdx := fetcherToChunksGroupIdx[fetcher]

		indexedChunks[chunksGroupIdx] = append(indexedChunks[chunksGroupIdx], chk)
	}

	return indexedChunks, fetchers, nil
}

func filterDuplicateUnindexedChunks(indexedChunks [][]chunk.Chunk, unindexedChunkIDs []string) []string {
	filteredChunkIDs := make([]string, 0, len(unindexedChunkIDs))
	seen := make(map[string]struct{}, len(indexedChunks))

	for i := range indexedChunks {
		for j := range indexedChunks[i] {
			seen[indexedChunks[i][j].ExternalKey()] = struct{}{}
		}
	}

	for _, unindexedChunkID := range unindexedChunkIDs {
		if _, ok := seen[unindexedChunkID]; !ok {
			filteredChunkIDs = append(filteredChunkIDs, unindexedChunkID)
			seen[unindexedChunkID] = struct{}{}
		}
	}

	return filteredChunkIDs
}
