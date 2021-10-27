package storage

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util"
)

// storeMock is a mockable version of Loki's storage, used in querier unit tests
// to control the behaviour of the store without really hitting any storage backend
type storeMock struct {
	chunk.Store
	util.ExtendedMock
}

func newStoreMock() *storeMock {
	return &storeMock{}
}

func (s *storeMock) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	args := s.Called(ctx, userID, from, through, matchers)
	return args.Get(0).([][]chunk.Chunk), args.Get(1).([]*chunk.Fetcher), args.Error(2)
}

func (s *storeMock) GetChunkFetcher(tm model.Time) *chunk.Fetcher {
	args := s.Called(tm)
	return args.Get(0).(*chunk.Fetcher)
}

type ingesterQuerierMock struct {
	IngesterQuerier
	util.ExtendedMock
}

func newIngesterQuerierMock() *ingesterQuerierMock {
	return &ingesterQuerierMock{}
}

func (i *ingesterQuerierMock) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	args := i.Called(ctx, from, through, matchers)
	return args.Get(0).([]string), args.Error(1)
}

func buildMockChunkRef(t *testing.T, num int) []chunk.Chunk {
	now := time.Now()
	var chunks []chunk.Chunk

	for i := 0; i < num; i++ {
		chk := newChunk(buildTestStreams(fooLabelsWithName, timeRange{
			from: now.Add(time.Duration(i) * time.Minute),
			to:   now.Add(time.Duration(i+1) * time.Minute),
		}))

		chunkRef, err := chunk.ParseExternalKey(chk.UserID, chk.ExternalKey())
		require.NoError(t, err)

		chunks = append(chunks, chunkRef)
	}

	return chunks
}

func buildMockFetchers(num int) []*chunk.Fetcher {
	var fetchers []*chunk.Fetcher
	for i := 0; i < num; i++ {
		fetchers = append(fetchers, &chunk.Fetcher{})
	}

	return fetchers
}

func TestAsyncStore_mergeIngesterAndStoreChunks(t *testing.T) {
	testChunks := buildMockChunkRef(t, 10)
	fetchers := buildMockFetchers(3)
	for _, tc := range []struct {
		name             string
		storeChunks      [][]chunk.Chunk
		storeFetcher     []*chunk.Fetcher
		ingesterChunkIDs []string
		ingesterFetcher  *chunk.Fetcher
		expectedChunks   [][]chunk.Chunk
		expectedFetchers []*chunk.Fetcher
	}{
		{
			name: "no chunks from both",
		},
		{
			name:             "no chunks from ingester",
			storeChunks:      [][]chunk.Chunk{testChunks},
			storeFetcher:     fetchers[0:1],
			expectedChunks:   [][]chunk.Chunk{testChunks},
			expectedFetchers: fetchers[0:1],
		},
		{
			name:             "no chunks from querier",
			ingesterChunkIDs: convertChunksToChunkIDs(testChunks),
			ingesterFetcher:  fetchers[0],
			expectedChunks:   [][]chunk.Chunk{testChunks},
			expectedFetchers: fetchers[0:1],
		},
		{
			name: "nothing duplicate",
			storeChunks: [][]chunk.Chunk{
				testChunks[0:5],
			},
			storeFetcher:     fetchers[0:1],
			ingesterChunkIDs: convertChunksToChunkIDs(testChunks[5:]),
			ingesterFetcher:  fetchers[1],
			expectedChunks: [][]chunk.Chunk{
				testChunks[0:5],
				testChunks[5:],
			},
			expectedFetchers: fetchers[0:2],
		},
		{
			name: "duplicate chunks, different fetchers",
			storeChunks: [][]chunk.Chunk{
				testChunks[0:5],
				testChunks[5:],
			},
			storeFetcher:     fetchers[0:2],
			ingesterChunkIDs: convertChunksToChunkIDs(testChunks[5:]),
			ingesterFetcher:  fetchers[2],
			expectedChunks: [][]chunk.Chunk{
				testChunks[0:5],
				testChunks[5:],
			},
			expectedFetchers: fetchers[0:2],
		},
		{
			name: "different chunks, duplicate fetchers",
			storeChunks: [][]chunk.Chunk{
				testChunks[0:2],
				testChunks[2:5],
			},
			storeFetcher:     fetchers[0:2],
			ingesterChunkIDs: convertChunksToChunkIDs(testChunks[5:]),
			ingesterFetcher:  fetchers[1],
			expectedChunks: [][]chunk.Chunk{
				testChunks[0:2],
				testChunks[2:],
			},
			expectedFetchers: fetchers[0:2],
		},
		{
			name: "different chunks, different fetchers",
			storeChunks: [][]chunk.Chunk{
				testChunks[0:2],
				testChunks[2:5],
			},
			storeFetcher:     fetchers[0:2],
			ingesterChunkIDs: convertChunksToChunkIDs(testChunks[5:]),
			ingesterFetcher:  fetchers[2],
			expectedChunks: [][]chunk.Chunk{
				testChunks[0:2],
				testChunks[2:5],
				testChunks[5:],
			},
			expectedFetchers: fetchers[0:3],
		},
		{
			name: "duplicate chunks from ingesters",
			storeChunks: [][]chunk.Chunk{
				testChunks[0:5],
			},
			storeFetcher:     fetchers[0:1],
			ingesterChunkIDs: convertChunksToChunkIDs(append(testChunks[5:], testChunks[5:]...)),
			ingesterFetcher:  fetchers[0],
			expectedChunks: [][]chunk.Chunk{
				testChunks,
			},
			expectedFetchers: fetchers[0:1],
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStoreMock()
			store.On("GetChunkRefs", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(tc.storeChunks, tc.storeFetcher, nil)
			store.On("GetChunkFetcher", mock.Anything).Return(tc.ingesterFetcher)

			ingesterQuerier := newIngesterQuerierMock()
			ingesterQuerier.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(tc.ingesterChunkIDs, nil)

			asyncStore := NewAsyncStore(store, ingesterQuerier, 0)

			chunks, fetchers, err := asyncStore.GetChunkRefs(context.Background(), "fake", model.Now(), model.Now(), nil)
			require.NoError(t, err)

			require.Equal(t, tc.expectedChunks, chunks)
			require.Len(t, fetchers, len(tc.expectedFetchers))
			for i := range tc.expectedFetchers {
				require.Same(t, tc.expectedFetchers[i], fetchers[i])
			}
		})
	}
}

func TestAsyncStore_QueryIngestersWithin(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		queryIngestersWithin       time.Duration
		queryFrom, queryThrough    model.Time
		expectedIngestersQueryFrom model.Time
		shouldQueryIngester        bool
	}{
		{
			name:                "queryIngestersWithin 0, query last 12h",
			queryFrom:           model.Now().Add(-12 * time.Hour),
			queryThrough:        model.Now(),
			shouldQueryIngester: true,
		},
		{
			name:                 "queryIngestersWithin 3h, query last 12h",
			queryIngestersWithin: 3 * time.Hour,
			queryFrom:            model.Now().Add(-12 * time.Hour),
			queryThrough:         model.Now(),
			shouldQueryIngester:  true,
		},
		{
			name:                 "queryIngestersWithin 3h, query last 2h",
			queryIngestersWithin: 3 * time.Hour,
			queryFrom:            model.Now().Add(-2 * time.Hour),
			queryThrough:         model.Now(),
			shouldQueryIngester:  true,
		},
		{
			name:                 "queryIngestersWithin 3h, query upto last 3h",
			queryIngestersWithin: 3 * time.Hour,
			queryFrom:            model.Now().Add(-12 * time.Hour),
			queryThrough:         model.Now().Add(-3 * time.Hour),
			shouldQueryIngester:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStoreMock()
			store.On("GetChunkRefs", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([][]chunk.Chunk{}, []*chunk.Fetcher{}, nil)

			ingesterQuerier := newIngesterQuerierMock()
			ingesterQuerier.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

			asyncStore := NewAsyncStore(store, ingesterQuerier, tc.queryIngestersWithin)

			_, _, err := asyncStore.GetChunkRefs(context.Background(), "fake", tc.queryFrom, tc.queryThrough, nil)
			require.NoError(t, err)

			expectedNumCalls := 0
			if tc.shouldQueryIngester {
				expectedNumCalls = 1
			}
			require.Len(t, ingesterQuerier.GetMockedCallsByMethod("GetChunkIDs"), expectedNumCalls)
		})
	}
}

func convertChunksToChunkIDs(chunks []chunk.Chunk) []string {
	var chunkIDs []string
	for _, chk := range chunks {
		chunkIDs = append(chunkIDs, chk.ExternalKey())
	}

	return chunkIDs
}
