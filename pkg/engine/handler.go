package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

// combinedLimits combines querier limits with retention limits.
type combinedLimits interface {
	querier_limits.Limits
	retentionLimits
}

// Handler returns an [http.Handler] for serving queries. Unsupported queries
// will result in an error.
func Handler(cfg Config, logger log.Logger, engine *Engine, limits combinedLimits) http.Handler {
	return executorHandler(cfg, logger, engine, limits)
}

// queryExecutor is an interface implemented by [Engine] for mocking in tests.
type queryExecutor interface {
	Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error)
}

func executorHandler(cfg Config, logger log.Logger, exec queryExecutor, limits combinedLimits) http.Handler {
	h := &queryHandler{
		cfg:    cfg,
		logger: logger,
		exec:   exec,
		limits: limits,
	}

	if cfg.EnforceRetentionPeriod {
		h.retentionChecker = newRetentionChecker(limits, logger)
	}

	return queryrange.NewSerializeHTTPHandler(h, queryrange.DefaultCodec)
}

type queryHandler struct {
	cfg              Config
	logger           log.Logger
	exec             queryExecutor
	limits           querier_limits.Limits
	retentionChecker *retentionChecker
}

var _ queryrangebase.Handler = (*queryHandler)(nil)

func (h *queryHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	// TODO(rfratto): Can this be removed by making [querier.Handler] and
	// [querier.QuerierAPI] more generic?

	switch req := req.(type) {
	case *queryrange.LokiRequest:
		res, err := h.doRequest(ctx, req)
		if err != nil {
			return nil, err
		}
		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
			return nil, err
		}
		return queryrange.ResultToResponse(res, params)

	case *queryrange.LokiInstantRequest:
		res, err := h.doInstantRequest(ctx, req)
		if err != nil {
			return nil, err
		}
		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
			return nil, err
		}
		return queryrange.ResultToResponse(res, params)

	default:
		return nil, httpgrpc.Errorf(http.StatusNotImplemented, "unsupported query type %T", req)
	}
}

func (h *queryHandler) doRequest(ctx context.Context, req *queryrange.LokiRequest) (logqlmodel.Result, error) {
	logger := utillog.WithContext(ctx, h.logger)

	if err := h.validateMaxEntriesLimits(ctx, req.Plan.AST, req.Limit); err != nil {
		return logqlmodel.Result{}, err
	}

	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return logqlmodel.Result{}, err
	}

	return h.execute(ctx, logger, params)
}

func (h *queryHandler) validateMaxEntriesLimits(ctx context.Context, expr syntax.Expr, limit uint32) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	// entry limit does not apply to metric queries.
	if _, ok := expr.(syntax.SampleExpr); ok {
		return nil
	}

	maxEntriesCapture := func(id string) int { return h.limits.MaxEntriesLimitPerQuery(ctx, id) }
	maxEntriesLimit := util_validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxEntriesCapture)
	if int(limit) > maxEntriesLimit && maxEntriesLimit != 0 {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max entries limit per query exceeded, limit > max_entries_limit_per_query (%d > %d)", limit, maxEntriesLimit)
	}
	return nil
}

func (h *queryHandler) execute(ctx context.Context, logger log.Logger, params logql.Params) (logqlmodel.Result, error) {
	if err := h.validateTimeRange(params); err != nil {
		return logqlmodel.Result{}, httpgrpc.Error(http.StatusNotImplemented, err.Error())
	}

	if h.retentionChecker != nil {
		result := h.retentionChecker.Validate(ctx, params)
		if result.Error != nil {
			return logqlmodel.Result{}, result.Error
		}

		if result.EmptyResponse {
			return emptyResult(ctx, params)
		}

		// continue with adjusted params
		params = result.Params
	}

	res, err := h.exec.Execute(ctx, params)
	if err != nil && errors.Is(err, ErrNotSupported) {
		level.Warn(logger).Log("msg", "unsupported query", "err", err)
		return res, httpgrpc.Error(http.StatusNotImplemented, "unsupported query")
	} else if err != nil {
		level.Error(logger).Log("msg", "query execution failed with new query engine", "err", err)
		return res, err
	}
	return res, nil
}

// validateTimeRange returns an error if the requested time range in params is
// outside of the time range supported by the new engine.
func (h *queryHandler) validateTimeRange(params logql.Params) error {
	reqStart, reqEnd := params.Start(), params.End()
	validStart, validEnd := h.cfg.ValidQueryRange()

	if !reqEnd.After(validEnd) && !reqStart.Before(validStart) {
		return nil
	}

	return fmt.Errorf(
		"query outside of acceptable time range: requested {start=%s, end=%s}, valid range {start=%s, end=%s}",
		reqStart, reqEnd,
		validStart, validEnd,
	)
}

func (h *queryHandler) doInstantRequest(ctx context.Context, req *queryrange.LokiInstantRequest) (logqlmodel.Result, error) {
	logger := utillog.WithContext(ctx, h.logger)

	// Do not allow log selector expression (aka log query) as instant query.
	if _, ok := req.Plan.AST.(syntax.SampleExpr); !ok {
		return logqlmodel.Result{}, logqlmodel.ErrUnsupportedSyntaxForInstantQuery
	}

	if err := h.validateMaxEntriesLimits(ctx, req.Plan.AST, req.Limit); err != nil {
		return logqlmodel.Result{}, err
	}

	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	return h.execute(ctx, logger, params)
}

func emptyResult(ctx context.Context, params logql.Params) (logqlmodel.Result, error) {
	var data parser.Value
	switch params.GetExpression().(type) {
	case syntax.SampleExpr:
		if params.Step() > 0 {
			data = promql.Matrix{}
		} else {
			data = promql.Vector{}
		}
	case syntax.LogSelectorExpr:
		data = logqlmodel.Streams{}
	default:
		return logqlmodel.Result{}, httpgrpc.Errorf(http.StatusInternalServerError, "unsupported expression type %T", params.GetExpression())
	}

	md := metadata.FromContext(ctx)
	md.AddWarning("Query was executed using the new experimental query engine.")

	return logqlmodel.Result{
		Data:     data,
		Headers:  md.Headers(),
		Warnings: md.Warnings(),
	}, nil
}
