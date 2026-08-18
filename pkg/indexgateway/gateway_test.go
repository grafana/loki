package indexgateway

import (
	"context"
	"math"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/storage/chunk"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	tsdb_index "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_test "github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type mockLimits struct {
	shardSize        int
	maxCapacity      float64
	maxBytesPerShard int
	precomputeChunks bool
}

func (l mockLimits) IndexGatewayShardSize(_ string) int       { return l.shardSize }
func (l mockLimits) IndexGatewayMaxCapacity(_ string) float64 { return l.maxCapacity }
func (l mockLimits) TSDBMaxBytesPerShard(_ string) int        { return l.maxBytesPerShard }
func (l mockLimits) TSDBPrecomputeChunks(_ string) bool       { return l.precomputeChunks }

func TestVolume(t *testing.T) {
	indexQuerier := newIngesterQuerierMock()
	indexQuerier.On("Volume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&logproto.VolumeResponse{Volumes: []logproto.Volume{
		{Name: "bar", Volume: 38},
	}}, nil)

	gateway, err := NewIndexGateway(Config{}, mockLimits{}, util_log.Logger, nil, indexQuerier, nil, nil)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	vol, err := gateway.GetVolume(ctx, &logproto.VolumeRequest{Matchers: "{}"})
	require.NoError(t, err)

	require.Equal(t, &logproto.VolumeResponse{Volumes: []logproto.Volume{
		{Name: "bar", Volume: 38},
	}}, vol)
}

func TestNewIndexGateway_DataObjectSectionsEnabledWithoutMetastore(t *testing.T) {
	// Enabling the feature without an injected metastore is a wiring bug: it must fail startup loudly
	// rather than silently answer every RPC with Unimplemented and make queriers fall back.
	_, err := NewIndexGateway(
		Config{DataObjectSections: DataObjectSectionsConfig{Enabled: true}},
		mockLimits{}, util_log.Logger, nil, newIngesterQuerierMock(), nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no metastore was injected")
}

type indexQuerierMock struct {
	IndexQuerier
	util_test.ExtendedMock
}

func newIngesterQuerierMock() *indexQuerierMock {
	return &indexQuerierMock{}
}

func (i *indexQuerierMock) Volume(_ context.Context, userID string, from, through model.Time, _ int32, _ []string, _ string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := i.Called(userID, from, through, matchers)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
}

func (i *indexQuerierMock) HasChunkSizingInfo(from, through model.Time) bool {
	args := i.Called(from, through)
	return args.Bool(0)
}

func (i *indexQuerierMock) GetShards(
	_ context.Context,
	userID string,
	from, through model.Time,
	targetBytesPerShard uint64,
	_ chunk.Predicate,
) (*logproto.ShardsResponse, error) {
	args := i.Called(userID, from, through, targetBytesPerShard)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*logproto.ShardsResponse), args.Error(1)
}

func (i *indexQuerierMock) GetChunkRefsWithSizingInfo(
	_ context.Context,
	userID string,
	from, through model.Time,
	_ chunk.Predicate,
) ([]logproto.ChunkRefWithSizingInfo, error) {
	args := i.Called(userID, from, through)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]logproto.ChunkRefWithSizingInfo), args.Error(1)
}

// Tests for various cases of the `refWithSizingInfo.Cmp` function
func TestRefWithSizingInfo(t *testing.T) {
	for _, tc := range []struct {
		desc string
		a    refWithSizingInfo
		b    tsdb_index.ChunkMeta
		exp  v2.Ord
	}{
		{
			desc: "less by from",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					From: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				MinTime: 2,
			},
			exp: v2.Less,
		},
		{
			desc: "eq by from",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					From: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				MinTime: 1,
			},
			exp: v2.Eq,
		},
		{
			desc: "gt by from",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					From: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				MinTime: 1,
			},
			exp: v2.Greater,
		},
		{
			desc: "less by through",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Through: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				MaxTime: 2,
			},
			exp: v2.Less,
		},
		{
			desc: "eq by through",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Through: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				MaxTime: 2,
			},
			exp: v2.Eq,
		},
		{
			desc: "gt by through",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Through: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				MaxTime: 1,
			},
			exp: v2.Greater,
		},
		{
			desc: "less by checksum",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Checksum: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				Checksum: 2,
			},
			exp: v2.Less,
		},
		{
			desc: "eq by checksum",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Checksum: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				Checksum: 2,
			},
			exp: v2.Eq,
		},
		{
			desc: "gt by checksum",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Checksum: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				Checksum: 1,
			},
			exp: v2.Greater,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.a.Cmp(tc.b))
		})
	}
}

// TODO(owen-d): more testing for specific cases
func TestAccumulateChunksToShards(t *testing.T) {
	// only check eq by checksum for convenience -- we're not testing the comparison function here
	mkRef := func(fp model.Fingerprint, checksum uint32) logproto.ChunkRef {
		return logproto.ChunkRef{
			Fingerprint: uint64(fp),
			Checksum:    checksum,
		}
	}

	sized := func(ref logproto.ChunkRef, kb, entries uint32) logproto.ChunkRefWithSizingInfo {
		return logproto.ChunkRefWithSizingInfo{
			ChunkRef: ref,
			KB:       kb,
			Entries:  entries,
		}

	}

	filtered := []logproto.ChunkRefWithSizingInfo{
		// shard 0
		sized(mkRef(1, 0), 100, 1),
		sized(mkRef(1, 1), 100, 1),
		sized(mkRef(1, 2), 100, 1),

		// shard 1
		sized(mkRef(2, 10), 100, 1),
		sized(mkRef(2, 20), 100, 1),
		sized(mkRef(2, 30), 100, 1),

		// shard 2 split across multiple series
		sized(mkRef(3, 10), 50, 1),
		sized(mkRef(4, 10), 30, 1),
		sized(mkRef(4, 20), 30, 1),

		// last shard contains leftovers + skip a few fps in between
		sized(mkRef(7, 10), 25, 1),
	}

	shards, grps, err := accumulateChunksToShards(&logproto.ShardsRequest{
		TargetBytesPerShard: 100 << 10,
	}, filtered)

	expectedChks := [][]logproto.ChunkRefWithSizingInfo{
		filtered[0:3],
		filtered[3:6],
		filtered[6:9],
		filtered[9:10],
	}
	exp := []logproto.Shard{
		{
			Bounds: logproto.FPBounds{Min: 0, Max: 1},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  3,
				Entries: 3,
				Bytes:   300 << 10,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: 2, Max: 2},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  3,
				Entries: 3,
				Bytes:   300 << 10,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: 3, Max: 6},
			Stats: &logproto.IndexStatsResponse{
				Streams: 2,
				Chunks:  3,
				Entries: 3,
				Bytes:   110 << 10,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: 7, Max: math.MaxUint64},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  1,
				Entries: 1,
				Bytes:   25 << 10,
			},
		},
	}

	require.NoError(t, err)

	for i := range shards {
		require.Equal(t, exp[i], shards[i], "invalid shard at index %d", i)
		for j := range grps[i].Refs {
			require.Equal(t, &expectedChks[i][j].ChunkRef, grps[i].Refs[j], "invalid chunk in grp %d at index %d", i, j)
		}
	}
	require.Equal(t, len(exp), len(shards))

}

type mockGetShardsServer struct {
	ctx  context.Context
	sent []*logproto.ShardsResponse
}

var _ logproto.IndexGateway_GetShardsServer = (*mockGetShardsServer)(nil)

func (s *mockGetShardsServer) Send(response *logproto.ShardsResponse) error {
	s.sent = append(s.sent, response)
	return nil
}

func (s *mockGetShardsServer) Context() context.Context { return s.ctx }

func (s *mockGetShardsServer) SetHeader(_ metadata.MD) error  { panic("unused") }
func (s *mockGetShardsServer) SendHeader(_ metadata.MD) error { panic("unused") }
func (s *mockGetShardsServer) SetTrailer(_ metadata.MD)       { panic("unused") }
func (s *mockGetShardsServer) SendMsg(_ any) error            { panic("unused") }
func (s *mockGetShardsServer) RecvMsg(_ any) error            { panic("unused") }

// sendShards runs a shard request against a gateway backed by indexQuerier and returns the
// single response it streamed back.
func sendShards(t *testing.T, indexQuerier IndexQuerier, tenant string, precomputeChunks bool, req *logproto.ShardsRequest) *logproto.ShardsResponse {
	t.Helper()

	gateway, err := NewIndexGateway(Config{}, mockLimits{precomputeChunks: precomputeChunks}, util_log.Logger, nil, indexQuerier, nil, nil)
	require.NoError(t, err)

	server := &mockGetShardsServer{ctx: user.InjectOrgID(context.Background(), tenant)}
	require.NoError(t, gateway.GetShards(req, server))
	require.Len(t, server.sent, 1)

	return server.sent[0]
}

func TestGetShards(t *testing.T) {
	const (
		tenant = "fake"
		query  = `{service_name=~".+"}`

		// Every chunk in the fixtures below carries the same number of entries; only
		// the byte size varies, since that is what drives sharding.
		chunkEntries = 10

		// Shard sizes are accumulated from per-chunk KB, so express the target the same
		// way and derive the chunk sizes from it.
		targetKB            = 100
		targetBytesPerShard = targetKB << 10

		smallChunkKB = 4
	)

	var (
		from    = model.Time(1000)
		through = model.Time(2000)

		// Fingerprints of the streams used by the multi-shard fixture
		fpA = model.Fingerprint(1)
		fpB = fpA + 1
		fpC = fpB + 1
	)

	mkRef := func(fp model.Fingerprint, checksum, kb uint32) logproto.ChunkRefWithSizingInfo {
		return logproto.ChunkRefWithSizingInfo{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(fp),
				UserID:      tenant,
				From:        from,
				Through:     through,
				Checksum:    checksum,
			},
			KB:      kb,
			Entries: chunkEntries,
		}
	}

	// When the index matches nothing, the gateway still has to answer with a shard so the
	// querier has something to execute against.
	noStreamRefs := []logproto.ChunkRefWithSizingInfo{}

	noStreamShards := []logproto.Shard{
		{
			Bounds: logproto.FPBounds{Min: 0, Max: math.MaxUint64},
			Stats:  &logproto.IndexStatsResponse{},
		},
	}

	// A single small stream, which fits in one shard covering the whole keyspace.
	oneStreamChunk := mkRef(fpA, 1, smallChunkKB)
	oneStreamRefs := []logproto.ChunkRefWithSizingInfo{oneStreamChunk}

	oneStreamShards := []logproto.Shard{
		{
			Bounds: logproto.FPBounds{Min: 0, Max: math.MaxUint64},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  1,
				Bytes:   smallChunkKB << 10,
				Entries: chunkEntries,
			},
		},
	}

	oneStreamGroups := []logproto.ChunkRefGroup{
		{Refs: []*logproto.ChunkRef{&oneStreamChunk.ChunkRef}},
	}

	// Three streams, each exactly the size of a whole shard, so none of them can share
	// one. The refs must be ordered by fingerprint, as the index returns them.
	streamAChunk1 := mkRef(fpA, 1, targetKB/2)
	streamAChunk2 := mkRef(fpA, 2, targetKB/2)
	streamBChunk := mkRef(fpB, 1, targetKB)
	streamCChunk := mkRef(fpC, 1, targetKB)
	manyStreamRefs := []logproto.ChunkRefWithSizingInfo{streamAChunk1, streamAChunk2, streamBChunk, streamCChunk}

	manyStreamShards := []logproto.Shard{
		{
			// Starts at the beginning of the keyspace and stops just short of the next stream.
			Bounds: logproto.FPBounds{Min: 0, Max: fpB - 1},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  2,
				Bytes:   targetBytesPerShard,
				Entries: 2 * chunkEntries,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: fpB, Max: fpC - 1},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  1,
				Bytes:   targetBytesPerShard,
				Entries: chunkEntries,
			},
		},
		{
			// The last shard always extends to the end of the keyspace.
			Bounds: logproto.FPBounds{Min: fpC, Max: math.MaxUint64},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  1,
				Bytes:   targetBytesPerShard,
				Entries: chunkEntries,
			},
		},
	}

	// One group per shard, holding that shard's chunks.
	manyStreamGroups := []logproto.ChunkRefGroup{
		{Refs: []*logproto.ChunkRef{&streamAChunk1.ChunkRef, &streamAChunk2.ChunkRef}},
		{Refs: []*logproto.ChunkRef{&streamBChunk.ChunkRef}},
		{Refs: []*logproto.ChunkRef{&streamCChunk.ChunkRef}},
	}

	for _, tc := range []struct {
		desc             string
		refs             []logproto.ChunkRefWithSizingInfo
		precomputeChunks bool
		expectedShards   []logproto.Shard
		expectedGroups   []logproto.ChunkRefGroup
	}{
		{
			desc:             "no chunks yields a single empty shard covering the whole keyspace",
			refs:             noStreamRefs,
			precomputeChunks: true,
			expectedShards:   noStreamShards,
			expectedGroups:   nil,
		},
		{
			desc:             "single shard, chunk refs are discarded when precomputing is disabled",
			refs:             oneStreamRefs,
			precomputeChunks: false,
			expectedShards:   oneStreamShards,
			expectedGroups:   nil,
		},
		{
			desc:             "single shard, chunk refs are returned alongside the shards when precomputing is enabled",
			refs:             oneStreamRefs,
			precomputeChunks: true,
			expectedShards:   oneStreamShards,
			expectedGroups:   oneStreamGroups,
		},
		{
			desc:             "multiple shards, chunk refs are discarded when precomputing is disabled",
			refs:             manyStreamRefs,
			precomputeChunks: false,
			expectedShards:   manyStreamShards,
			expectedGroups:   nil,
		},
		{
			desc:             "multiple shards, chunk refs are grouped per shard when precomputing is enabled",
			refs:             manyStreamRefs,
			precomputeChunks: true,
			expectedShards:   manyStreamShards,
			expectedGroups:   manyStreamGroups,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			indexQuerier := newIngesterQuerierMock()
			indexQuerier.On("HasChunkSizingInfo", from, through).Return(true)
			indexQuerier.On("GetChunkRefsWithSizingInfo", tenant, from, through).Return(tc.refs, nil)

			resp := sendShards(t, indexQuerier, tenant, tc.precomputeChunks, &logproto.ShardsRequest{
				From:                from,
				Through:             through,
				Query:               query,
				TargetBytesPerShard: targetBytesPerShard,
			})

			require.Equal(t, tc.expectedShards, resp.Shards)
			require.Equal(t, tc.expectedGroups, resp.ChunkGroups)

			// Every stream lands in exactly one shard, so the index-level stream count
			// must add up to the per-shard counts.
			var expectedStreams int64
			for _, shard := range tc.expectedShards {
				expectedStreams += int64(shard.Stats.Streams)
			}

			require.Equal(t, int64(len(tc.refs)), resp.Statistics.Index.TotalChunks)
			require.Equal(t, expectedStreams, resp.Statistics.Index.TotalStreams)
			require.Positive(t, resp.Statistics.Index.ShardsDuration)

			indexQuerier.AssertExpectations(t)
		})
	}
}

// When the index has no chunk sizing info the gateway can't compute shards itself, so it
// delegates to the index querier and forwards that response untouched.
func TestGetShardsWithoutChunkSizingInfo(t *testing.T) {
	const (
		tenant              = "fake"
		query               = `{service_name=~".+"}`
		targetBytesPerShard = 100 << 10
	)

	var (
		from    = model.Time(1000)
		through = model.Time(2000)
	)

	// The response the index querier computed on the gateway's behalf. Its contents are
	// arbitrary, all the test cares about is
	// that they come back unchanged.
	// It is built by a constructor so the querier's copy and the expected copy are
	// distinct objects.
	fallbackResponse := func() *logproto.ShardsResponse {
		return &logproto.ShardsResponse{
			Shards: []logproto.Shard{
				{
					Bounds: logproto.FPBounds{Min: 0, Max: math.MaxUint64},
					Stats:  &logproto.IndexStatsResponse{Streams: 1, Chunks: 2},
				},
			},
			ChunkGroups: []logproto.ChunkRefGroup{
				{Refs: []*logproto.ChunkRef{{Checksum: 1}}},
			},
			Statistics: stats.Result{
				Index: stats.Index{TotalChunks: 2, TotalStreams: 1},
			},
		}
	}

	indexQuerier := newIngesterQuerierMock()
	indexQuerier.On("HasChunkSizingInfo", from, through).Return(false)
	indexQuerier.On("GetShards", tenant, from, through, uint64(targetBytesPerShard)).Return(fallbackResponse(), nil)

	resp := sendShards(t, indexQuerier, tenant, false, &logproto.ShardsRequest{
		From:                from,
		Through:             through,
		Query:               query,
		TargetBytesPerShard: targetBytesPerShard,
	})

	require.Equal(t, fallbackResponse(), resp)

	// The gateway must delegate instead of resolving chunk refs itself.
	indexQuerier.AssertNotCalled(t, "GetChunkRefsWithSizingInfo", mock.Anything, mock.Anything, mock.Anything)
	indexQuerier.AssertExpectations(t)
}
