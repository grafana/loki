package storage

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
)

// storeMock is a mockable version of Loki's storage, used in querier unit tests
// to control the behaviour of the store without really hitting any storage backend
type storeMock struct {
	Store
	util.ExtendedMock
}

func newStoreMock() *storeMock {
	return &storeMock{}
}

func (s *storeMock) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	args := s.Called(ctx, userID, from, through, matchers)
	return args.Get(0).([][]chunk.Chunk), args.Get(1).([]*fetcher.Fetcher), args.Error(2)
}

func (s *storeMock) GetChunkFetcher(tm model.Time) *fetcher.Fetcher {
	args := s.Called(tm)
	return args.Get(0).(*fetcher.Fetcher)
}

func (s *storeMock) SeriesVolume(_ context.Context, userID string, from, through model.Time, _ int32, targetLabels []string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := s.Called(userID, from, through, targetLabels, matchers)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
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

func (i *ingesterQuerierMock) SeriesVolume(_ context.Context, userID string, from, through model.Time, _ int32, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := i.Called(userID, from, through, matchers)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
}

func buildMockChunkRef(t *testing.T, num int) []chunk.Chunk {
	now := time.Now()
	var chunks []chunk.Chunk

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}

	for i := 0; i < num; i++ {
		chk := newChunk(buildTestStreams(fooLabelsWithName, timeRange{
			from: now.Add(time.Duration(i) * time.Minute),
			to:   now.Add(time.Duration(i+1) * time.Minute),
		}))

		chunkRef, err := chunk.ParseExternalKey(chk.UserID, s.ExternalKey(chk.ChunkRef))
		require.NoError(t, err)

		chunks = append(chunks, chunkRef)
	}

	return chunks
}

func buildMockFetchers(num int) []*fetcher.Fetcher {
	var fetchers []*fetcher.Fetcher
	for i := 0; i < num; i++ {
		fetchers = append(fetchers, &fetcher.Fetcher{})
	}

	return fetchers
}

func TestAsyncStore_mergeIngesterAndStoreChunks(t *testing.T) {
	testChunks := buildMockChunkRef(t, 10)
	fetchers := buildMockFetchers(3)

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}

	for _, tc := range []struct {
		name             string
		storeChunks      [][]chunk.Chunk
		storeFetcher     []*fetcher.Fetcher
		ingesterChunkIDs []string
		ingesterFetcher  *fetcher.Fetcher
		expectedChunks   [][]chunk.Chunk
		expectedFetchers []*fetcher.Fetcher
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
			ingesterChunkIDs: convertChunksToChunkIDs(s, testChunks),
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
			ingesterChunkIDs: convertChunksToChunkIDs(s, testChunks[5:]),
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
			ingesterChunkIDs: convertChunksToChunkIDs(s, testChunks[5:]),
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
			ingesterChunkIDs: convertChunksToChunkIDs(s, testChunks[5:]),
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
			ingesterChunkIDs: convertChunksToChunkIDs(s, testChunks[5:]),
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
			ingesterChunkIDs: convertChunksToChunkIDs(s, append(testChunks[5:], testChunks[5:]...)),
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

			asyncStoreCfg := AsyncStoreCfg{IngesterQuerier: ingesterQuerier}
			asyncStore := NewAsyncStore(asyncStoreCfg, store, config.SchemaConfig{})

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
			store.On("GetChunkRefs", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([][]chunk.Chunk{}, []*fetcher.Fetcher{}, nil)

			ingesterQuerier := newIngesterQuerierMock()
			ingesterQuerier.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

			asyncStoreCfg := AsyncStoreCfg{
				IngesterQuerier:      ingesterQuerier,
				QueryIngestersWithin: tc.queryIngestersWithin,
			}
			asyncStore := NewAsyncStore(asyncStoreCfg, store, config.SchemaConfig{})

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

func TestSeriesVolume(t *testing.T) {
	store := newStoreMock()
	store.On("SeriesVolume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		&logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: `{foo="bar"}`, Volume: 38},
			},
			Limit: 10,
		}, nil)

	ingesterQuerier := newIngesterQuerierMock()
	ingesterQuerier.On("SeriesVolume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{bar="baz"}`, Volume: 38},
		},
		Limit: 10,
	}, nil)

	asyncStoreCfg := AsyncStoreCfg{
		IngesterQuerier:      ingesterQuerier,
		QueryIngestersWithin: 3 * time.Hour,
	}
	asyncStore := NewAsyncStore(asyncStoreCfg, store, config.SchemaConfig{})

	vol, err := asyncStore.SeriesVolume(context.Background(), "test", model.Now().Add(-2*time.Hour), model.Now(), 10, nil, nil...)
	require.NoError(t, err)

	require.Equal(t, &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{bar="baz"}`, Volume: 38},
			{Name: `{foo="bar"}`, Volume: 38},
		},
		Limit: 10,
	}, vol)
}

func convertChunksToChunkIDs(s config.SchemaConfig, chunks []chunk.Chunk) []string {
	var chunkIDs []string
	for _, chk := range chunks {
		chunkIDs = append(chunkIDs, s.ExternalKey(chk.ChunkRef))
	}

	return chunkIDs
}
