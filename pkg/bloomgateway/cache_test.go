package bloomgateway

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Range is 1000-4000
var templateResponse = &logproto.FilterChunkRefResponse{
	ChunkRefs: []*logproto.GroupedChunkRefs{
		{
			Fingerprint: 1,
			Tenant:      "fake",
			Refs: []*logproto.ShortRef{
				{
					From:     1000,
					Through:  1500,
					Checksum: 10,
				},
				{
					From:     1500,
					Through:  2500,
					Checksum: 20,
				},
			},
		},
		{
			Fingerprint: 2,
			Tenant:      "fake",
			Refs: []*logproto.ShortRef{
				{
					From:     3000,
					Through:  4000,
					Checksum: 30,
				},
				{
					From:     1000,
					Through:  3000,
					Checksum: 40,
				},
			},
		},
	},
}

func TestExtract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		start    int64
		end      int64
		input    *logproto.FilterChunkRefResponse
		expected *logproto.FilterChunkRefResponse
	}{
		{
			name:  "start and end out of range",
			start: 100,
			end:   200,
			input: templateResponse,
			expected: &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{},
			},
		},
		{
			name:     "start spans exact range",
			start:    1000,
			end:      4000,
			input:    templateResponse,
			expected: templateResponse,
		},
		{
			name:     "start spans more than range",
			start:    100,
			end:      5000,
			input:    templateResponse,
			expected: templateResponse,
		},
		{
			name:  "start and end within range",
			start: 1700,
			end:   2700,
			input: templateResponse,
			expected: &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 1,
						Tenant:      "fake",
						Refs: []*logproto.ShortRef{
							{
								From:     1500,
								Through:  2500,
								Checksum: 20,
							},
						},
					},
					{
						Fingerprint: 2,
						Tenant:      "fake",
						Refs: []*logproto.ShortRef{
							{
								From:     1000,
								Through:  3000,
								Checksum: 40,
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newExtractor()
			actual := e.Extract(tc.start, tc.end, tc.input, 0, 0)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestMerge(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    []*logproto.FilterChunkRefResponse
		expected *logproto.FilterChunkRefResponse
	}{
		{
			name:  "empy input",
			input: []*logproto.FilterChunkRefResponse{},
			expected: &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{},
			},
		},
		{
			name:     "single input",
			input:    []*logproto.FilterChunkRefResponse{templateResponse},
			expected: templateResponse,
		},
		{
			name: "repeating and non-repeating fingerprint with repeating and non-repeating chunks",
			input: []*logproto.FilterChunkRefResponse{
				{
					ChunkRefs: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 1,
							Tenant:      "fake",
							Refs: []*logproto.ShortRef{
								{
									From:     1000,
									Through:  1500,
									Checksum: 10,
								},
								{
									From:     1500,
									Through:  2500,
									Checksum: 20,
								},
							},
						},
						{
							Fingerprint: 2,
							Tenant:      "fake",
							Refs: []*logproto.ShortRef{
								{
									From:     1000,
									Through:  1500,
									Checksum: 10,
								},
								{
									From:     1500,
									Through:  2500,
									Checksum: 20,
								},
							},
						},
					},
				},
				{
					ChunkRefs: []*logproto.GroupedChunkRefs{
						// Same FP as in previous input and same chunks
						{
							Fingerprint: 1,
							Tenant:      "fake",
							Refs: []*logproto.ShortRef{
								{
									From:     1000,
									Through:  1500,
									Checksum: 10,
								},
								{
									From:     1500,
									Through:  2500,
									Checksum: 20,
								},
							},
						},
						// Same FP as in previous input, but different chunks
						{
							Fingerprint: 2,
							Tenant:      "fake",
							Refs: []*logproto.ShortRef{
								// Same chunk as in previous input
								{
									From:     1500,
									Through:  2500,
									Checksum: 20,
								},
								// New chunk
								{
									From:     2000,
									Through:  2500,
									Checksum: 30,
								},
							},
						},
						// New FP
						{
							Fingerprint: 3,
							Tenant:      "fake",
							Refs: []*logproto.ShortRef{
								{
									From:     1000,
									Through:  1500,
									Checksum: 10,
								},
								{
									From:     1500,
									Through:  2500,
									Checksum: 20,
								},
							},
						},
					},
				},
				{
					ChunkRefs: []*logproto.GroupedChunkRefs{
						// Same FP as in previous input and diff chunks
						{
							Fingerprint: 2,
							Tenant:      "fake",
							Refs: []*logproto.ShortRef{
								{
									From:     700,
									Through:  1000,
									Checksum: 40,
								},
								{
									From:     2000,
									Through:  2700,
									Checksum: 50,
								},
							},
						},
					},
				},
			},
			expected: &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 1,
						Tenant:      "fake",
						Refs: []*logproto.ShortRef{
							{
								From:     1000,
								Through:  1500,
								Checksum: 10,
							},
							{
								From:     1500,
								Through:  2500,
								Checksum: 20,
							},
						},
					},
					{
						Fingerprint: 2,
						Tenant:      "fake",
						Refs: []*logproto.ShortRef{
							{
								From:     700,
								Through:  1000,
								Checksum: 40,
							},
							{
								From:     1000,
								Through:  1500,
								Checksum: 10,
							},
							{
								From:     1500,
								Through:  2500,
								Checksum: 20,
							},
							{
								From:     2000,
								Through:  2500,
								Checksum: 30,
							},
							{
								From:     2000,
								Through:  2700,
								Checksum: 50,
							},
						},
					},
					{
						Fingerprint: 3,
						Tenant:      "fake",
						Refs: []*logproto.ShortRef{
							{
								From:     1000,
								Through:  1500,
								Checksum: 10,
							},
							{
								From:     1500,
								Through:  2500,
								Checksum: 20,
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := make([]resultscache.Response, 0, len(tc.input))
			for _, i := range tc.input {
				input = append(input, i)
			}

			m := newMerger()
			actual, err := m.MergeResponse(input...)
			require.NoError(t, err)

			resp, ok := actual.(*logproto.FilterChunkRefResponse)
			require.True(t, ok)
			require.Equal(t, tc.expected, resp)
		})
	}
}

func TestCache(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")

	limits := mockLimits{
		cacheInterval: 15 * time.Minute,
	}

	cfg := CacheConfig{
		Config: resultscache.Config{
			CacheConfig: cache.Config{
				Cache: cache.NewMockCache(),
			},
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.BloomFilterCache, constants.Loki)
	require.NoError(t, err)
	defer c.Stop()

	chunkRefs := []*logproto.ChunkRef{
		{
			Fingerprint: 2,
			UserID:      "fake",
			From:        1500,
			Through:     2500,
			Checksum:    30,
		},
		{
			Fingerprint: 3,
			UserID:      "fake",
			From:        2500,
			Through:     3500,
		},
	}
	expr, err := syntax.ParseExpr(`{foo="bar"} |= "does not match"`)
	require.NoError(t, err)
	req := &logproto.FilterChunkRefRequest{
		From:    model.Time(2000),
		Through: model.Time(3000),
		Refs:    groupRefs(t, chunkRefs),
		Plan:    plan.QueryPlan{AST: expr},
	}
	expectedRes := &logproto.FilterChunkRefResponse{
		ChunkRefs: groupRefs(t, chunkRefs),
	}

	server, calls := newMockServer(expectedRes)

	cacheMiddleware := NewBloomGatewayClientCacheMiddleware(
		log.NewNopLogger(),
		server,
		c,
		limits,
		nil,
		false,
	)

	// First call should go to the server
	*calls = 0
	res, err := cacheMiddleware.FilterChunkRefs(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.Equal(t, expectedRes, res)

	// Second call should go to the cache
	*calls = 0
	res, err = cacheMiddleware.FilterChunkRefs(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, *calls)
	require.Equal(t, expectedRes, res)

	// Doing a request with new start and end should:
	// 1. hit the server the leading time
	// 2. hit the cache the cached span
	// 3. hit the server for the trailing time
	newChunkRefs := []*logproto.ChunkRef{
		{
			Fingerprint: 1,
			UserID:      "fake",
			From:        1000,
			Through:     1500,
			Checksum:    10,
		},
		{
			Fingerprint: 4,
			UserID:      "fake",
			From:        3500,
			Through:     4500,
		},
	}
	server.SetResponse(&logproto.FilterChunkRefResponse{
		ChunkRefs: groupRefs(t, newChunkRefs),
	})
	expectedRes = &logproto.FilterChunkRefResponse{
		ChunkRefs: groupRefs(t, append(chunkRefs, newChunkRefs...)),
	}
	req.From = model.Time(100)
	req.Through = model.Time(5000)
	*calls = 0
	res, err = cacheMiddleware.FilterChunkRefs(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 2, *calls)
	require.ElementsMatch(t, expectedRes.ChunkRefs, res.ChunkRefs)

	// Doing a request again should only hit the cache
	*calls = 0
	res, err = cacheMiddleware.FilterChunkRefs(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, *calls)
	require.ElementsMatch(t, expectedRes.ChunkRefs, res.ChunkRefs)
}

type mockServer struct {
	calls *int
	res   *logproto.FilterChunkRefResponse
}

func newMockServer(res *logproto.FilterChunkRefResponse) (*mockServer, *int) {
	var calls int
	return &mockServer{
		calls: &calls,
		res:   res,
	}, &calls
}

func (s *mockServer) SetResponse(res *logproto.FilterChunkRefResponse) {
	s.res = res
}

func (s *mockServer) FilterChunkRefs(_ context.Context, _ *logproto.FilterChunkRefRequest, _ ...grpc.CallOption) (*logproto.FilterChunkRefResponse, error) {
	*s.calls++
	return s.res, nil
}

type mockLimits struct {
	cacheFreshness time.Duration
	cacheInterval  time.Duration
}

func (m mockLimits) MaxCacheFreshness(_ context.Context, _ string) time.Duration {
	return m.cacheFreshness
}

func (m mockLimits) BloomGatewayCacheKeyInterval(_ string) time.Duration {
	return m.cacheInterval
}
