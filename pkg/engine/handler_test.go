package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	querytest "github.com/grafana/loki/v3/pkg/querier/testutil"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

type mockEngine struct {
	executeFunc func(ctx context.Context, params logql.Params) (logqlmodel.Result, error)
}

func (m *mockEngine) Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, params)
	}
	return logqlmodel.Result{}, nil
}

func newTestHandler(cfg Config, exec QueryExecutor, limits querier_limits.Limits) queryrangebase.Handler {
	return &queryHandler{
		cfg:    cfg,
		exec:   exec,
		logger: log.NewNopLogger(),
		limits: limits,
	}
}

type mockLimits struct {
	querier_limits.Limits
	RetentionLimits
}

func (m *mockLimits) MaxCacheFreshness(_ context.Context, _ string) time.Duration { return 0 }
func (m *mockLimits) MaxQueryParallelism(_ context.Context, _ string) int         { return 1 }
func (m *mockLimits) EngineResultsCacheTimeBucketInterval(_ string) time.Duration {
	return 24 * time.Hour
}

func TestHandler(t *testing.T) {
	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}
	logger := log.NewNopLogger()
	eng := &mockEngine{}
	limits := &mockLimits{}

	handler, err := executorHandler(cfg, logger, eng, limits, nil)
	require.NoError(t, err)
	require.NotNil(t, handler)
}

func TestMetricStepAlignMiddleware(t *testing.T) {
	base := time.Date(2026, 3, 2, 6, 43, 7, 0, time.UTC)
	startWithSubSecond := base.Add(123 * time.Millisecond)
	endWithSubSecond := base.Add(456 * time.Millisecond)

	step := int64(1000) // 1s step in milliseconds

	tests := []struct {
		name          string
		query         string
		startTs       time.Time
		endTs         time.Time
		step          int64
		expectAligned bool
	}{
		{
			name:          "log query preserves sub-second timestamps",
			query:         `{app="test"}`,
			startTs:       startWithSubSecond,
			endTs:         endWithSubSecond,
			step:          step,
			expectAligned: false,
		},
		{
			name:          "metric query gets step-aligned",
			query:         `rate({app="test"}[5m])`,
			startTs:       startWithSubSecond,
			endTs:         endWithSubSecond,
			step:          step,
			expectAligned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedStart, capturedEnd time.Time

			inner := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				capturedStart = r.GetStart()
				capturedEnd = r.GetEnd()
				return &queryrange.LokiResponse{}, nil
			})

			middleware := newMetricStepAlignMiddleware()
			handler := middleware.Wrap(inner)

			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)

			req := &queryrange.LokiRequest{
				Query:   tt.query,
				StartTs: tt.startTs,
				EndTs:   tt.endTs,
				Step:    tt.step,
				Plan:    &plan.QueryPlan{AST: expr},
			}

			_, err = handler.Do(context.Background(), req)
			require.NoError(t, err)

			if tt.expectAligned {
				alignedStart := time.UnixMilli((tt.startTs.UnixMilli() / tt.step) * tt.step)
				alignedEnd := time.UnixMilli((tt.endTs.UnixMilli() / tt.step) * tt.step)
				require.Equal(t, alignedStart, capturedStart, "metric query start should be step-aligned")
				require.Equal(t, alignedEnd, capturedEnd, "metric query end should be step-aligned")
			} else {
				require.Equal(t, tt.startTs, capturedStart, "log query start should preserve original timestamp")
				require.Equal(t, tt.endTs, capturedEnd, "log query end should preserve original timestamp")
			}
		})
	}
}

func TestQueryHandler_Do_LokiRequest(t *testing.T) {
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}

	t.Run("successful log query", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{
					Data: logqlmodel.Streams{},
				}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)
	})

	t.Run("exceeds max entries limit", func(t *testing.T) {
		eng := &mockEngine{}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 50}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.Error(t, err)

		httpErr, ok := httpgrpc.HTTPResponseFromError(err)
		require.True(t, ok)
		require.Equal(t, int32(http.StatusBadRequest), httpErr.Code)
		require.Contains(t, string(httpErr.Body), "max entries limit per query exceeded")
	})

	t.Run("metric query ignores entry limit", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{
					Data: logqlmodel.Streams{},
				}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 50}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`rate({app="test"}[5m])`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `rate({app="test"}[5m])`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)
	})

	t.Run("unsupported query type", func(t *testing.T) {
		eng := &mockEngine{}
		limits := &querytest.MockLimits{}

		handler := newTestHandler(cfg, eng, limits)

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err := handler.Do(ctx, &queryrangebase.PrometheusRequest{})
		require.Error(t, err)

		httpErr, ok := httpgrpc.HTTPResponseFromError(err)
		require.True(t, ok)
		require.Equal(t, int32(http.StatusNotImplemented), httpErr.Code)
	})

	t.Run("engine returns ErrNotSupported", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{}, ErrNotSupported
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.Error(t, err)

		httpErr, ok := httpgrpc.HTTPResponseFromError(err)
		require.True(t, ok)
		require.Equal(t, int32(http.StatusNotImplemented), httpErr.Code)
		require.Contains(t, string(httpErr.Body), "unsupported query")
	})

	t.Run("engine returns other error", func(t *testing.T) {
		expectedErr := errors.New("execution failed")
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{}, expectedErr
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.Error(t, err)
		require.True(t, errors.Is(err, expectedErr))
	})
}

func TestQueryHandler_Do_LokiInstantRequest(t *testing.T) {
	now := time.Now()

	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}

	t.Run("successful metric instant query", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{
					Data: logqlmodel.Streams{},
				}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`rate({app="test"}[5m])`)
		require.NoError(t, err)

		req := &queryrange.LokiInstantRequest{
			Query:     `rate({app="test"}[5m])`,
			Limit:     100,
			TimeTs:    now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)
	})

	t.Run("log query not allowed for instant query", func(t *testing.T) {
		eng := &mockEngine{}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiInstantRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			TimeTs:    now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.Error(t, err)
		require.True(t, errors.Is(err, logqlmodel.ErrUnsupportedSyntaxForInstantQuery))
	})

	t.Run("metric query ignores entry limit", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{
					Data: logqlmodel.Streams{},
				}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 50}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`rate({app="test"}[5m])`)
		require.NoError(t, err)

		req := &queryrange.LokiInstantRequest{
			Query:     `rate({app="test"}[5m])`,
			Limit:     100,
			TimeTs:    now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)
	})
}

func TestQueryHandler_ValidTimeRange(t *testing.T) {
	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	fourHoursAgo := now.Add(-4 * time.Hour)

	tests := []struct {
		name        string
		cfg         Config
		startTime   time.Time
		endTime     time.Time
		expectValid bool
	}{
		{
			name: "valid time range within bounds",
			cfg: Config{
				StorageLag: 1 * time.Hour,
			},
			startTime:   fourHoursAgo,
			endTime:     twoHoursAgo,
			expectValid: true,
		},
		{
			name: "end time too recent",
			cfg: Config{
				StorageLag: 1 * time.Hour,
			},
			startTime:   twoHoursAgo,
			endTime:     now.Add(-30 * time.Minute),
			expectValid: false,
		},
		{
			name: "start time too early",
			cfg: Config{
				StorageLag:       1 * time.Hour,
				StorageStartDate: flagext.Time(now.Add(-3 * time.Hour)),
			},
			startTime:   fourHoursAgo,
			endTime:     twoHoursAgo,
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &mockEngine{
				executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
					return logqlmodel.Result{
						Data: logqlmodel.Streams{},
					}, nil
				},
			}
			limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}

			handler := newTestHandler(tt.cfg, eng, limits)

			expr, err := syntax.ParseExpr(`{app="test"}`)
			require.NoError(t, err)

			req := &queryrange.LokiRequest{
				Query:     `{app="test"}`,
				Limit:     100,
				StartTs:   tt.startTime,
				EndTs:     tt.endTime,
				Direction: 0,
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			}

			ctx := user.InjectOrgID(context.Background(), "fake")
			_, err = handler.Do(ctx, req)

			if tt.expectValid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				httpErr, ok := httpgrpc.HTTPResponseFromError(err)
				require.True(t, ok)
				require.Equal(t, int32(http.StatusNotImplemented), httpErr.Code)
				require.Contains(t, string(httpErr.Body), "query outside of acceptable time range")
			}
		})
	}
}

func TestQueryHandler_ValidateMaxEntriesLimits(t *testing.T) {
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}

	t.Run("no limit enforced when maxEntriesLimitPerQuery is zero", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{
					Data: logqlmodel.Streams{},
				}, nil
			},
		}
		limits := &querytest.MockLimits{}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     1000000,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)
	})

	t.Run("limit equal to max is allowed", func(t *testing.T) {
		eng := &mockEngine{
			executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
				return logqlmodel.Result{
					Data: logqlmodel.Streams{},
				}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 100}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)
	})

	t.Run("missing tenant ID returns error", func(t *testing.T) {
		eng := &mockEngine{}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 100}

		handler := newTestHandler(cfg, eng, limits)

		expr, err := syntax.ParseExpr(`{app="test"}`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:     `{app="test"}`,
			Limit:     100,
			StartTs:   startTime,
			EndTs:     now,
			Direction: 0,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		_, err = handler.Do(context.Background(), req)
		require.Error(t, err)

		httpErr, ok := httpgrpc.HTTPResponseFromError(err)
		require.True(t, ok)
		require.Equal(t, int32(http.StatusBadRequest), httpErr.Code)
	})
}

func TestQueryHandler_ValidateRequest(t *testing.T) {
	now := time.Now()

	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}

	tests := []struct {
		name           string
		query          string
		limit          uint32
		startTs        time.Time
		endTs          time.Time
		limits         *querytest.MockLimits
		expectError    bool
		expectedStatus int32
		errorContains  string
	}{
		// MaxQueryLookback tests
		{
			name:    "query within lookback period is allowed",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLookbackVal:        24 * time.Hour,
			},
			expectError: false,
		},
		{
			name:    "query start time before lookback period is clamped",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-3 * time.Hour), // Start is outside lookback
			endTs:   now,                     // End is within lookback
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLookbackVal:        1 * time.Hour,
			},
			expectError: false, // Should succeed with clamped start time
		},
		{
			name:    "query end time before lookback period returns error",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-3 * time.Hour),
			endTs:   now.Add(-2 * time.Hour),
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLookbackVal:        1 * time.Hour,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "outside the allowed lookback period",
		},
		{
			name:    "no lookback limit allows all queries",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-720 * time.Hour), // 30 days ago
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLookbackVal:        0, // No limit
			},
			expectError: false,
		},
		// MaxQueryLength tests
		{
			name:    "query within max length is allowed",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLengthVal:          24 * time.Hour,
			},
			expectError: false,
		},
		{
			name:    "query exceeding max length returns error",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-2 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLengthVal:          1 * time.Hour,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "the query time range exceeds the limit",
		},
		{
			name:    "no max length allows all queries",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-720 * time.Hour), // 30 days
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLengthVal:          0, // No limit
			},
			expectError: false,
		},
		// MaxQueryRange tests
		{
			name:    "metric query with range within limit is allowed",
			query:   `rate({app="test"}[5m])`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryRangeVal:           1 * time.Hour,
			},
			expectError: false,
		},
		{
			name:    "metric query with range exceeding limit returns error",
			query:   `rate({app="test"}[5m])`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryRangeVal:           1 * time.Minute,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "[interval] value exceeds limit",
		},
		{
			name:    "log query ignores max query range",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryRangeVal:           1 * time.Minute,
			},
			expectError: false,
		},
		// RequiredLabels tests
		{
			name:    "query with required labels is allowed",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredLabelsVal:          []string{"app"},
			},
			expectError: false,
		},
		{
			name:    "query missing required labels returns error",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredLabelsVal:          []string{"namespace"},
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "missing required matchers",
		},
		{
			name:    "query with multiple required labels",
			query:   `{app="test", namespace="prod"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredLabelsVal:          []string{"app", "namespace"},
			},
			expectError: false,
		},
		{
			name:    "no required labels allows all queries",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredLabelsVal:          nil,
			},
			expectError: false,
		},
		// RequiredNumberLabels tests
		{
			name:    "query with enough labels is allowed",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredNumberLabelsVal:    1,
			},
			expectError: false,
		},
		{
			name:    "query with too few labels returns error",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredNumberLabelsVal:    2,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "less label matchers than required",
		},
		{
			name:    "no required number allows all queries",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredNumberLabelsVal:    0,
			},
			expectError: false,
		},
		// MaxEntriesLimit tests
		{
			name:    "exceeds max entries limit",
			query:   `{app="test"}`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 50,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "max entries limit per query exceeded",
		},
		{
			name:    "metric query ignores entry limit",
			query:   `rate({app="test"}[5m])`,
			limit:   100,
			startTs: now.Add(-1 * time.Hour),
			endTs:   now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 50,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &mockEngine{
				executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
					return logqlmodel.Result{
						Data: logqlmodel.Streams{},
					}, nil
				},
			}

			handler := newTestHandler(cfg, eng, tt.limits)

			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)

			req := &queryrange.LokiRequest{
				Query:     tt.query,
				Limit:     tt.limit,
				StartTs:   tt.startTs,
				EndTs:     tt.endTs,
				Direction: 0,
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			}

			ctx := user.InjectOrgID(t.Context(), "fake")
			_, err = handler.Do(ctx, req)

			if tt.expectError {
				require.Error(t, err)
				httpErr, ok := httpgrpc.HTTPResponseFromError(err)
				require.True(t, ok)
				require.Equal(t, tt.expectedStatus, httpErr.Code)
				require.Contains(t, string(httpErr.Body), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExecutorHandler_AlignQueriesWithStep(t *testing.T) {
	stepMs := int64(60 * 1000) // 60 seconds in milliseconds

	now := time.Now()
	// Create start/end aligned to step, then add 27s offset to misalign.
	alignedStartMs := (now.Add(-2*time.Hour).UnixMilli() / stepMs) * stepMs
	alignedEndMs := (now.Add(-90*time.Minute).UnixMilli() / stepMs) * stepMs
	unalignedStart := time.UnixMilli(alignedStartMs + 27*1000)
	unalignedEnd := time.UnixMilli(alignedEndMs + 27*1000)
	expectedAlignedStart := time.UnixMilli(alignedStartMs)
	expectedAlignedEnd := time.UnixMilli(alignedEndMs)

	makeHandler := func(cfg Config, eng QueryExecutor, limits querier_limits.Limits) queryrangebase.Handler {
		var h queryrangebase.Handler = &queryHandler{
			cfg:    cfg,
			exec:   eng,
			logger: log.NewNopLogger(),
			limits: limits,
		}
		if cfg.AlignQueriesWithStep {
			h = queryrangebase.StepAlignMiddleware.Wrap(h)
		}
		return h
	}

	t.Run("aligns start and end to step when enabled", func(t *testing.T) {
		var capturedParams logql.Params
		eng := &mockEngine{
			executeFunc: func(_ context.Context, params logql.Params) (logqlmodel.Result, error) {
				capturedParams = params
				return logqlmodel.Result{Data: logqlmodel.Streams{}}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}
		handler := makeHandler(Config{AlignQueriesWithStep: true}, eng, limits)

		expr, err := syntax.ParseExpr(`rate({app="test"}[5m])`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:   `rate({app="test"}[5m])`,
			Limit:   100,
			Step:    stepMs,
			StartTs: unalignedStart,
			EndTs:   unalignedEnd,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)

		require.NotNil(t, capturedParams)
		require.Equal(t, expectedAlignedStart, capturedParams.Start())
		require.Equal(t, expectedAlignedEnd, capturedParams.End())
	})

	t.Run("does not align when disabled", func(t *testing.T) {
		var capturedParams logql.Params
		eng := &mockEngine{
			executeFunc: func(_ context.Context, params logql.Params) (logqlmodel.Result, error) {
				capturedParams = params
				return logqlmodel.Result{Data: logqlmodel.Streams{}}, nil
			},
		}
		limits := &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}
		handler := makeHandler(Config{AlignQueriesWithStep: false}, eng, limits)

		expr, err := syntax.ParseExpr(`rate({app="test"}[5m])`)
		require.NoError(t, err)

		req := &queryrange.LokiRequest{
			Query:   `rate({app="test"}[5m])`,
			Limit:   100,
			Step:    stepMs,
			StartTs: unalignedStart,
			EndTs:   unalignedEnd,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err = handler.Do(ctx, req)
		require.NoError(t, err)

		require.NotNil(t, capturedParams)
		require.Equal(t, unalignedStart, capturedParams.Start())
		require.Equal(t, unalignedEnd, capturedParams.End())
	})
}

func TestQueryHandler_ValidateInstantRequest(t *testing.T) {
	now := time.Now()

	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}

	tests := []struct {
		name           string
		query          string
		limit          uint32
		timeTs         time.Time
		limits         *querytest.MockLimits
		expectError    bool
		expectedStatus int32
		errorContains  string
	}{
		// RequiredLabels tests
		{
			name:   "instant query with required labels is allowed",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredLabelsVal:          []string{"app"},
			},
			expectError: false,
		},
		{
			name:   "instant query missing required labels returns error",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredLabelsVal:          []string{"namespace"},
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "missing required matchers",
		},
		// MaxQueryRange tests
		{
			name:   "instant query with range within limit is allowed",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryRangeVal:           1 * time.Hour,
			},
			expectError: false,
		},
		{
			name:   "instant query exceeding max query range returns error",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryRangeVal:           1 * time.Minute,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "[interval] value exceeds limit",
		},
		// MaxQueryLookback tests
		{
			name:   "instant query within lookback period is allowed",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLookbackVal:        1 * time.Hour,
			},
			expectError: false,
		},
		{
			name:   "instant query outside lookback period returns error",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now.Add(-2 * time.Hour), // 2 hours ago, outside 1h lookback
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				MaxQueryLookbackVal:        1 * time.Hour,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "outside the allowed lookback period",
		},
		// RequiredNumberLabels tests
		{
			name:   "instant query with enough labels is allowed",
			query:  `rate({app="test", namespace="prod"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredNumberLabelsVal:    2,
			},
			expectError: false,
		},
		{
			name:   "instant query with too few labels returns error",
			query:  `rate({app="test"}[5m])`,
			limit:  100,
			timeTs: now,
			limits: &querytest.MockLimits{
				MaxEntriesLimitPerQueryVal: 1000,
				RequiredNumberLabelsVal:    2,
			},
			expectError:    true,
			expectedStatus: http.StatusBadRequest,
			errorContains:  "less label matchers than required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &mockEngine{
				executeFunc: func(_ context.Context, _ logql.Params) (logqlmodel.Result, error) {
					return logqlmodel.Result{
						Data: logqlmodel.Streams{},
					}, nil
				},
			}

			handler := newTestHandler(cfg, eng, tt.limits)

			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)

			req := &queryrange.LokiInstantRequest{
				Query:     tt.query,
				Limit:     tt.limit,
				TimeTs:    tt.timeTs,
				Direction: 0,
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			}

			ctx := user.InjectOrgID(t.Context(), "fake")
			_, err = handler.Do(ctx, req)

			if tt.expectError {
				require.Error(t, err)
				httpErr, ok := httpgrpc.HTTPResponseFromError(err)
				require.True(t, ok)
				require.Equal(t, tt.expectedStatus, httpErr.Code)
				require.Contains(t, string(httpErr.Body), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMetricStepAlignBeforeCache asserts that the step-alignment middleware
// runs *before* the results-cache middleware in the v2 engine handler chain,
// by exercising the production [HandlerFromExecutor] composition end-to-end.
//
// The cache extractor ([queryrangebase.PrometheusResponseExtractor.Extract])
// drops samples outside [req.Start, req.End] on every cache read. If alignment
// ran inside the cache (cache wrapping alignment), the cache would see the
// un-aligned request bounds, store extents under those bounds, and clip out
// the leading floor-aligned sample on every cache hit — turning a 4-sample
// metric response into 3 silently. By wrapping cache inside alignment, the
// cache sees the aligned start/end, so the leading sample falls inside the
// stored extent and survives the per-read [extractMatrix] clip.
func TestMetricStepAlignBeforeCache(t *testing.T) {
	const stepMs = int64(172_000) // 172s — does not divide evenly into 12:00:00.
	// start = 12:00:00 UTC = 1782734400_000ms. 1782734400000 % 172000 = 152000ms.
	// → aligned start = 1782734400000 - 152000 = 1782734248000 = 11:57:28 UTC.
	startMs := int64(1782734400_000)
	alignedStartMs := (startMs / stepMs) * stepMs
	endMs := startMs + 3*stepMs
	alignedEndMs := (endMs / stepMs) * stepMs

	// Track the start each invocation of the inner executor sees, so we can
	// confirm alignment ran before the cache layer. Count invocations so
	// we can assert the second query actually hit the cache (rather than
	// silently missing and re-computing the same response).
	var (
		capturedStartMs int64
		executorCalls   int
	)
	exec := &mockEngine{
		executeFunc: func(_ context.Context, params logql.Params) (logqlmodel.Result, error) {
			executorCalls++
			capturedStartMs = params.Start().UnixMilli()
			// Emit samples on the aligned step grid: aligned_start, +step, ...
			// The first sample at aligned_start is BEFORE the requested startMs;
			// it must be preserved across cache reads.
			samples := make([]promql.FPoint, 0, 4)
			for ts := alignedStartMs; ts <= alignedEndMs; ts += stepMs {
				samples = append(samples, promql.FPoint{T: ts, F: float64(ts)})
			}
			return logqlmodel.Result{
				Data: promql.Matrix{
					{Metric: labels.EmptyLabels(), Floats: samples},
				},
			}, nil
		},
	}

	cfg := Config{
		Executor:             ExecutorConfig{BatchSize: 100},
		AlignQueriesWithStep: true,
		ResultsCache: resultscache.Config{
			CacheConfig: cache.Config{Cache: cache.NewMockCache()},
		},
	}
	limits := &mockLimits{Limits: &querytest.MockLimits{MaxEntriesLimitPerQueryVal: 1000}}

	// Use the production handler composition so the test depends on the
	// actual middleware order in [executorHandler].
	httpHandler, err := HandlerFromExecutor(cfg, log.NewNopLogger(), exec, limits, nil)
	require.NoError(t, err)

	doQuery := func() []promql.FPoint {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://localhost/loki/api/v1/query_range?"+url.Values{
			"query": {`count_over_time({app="test"}[1m])`},
			"start": {strconv.FormatInt(startMs*int64(time.Millisecond), 10)},
			"end":   {strconv.FormatInt(endMs*int64(time.Millisecond), 10)},
			"step":  {strconv.FormatInt(stepMs/1000, 10)},
		}.Encode(), nil)
		req = req.WithContext(user.InjectOrgID(req.Context(), "fake"))
		rec := httptest.NewRecorder()
		httpHandler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		return parsePromSamples(t, rec.Body.Bytes())
	}

	// First call: cache miss — executor must be invoked exactly once.
	got1 := doQuery()
	require.Equal(t, 1, executorCalls, "first call should invoke the executor (cache miss)")
	require.Equal(t, alignedStartMs, capturedStartMs,
		"alignment must run before the engine sees the request")
	require.Len(t, got1, 4,
		"cache-miss path must return all four aligned samples including the leading one at %v",
		time.UnixMilli(alignedStartMs).UTC().Format(time.RFC3339Nano))
	require.Equal(t, alignedStartMs, got1[0].T,
		"first sample must be at the floor-aligned start")

	// Second call: cache hit — executor must NOT be invoked again, and the
	// response must still contain the leading aligned sample. With the fix,
	// the cache key, stored extent bounds, and the read-time clip all use
	// the aligned start, so the leading sample at aligned_start survives.
	// With the pre-fix order, extractMatrix would clip samples to
	// [startMs, endMs] and drop the leading sample.
	got2 := doQuery()
	require.Equal(t, 1, executorCalls,
		"second call must be served from cache (executor should not run again)")
	require.Len(t, got2, 4,
		"cache-hit path must preserve the leading aligned sample (regression: cache wrapping alignment would yield 3)")
	require.Equal(t, got1, got2,
		"cache-hit response must equal cache-miss response")
}

// parsePromSamples decodes the JSON body of a Loki /query_range matrix
// response and returns the samples of the (single) series.
func parsePromSamples(t *testing.T, body []byte) []promql.FPoint {
	t.Helper()
	var resp struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Values [][]any           `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp), "body=%s", body)
	require.Equal(t, "success", resp.Status, "body=%s", body)
	require.Equal(t, "matrix", resp.Data.ResultType)
	require.Len(t, resp.Data.Result, 1, "expected exactly one series; body=%s", body)
	pts := make([]promql.FPoint, 0, len(resp.Data.Result[0].Values))
	for _, pair := range resp.Data.Result[0].Values {
		require.Len(t, pair, 2)
		// Prometheus encodes timestamps in seconds as float64.
		tsSec, ok := pair[0].(float64)
		require.True(t, ok, "ts is %T: %v", pair[0], pair[0])
		valStr, ok := pair[1].(string)
		require.True(t, ok, "value is %T: %v", pair[1], pair[1])
		v, err := strconv.ParseFloat(valStr, 64)
		require.NoError(t, err)
		pts = append(pts, promql.FPoint{T: int64(tsSec * 1000), F: v})
	}
	return pts
}
