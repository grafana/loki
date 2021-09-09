package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
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
	ingesterQuerier      IngesterQuerier
	queryIngestersWithin time.Duration
}

func NewAsyncStore(store chunk.Store, querier IngesterQuerier, queryIngestersWithin time.Duration) *AsyncStore {
	return &AsyncStore{
		Store:                store,
		ingesterQuerier:      querier,
		queryIngestersWithin: queryIngestersWithin,
	}
}

func (a *AsyncStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	spanLogger := spanlogger.FromContext(ctx)

	errs := make(chan error)

	var storeChunks [][]chunk.Chunk
	var fetchers []*chunk.Fetcher
	go func() {
		var err error
		storeChunks, fetchers, err = a.Store.GetChunkRefs(ctx, userID, from, through, matchers...)
		errs <- err
	}()

	var ingesterChunks []string

	go func() {
		if a.queryIngestersWithin != 0 {
			// don't query ingesters if the query does not overlap with queryIngestersWithin.
			if !through.After(model.Now().Add(-a.queryIngestersWithin)) {
				level.Debug(util_log.Logger).Log("msg", "skipping querying ingesters for chunk ids", "query-from", from, "query-through", through)
				errs <- nil
				return
			}
		}

		var err error
		ingesterChunks, err = a.ingesterQuerier.GetChunkIDs(ctx, from, through, matchers...)

		if err == nil {
			level.Debug(spanLogger).Log("ingester-chunks-count", len(ingesterChunks))
			level.Debug(util_log.Logger).Log("msg", "got chunk ids from ingester", "count", len(ingesterChunks))
		}
		errs <- err
	}()

	for i := 0; i < 2; i++ {
		err := <-errs
		if err != nil {
			return nil, nil, err
		}
	}

	if len(ingesterChunks) == 0 {
		return storeChunks, fetchers, nil
	}

	return a.mergeIngesterAndStoreChunks(userID, storeChunks, fetchers, ingesterChunks)
}

func (a *AsyncStore) mergeIngesterAndStoreChunks(userID string, storeChunks [][]chunk.Chunk, fetchers []*chunk.Fetcher, ingesterChunkIDs []string) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	ingesterChunkIDs = filterDuplicateChunks(storeChunks, ingesterChunkIDs)
	level.Debug(util_log.Logger).Log("msg", "post-filtering ingester chunks", "count", len(ingesterChunkIDs))

	fetcherToChunksGroupIdx := make(map[*chunk.Fetcher]int, len(fetchers))

	for i, fetcher := range fetchers {
		fetcherToChunksGroupIdx[fetcher] = i
	}

	for _, chunkID := range ingesterChunkIDs {
		chk, err := chunk.ParseExternalKey(userID, chunkID)
		if err != nil {
			return nil, nil, err
		}

		// ToDo(Sandeep) possible optimization: Keep the chunk fetcher reference handy after first call since it is expected to stay the same.
		fetcher := a.Store.GetChunkFetcher(chk.Through)
		if fetcher == nil {
			return nil, nil, fmt.Errorf("got a nil fetcher for chunk %s", chk.ExternalKey())
		}

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
