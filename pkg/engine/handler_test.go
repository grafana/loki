package engine

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	querytest "github.com/grafana/loki/v3/pkg/querier/testutil"
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

func newTestHandler(cfg Config, exec queryExecutor, limits querier_limits.Limits) queryrangebase.Handler {
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

	handler := executorHandler(cfg, logger, eng, limits)
	require.NotNil(t, handler)
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
