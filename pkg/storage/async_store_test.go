package storage

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util"
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

func (s *storeMock) GetChunks(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate, overrides *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	args := s.Called(ctx, userID, from, through, predicate, overrides)
	return args.Get(0).([][]chunk.Chunk), args.Get(1).([]*fetcher.Fetcher), args.Error(2)
}

func (s *storeMock) GetChunkFetcher(tm model.Time) *fetcher.Fetcher {
	args := s.Called(tm)
	return args.Get(0).(*fetcher.Fetcher)
}

func (s *storeMock) Volume(_ context.Context, userID string, from, through model.Time, _ int32, targetLabels []string, _ string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
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

func (i *ingesterQuerierMock) Volume(_ context.Context, userID string, from, through model.Time, _ int32, targetLabels []string, _ string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := i.Called(userID, from, through, targetLabels, matchers)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
}

func buildMockChunkRef(t *testing.T, num int) []chunk.Chunk {
	now := time.Now()
	var chunks []chunk.Chunk

	periodConfig := config.PeriodConfig{

		From:      config.DayTime{Time: 0},
		Schema:    "v11",
		RowShards: 16,
	}

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			periodConfig,
		},
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	for i := 0; i < num; i++ {
		chk := newChunk(chunkfmt, headfmt, buildTestStreams(fooLabelsWithName, timeRange{
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
			store.On("GetChunks", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(tc.storeChunks, tc.storeFetcher, nil)
			store.On("GetChunkFetcher", mock.Anything).Return(tc.ingesterFetcher)

			ingesterQuerier := newIngesterQuerierMock()
			ingesterQuerier.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(tc.ingesterChunkIDs, nil)

			asyncStoreCfg := AsyncStoreCfg{IngesterQuerier: ingesterQuerier}
			asyncStore := NewAsyncStore(asyncStoreCfg, store, config.SchemaConfig{})

			chunks, fetchers, err := asyncStore.GetChunks(context.Background(), "fake", model.Now(), model.Now(), chunk.NewPredicate(nil, nil), nil)
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
			store.On("GetChunks", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([][]chunk.Chunk{}, []*fetcher.Fetcher{}, nil)

			ingesterQuerier := newIngesterQuerierMock()
			ingesterQuerier.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{}, nil)

			asyncStoreCfg := AsyncStoreCfg{
				IngesterQuerier:      ingesterQuerier,
				QueryIngestersWithin: tc.queryIngestersWithin,
			}
			asyncStore := NewAsyncStore(asyncStoreCfg, store, config.SchemaConfig{})

			_, _, err := asyncStore.GetChunks(context.Background(), "fake", tc.queryFrom, tc.queryThrough, chunk.NewPredicate(nil, nil), nil)
			require.NoError(t, err)

			expectedNumCalls := 0
			if tc.shouldQueryIngester {
				expectedNumCalls = 1
			}
			require.Len(t, ingesterQuerier.GetMockedCallsByMethod("GetChunkIDs"), expectedNumCalls)
		})
	}
}

func TestVolume(t *testing.T) {
	store := newStoreMock()
	store.On("Volume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		&logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: `{foo="bar"}`, Volume: 38},
			},
			Limit: 10,
		}, nil)

	ingesterQuerier := newIngesterQuerierMock()
	ingesterQuerier.On("Volume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&logproto.VolumeResponse{
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

	vol, err := asyncStore.Volume(context.Background(), "test", model.Now().Add(-2*time.Hour), model.Now(), 10, nil, "labels", nil...)
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

func TestMergeShardsFromIngestersAndStore(t *testing.T) {
	mkStats := func(bytes, chks uint64) logproto.IndexStatsResponse {
		return logproto.IndexStatsResponse{
			Bytes:  bytes,
			Chunks: chks,
		}
	}

	// creates n shards with bytesPerShard * n bytes and chks chunks
	mkShards := func(n int, bytesPerShard uint64) logproto.ShardsResponse {
		return logproto.ShardsResponse{
			Shards: sharding.LinearShards(n, bytesPerShard*uint64(n)),
		}
	}

	targetBytesPerShard := 10

	for _, tc := range []struct {
		desc     string
		ingester logproto.IndexStatsResponse
		store    logproto.ShardsResponse
		exp      logproto.ShardsResponse
	}{
		{
			desc:     "zero bytes returns one full shard",
			ingester: mkStats(0, 0),
			store:    mkShards(0, 0),
			exp:      mkShards(1, 0),
		},
		{
			desc:     "zero ingester bytes honors store",
			ingester: mkStats(0, 0),
			store:    mkShards(10, uint64(targetBytesPerShard)),
			exp:      mkShards(10, uint64(targetBytesPerShard)),
		},
		{
			desc:     "zero store bytes honors ingester",
			ingester: mkStats(uint64(targetBytesPerShard*10), 10),
			store:    mkShards(0, 0),
			exp:      mkShards(10, uint64(targetBytesPerShard)),
		},
		{
			desc:     "ingester bytes below threshold ignored",
			ingester: mkStats(uint64(targetBytesPerShard*2), 10), // 2 shards worth from ingesters
			store:    mkShards(10, uint64(targetBytesPerShard)),  // 10 shards worth from store
			exp:      mkShards(10, uint64(targetBytesPerShard)),  // use the store's resp
		},
		{
			desc:     "ingester bytes above threshold recreate shards",
			ingester: mkStats(uint64(targetBytesPerShard*4), 10), // 4 shards worth from ingesters
			store:    mkShards(10, uint64(targetBytesPerShard)),  // 10 shards worth from store
			exp:      mkShards(14, uint64(targetBytesPerShard)),  // regenerate 14 shards
		},
	} {

		t.Run(tc.desc, func(t *testing.T) {
			got := mergeShardsFromIngestersAndStore(
				log.NewNopLogger(),
				&tc.store,
				&tc.ingester,
				uint64(targetBytesPerShard),
			)
			require.Equal(t, tc.exp.Statistics, got.Statistics)
			require.Equal(t, tc.exp.ChunkGroups, got.ChunkGroups)
			for i, shard := range tc.exp.Shards {
				require.Equal(t, shard, got.Shards[i], "shard %d", i)
			}
		})
	}
}
