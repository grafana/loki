package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

// testLimits wraps mockLimits and allows overriding the bucket interval.
type testLimits struct {
	*mockLimits
	interval time.Duration
}

func (m testLimits) EngineResultsCacheTimeBucketInterval(_ string) time.Duration {
	return m.interval
}

func TestMetricCacheKeyGenerator(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		reqA     resultscache.Request
		reqB     resultscache.Request
		wantSame bool
	}{
		{
			name:     "same start falls in same bucket",
			interval: time.Hour,
			reqA: &queryrange.LokiRequest{
				Query:   `{app="test"}`,
				StartTs: time.Unix(3600, 0),
				Step:    60000,
			},
			reqB: &queryrange.LokiRequest{
				Query:   `{app="test"}`,
				StartTs: time.Unix(3600, 0),
				Step:    60000,
			},
			wantSame: true,
		},
		{
			name:     "different start times within same bucket share key",
			interval: time.Hour,
			reqA: &queryrange.LokiRequest{
				Query:   `{app="test"}`,
				StartTs: time.Unix(3600, 0),
				Step:    60000,
			},
			reqB: &queryrange.LokiRequest{
				Query:   `{app="test"}`,
				StartTs: time.Unix(3700, 0),
				Step:    60000,
			},
			wantSame: true,
		},
		{
			name:     "start times in different buckets produce different keys",
			interval: time.Hour,
			reqA: &queryrange.LokiRequest{
				Query:   `{app="test"}`,
				StartTs: time.Unix(3600, 0),
				Step:    60000,
			},
			reqB: &queryrange.LokiRequest{
				Query:   `{app="test"}`,
				StartTs: time.Unix(7200, 0),
				Step:    60000,
			},
			wantSame: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			splitter := MetricCacheKeyGenerator{limits: testLimits{mockLimits: &mockLimits{}, interval: tc.interval}}
			keyA := splitter.GenerateCacheKey(context.Background(), "tenant1", tc.reqA)
			keyB := splitter.GenerateCacheKey(context.Background(), "tenant1", tc.reqB)
			if tc.wantSame {
				require.Equal(t, keyA, keyB)
			} else {
				require.NotEqual(t, keyA, keyB)
			}
		})
	}

	t.Run("key changes when interval changes", func(t *testing.T) {
		req := &queryrange.LokiRequest{
			Query:   `{app="test"}`,
			StartTs: time.Unix(3600, 0),
			Step:    60000,
		}
		splitter1h := MetricCacheKeyGenerator{limits: testLimits{mockLimits: &mockLimits{}, interval: time.Hour}}
		splitter2h := MetricCacheKeyGenerator{limits: testLimits{mockLimits: &mockLimits{}, interval: 2 * time.Hour}}
		key1h := splitter1h.GenerateCacheKey(context.Background(), "tenant1", req)
		key2h := splitter2h.GenerateCacheKey(context.Background(), "tenant1", req)
		require.NotEqual(t, key1h, key2h)
	})
}

func TestShouldCacheRequest(t *testing.T) {
	// cacheHandler.Do already routes queries to the correct middleware; this
	// function only checks whether caching is explicitly disabled for the request.
	for _, tc := range []struct {
		name     string
		disabled bool
		want     bool
	}{
		{
			name: "caching enabled",
			want: true,
		},
		{
			name:     "caching disabled",
			disabled: true,
			want:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &queryrange.LokiRequest{
				Query:          `{app="test"}`,
				CachingOptions: resultscache.CachingOptions{Disabled: tc.disabled},
			}
			got := shouldCacheRequest(context.Background(), req)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLogCacheKeyGenerator(t *testing.T) {
	for _, tc := range []struct {
		name       string
		interval   time.Duration
		tenantIDsA []string
		tenantIDsB []string
		reqA       *queryrange.LokiRequest
		reqB       *queryrange.LokiRequest
		// wantExact, when non-nil, asserts the exact key produced from reqA/tenantIDsA.
		// When nil, keyA and keyB are compared using wantSame.
		wantExact *string
		wantSame  bool
	}{
		{
			name:       "zero limit returns empty key",
			interval:   time.Hour,
			tenantIDsA: []string{"tenant1"},
			reqA:       &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(3600, 0), Limit: 0},
			wantExact:  new(string), // expects ""
		},
		{
			name:       "start times within the same bucket share a key",
			interval:   time.Hour,
			tenantIDsA: []string{"tenant1"},
			tenantIDsB: []string{"tenant1"},
			// 3600s and 7199s both fall in the bucket [3600, 7200)
			reqA:     &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(3600, 0), Limit: 100},
			reqB:     &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(7199, 0), Limit: 100},
			wantSame: true,
		},
		{
			name:       "start times in different buckets produce different keys",
			interval:   time.Hour,
			tenantIDsA: []string{"tenant1"},
			tenantIDsB: []string{"tenant1"},
			// 3600s is in bucket [3600, 7200); 7200s starts the next bucket [7200, 10800)
			reqA:     &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(3600, 0), Limit: 100},
			reqB:     &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(7200, 0), Limit: 100},
			wantSame: false,
		},
		{
			name:       "different tenants produce different keys",
			interval:   time.Hour,
			tenantIDsA: []string{"tenantA"},
			tenantIDsB: []string{"tenantB"},
			reqA:       &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(3600, 0), Limit: 100},
			reqB:       &queryrange.LokiRequest{Query: `{app="test"}`, StartTs: time.Unix(3600, 0), Limit: 100},
			wantSame:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := &LogCacheKeyGenerator{
				limits: testLimits{mockLimits: &mockLimits{}, interval: tc.interval},
			}
			if tc.wantExact != nil {
				key := gen.GenerateCacheKey(context.Background(), tc.tenantIDsA, tc.reqA)
				require.Equal(t, *tc.wantExact, key)
				return
			}
			keyA := gen.GenerateCacheKey(context.Background(), tc.tenantIDsA, tc.reqA)
			keyB := gen.GenerateCacheKey(context.Background(), tc.tenantIDsB, tc.reqB)
			if tc.wantSame {
				require.Equal(t, keyA, keyB)
			} else {
				require.NotEqual(t, keyA, keyB)
			}
		})
	}
}
