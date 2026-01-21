package engine

import (
	"net/http"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/validation"
)

type fakeRetentionLimits struct {
	retentionPeriods map[string]time.Duration
	streamRetentions map[string][]validation.StreamRetention
}

func (f fakeRetentionLimits) RetentionPeriod(userID string) time.Duration {
	return f.retentionPeriods[userID]
}

func (f fakeRetentionLimits) StreamRetention(userID string) []validation.StreamRetention {
	return f.streamRetentions[userID]
}

type fakeParams struct {
	start time.Time
	end   time.Time
}

func (f fakeParams) QueryString() string                     { return "" }
func (f fakeParams) Start() time.Time                        { return f.start }
func (f fakeParams) End() time.Time                          { return f.end }
func (f fakeParams) Step() time.Duration                     { return 0 }
func (f fakeParams) Interval() time.Duration                 { return 0 }
func (f fakeParams) Limit() uint32                           { return 0 }
func (f fakeParams) Direction() logproto.Direction           { return logproto.BACKWARD }
func (f fakeParams) Shards() []string                        { return nil }
func (f fakeParams) GetExpression() syntax.Expr              { return nil }
func (f fakeParams) GetStoreChunks() *logproto.ChunkRefGroup { return nil }
func (f fakeParams) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
}

func TestRetentionChecker(t *testing.T) {
	now := time.Date(2026, 1, 19, 06, 15, 30, 0, time.UTC)
	today := now.Truncate(24 * time.Hour)

	globalRetention := 30 * 24 * time.Hour // 30 days
	globalBoundary := today.Add(-globalRetention)

	tests := []struct {
		name            string
		queryStart      time.Time
		queryEnd        time.Time
		streamRetention []validation.StreamRetention

		expectEmptyResponse bool
		expectError         bool
		expectErrorCode     int
		expectAdjustedStart time.Time
	}{
		{
			name:                "global only: query entirely within retention",
			queryStart:          now.Add(-7 * 24 * time.Hour),
			queryEnd:            now.Add(-1 * time.Hour),
			expectAdjustedStart: now.Add(-7 * 24 * time.Hour), // no adjustment
		},
		{
			name:                "global only: query entirely out of retention",
			queryStart:          now.Add(-60 * 24 * time.Hour),
			queryEnd:            now.Add(-45 * 24 * time.Hour),
			expectEmptyResponse: true,
		},
		{
			name:                "global only: query overlaps retention boundary",
			queryStart:          now.Add(-45 * 24 * time.Hour),
			queryEnd:            now.Add(-1 * time.Hour),
			expectAdjustedStart: globalBoundary, // snapped to boundary
		},
		{
			name:                "global only: query start exactly at boundary",
			queryStart:          globalBoundary,
			queryEnd:            now.Add(-1 * time.Hour),
			expectAdjustedStart: globalBoundary,
		},

		// Stream retention cases (with global retention)
		{
			name:       "stream+global: query within both retentions",
			queryStart: now.Add(-10 * 24 * time.Hour), // 10 days
			queryEnd:   now.Add(-1 * time.Hour),
			streamRetention: []validation.StreamRetention{
				{Period: model.Duration(20 * 24 * time.Hour)}, // 20 days
				{Period: model.Duration(60 * 24 * time.Hour)}, // 60 days
			},
			expectAdjustedStart: now.Add(-10 * 24 * time.Hour), // no adjustment
		},
		{
			name:       "stream+global: query overlaps retention boundary",
			queryStart: now.Add(-35 * 24 * time.Hour),
			queryEnd:   now.Add(-1 * time.Hour),
			streamRetention: []validation.StreamRetention{
				{Period: model.Duration(20 * 24 * time.Hour)}, // 20 days
			},
			expectError:     true,
			expectErrorCode: http.StatusNotImplemented,
		},
		{
			name:       "stream+global: multiple stream retentions - uses smallest",
			queryStart: now.Add(-15 * 24 * time.Hour),
			queryEnd:   now.Add(-1 * time.Hour),
			streamRetention: []validation.StreamRetention{
				{Period: model.Duration(40 * 24 * time.Hour)},
				{Period: model.Duration(10 * 24 * time.Hour)}, // most restrictive
			},
			expectError:     true,
			expectErrorCode: http.StatusNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := quartz.NewMock(t)
			clock.Set(now)

			limits := fakeRetentionLimits{
				retentionPeriods: map[string]time.Duration{
					"test-tenant": globalRetention,
				},
				streamRetentions: map[string][]validation.StreamRetention{
					"test-tenant": tt.streamRetention,
				},
			}

			rc := &retentionChecker{
				limits: limits,
				logger: log.NewNopLogger(),
				clock:  clock,
			}

			ctx := user.InjectOrgID(t.Context(), "test-tenant")
			params := fakeParams{
				start: tt.queryStart,
				end:   tt.queryEnd,
			}

			result := rc.Validate(ctx, params)

			// Check error case
			if tt.expectError {
				require.NotNil(t, result.Error)
				if tt.expectErrorCode > 0 {
					resp, ok := httpgrpc.HTTPResponseFromError(result.Error)
					require.True(t, ok)
					require.Equal(t, int32(tt.expectErrorCode), resp.Code)
				}

				return
			}
			require.Nil(t, result.Error)

			if tt.expectEmptyResponse {
				require.True(t, result.EmptyResponse)
				return
			}

			require.NotNil(t, result.Params)
			require.Equal(t, tt.expectAdjustedStart, result.Params.Start())
			require.Equal(t, tt.queryEnd, result.Params.End())
		})
	}
}
