package indexgateway

import (
	"context"
	"errors"
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
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	tsdb_index "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
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

func (i *indexQuerierMock) HasForSeries(from, through model.Time) (sharding.ForSeries, bool) {
	args := i.Called(from, through)
	if args.Get(0) == nil {
		return nil, args.Bool(1)
	}
	return args.Get(0).(sharding.ForSeries), args.Bool(1)
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

	// Three streams, each exactly the size of a whole shard.
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
			indexQuerier.On("HasForSeries", from, through).Return(seriesFromRefs(tc.refs), true)

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

func TestGetShardsIndexErrorDoesNotSendPartialResponse(t *testing.T) {
	sentinel := errors.New("index read failed")
	q := newIngesterQuerierMock()
	q.On("HasChunkSizingInfo", model.Time(1), model.Time(2)).Return(true)
	q.On("HasForSeries", model.Time(1), model.Time(2)).Return(sharding.ForSeriesFunc(func(context.Context, string, tsdb_index.FingerprintFilter, model.Time, model.Time, func(labels.Labels, model.Fingerprint, []tsdb_index.ChunkMeta) bool, ...*labels.Matcher) error {
		return sentinel
	}), true)
	gateway, err := NewIndexGateway(Config{}, mockLimits{}, util_log.Logger, nil, q, nil, nil)
	require.NoError(t, err)
	server := &mockGetShardsServer{ctx: user.InjectOrgID(context.Background(), "fake")}
	err = gateway.GetShards(&logproto.ShardsRequest{From: 1, Through: 2, Query: `{app="test"}`}, server)
	require.ErrorIs(t, err, sentinel)
	require.Empty(t, server.sent)
	q.AssertExpectations(t)
}
