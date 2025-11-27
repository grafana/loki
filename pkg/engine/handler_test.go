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

func TestHandler(t *testing.T) {
	cfg := Config{
		Executor: ExecutorConfig{
			BatchSize:          100,
			MergePrefetchCount: 0,
		},
	}
	logger := log.NewNopLogger()
	eng := &mockEngine{}
	limits := &querytest.MockLimits{}

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
