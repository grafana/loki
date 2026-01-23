package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
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

type Limits interface {
	querier_limits.Limits
	RetentionLimits
}

// Handler returns an [http.Handler] for serving queries. Unsupported queries
// will result in an error.
func Handler(cfg Config, logger log.Logger, engine *Engine, limits Limits) http.Handler {
	return executorHandler(cfg, logger, engine, limits)
}

// queryExecutor is an interface implemented by [Engine] for mocking in tests.
type queryExecutor interface {
	Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error)
}

func executorHandler(cfg Config, logger log.Logger, exec queryExecutor, limits Limits) http.Handler {
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

	req, err := h.validateRequest(ctx, req)
	if err != nil {
		return logqlmodel.Result{}, err
	}

	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return logqlmodel.Result{}, err
	}

	return h.execute(ctx, logger, params)
}

// validateRequest validates all limits for a range query request.
// Returns the potentially modified request (with adjusted start time) or an error.
func (h *queryHandler) validateRequest(ctx context.Context, req *queryrange.LokiRequest) (*queryrange.LokiRequest, error) {
	if err := h.validateMaxEntriesLimits(ctx, req.Plan.AST, req.Limit); err != nil {
		return nil, err
	}

	if err := h.validateRequiredLabels(ctx, req.Plan.AST); err != nil {
		return nil, err
	}

	if err := h.validateMaxQueryRange(ctx, req.Plan.AST); err != nil {
		return nil, err
	}

	// Validate and potentially adjust the query time range based on lookback limit
	// If the adjusted start is different from the original start, update the request.
	adjustedStart, err := h.validateMaxQueryLookback(ctx, req.StartTs, req.EndTs)
	if err != nil {
		return nil, err
	}
	if !adjustedStart.Equal(req.StartTs) {
		req = req.WithStartEnd(adjustedStart, req.EndTs).(*queryrange.LokiRequest)
	}

	if err := h.validateMaxQueryLength(ctx, req.StartTs, req.EndTs); err != nil {
		return nil, err
	}

	return req, nil
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

// validateMaxQueryLookback validates that the query time range is within the max lookback period.
// Returns an error if the query end time is before the minimum allowed start time.
// Returns the adjusted start time if the query start time needs to be clamped.
func (h *queryHandler) validateMaxQueryLookback(ctx context.Context, start, end time.Time) (time.Time, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return start, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	lookbackCapture := func(id string) time.Duration { return h.limits.MaxQueryLookback(ctx, id) }
	maxQueryLookback := util_validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, lookbackCapture)
	if maxQueryLookback <= 0 {
		return start, nil
	}

	minStartTime := time.Now().Add(-maxQueryLookback)

	// If the query end time is before the minimum allowed start time,
	// the query is fully outside the allowed range.
	if end.Before(minStartTime) {
		return start, httpgrpc.Errorf(http.StatusBadRequest,
			"the query time range is outside the allowed lookback period: query end (%s) is before the minimum start time (%s)",
			end.Format(time.RFC3339), minStartTime.Format(time.RFC3339))
	}

	// If the query start time is before the minimum allowed start time,
	// clamp it to the minimum allowed start time.
	if start.Before(minStartTime) {
		return minStartTime, nil
	}

	return start, nil
}

// validateMaxQueryLength validates that the query time range does not exceed the max query length.
func (h *queryHandler) validateMaxQueryLength(ctx context.Context, start, end time.Time) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	lengthCapture := func(id string) time.Duration { return h.limits.MaxQueryLength(ctx, id) }
	maxQueryLength := util_validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, lengthCapture)
	if maxQueryLength <= 0 {
		return nil
	}

	queryLen := end.Sub(start)
	if queryLen > maxQueryLength {
		return httpgrpc.Errorf(http.StatusBadRequest,
			util_validation.ErrQueryTooLong, queryLen, model.Duration(maxQueryLength))
	}

	return nil
}

// validateMaxQueryRange validates that range vector intervals in the query do not exceed the limit.
// This only applies to metric queries (SampleExpr).
func (h *queryHandler) validateMaxQueryRange(ctx context.Context, expr syntax.Expr) error {
	// MaxQueryRange only applies to metric queries.
	sampleExpr, ok := expr.(syntax.SampleExpr)
	if !ok {
		return nil
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	rangeCapture := func(id string) time.Duration { return h.limits.MaxQueryRange(ctx, id) }
	maxQueryRange := util_validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, rangeCapture)
	if maxQueryRange <= 0 {
		return nil
	}

	var rangeErr error
	sampleExpr.Walk(func(e syntax.Expr) bool {
		switch rangeExpr := e.(type) {
		case *syntax.LogRangeExpr:
			if rangeExpr.Interval > maxQueryRange {
				rangeErr = httpgrpc.Errorf(http.StatusBadRequest,
					"%s: [%s] > [%s]", logqlmodel.ErrIntervalLimit, model.Duration(rangeExpr.Interval), model.Duration(maxQueryRange))
				return false // stop walking
			}
		}
		return true
	})

	return rangeErr
}

// validateRequiredLabels validates that the query contains all required labels
// and has at least the minimum required number of label matchers.
func (h *queryHandler) validateRequiredLabels(ctx context.Context, expr syntax.Expr) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	// Collect required labels per tenant and compute the minimum required number of labels.
	// This avoids repeated calls to limits methods when validating each matcher group.
	requiredLabelsByTenant := make(map[string][]string, len(tenantIDs))
	var requiredNumberLabels int
	for _, tenantID := range tenantIDs {
		required := h.limits.RequiredLabels(ctx, tenantID)
		if len(required) > 0 {
			requiredLabelsByTenant[tenantID] = required
		}
		if n := h.limits.RequiredNumberLabels(ctx, tenantID); n > 0 {
			if requiredNumberLabels == 0 || n < requiredNumberLabels {
				requiredNumberLabels = n
			}
		}
	}

	// Early return if no tenant has any requirements configured.
	if len(requiredLabelsByTenant) == 0 && requiredNumberLabels == 0 {
		return nil
	}

	// Get matcher groups from the expression
	matcherGroups, err := syntax.MatcherGroups(expr)
	if err != nil {
		// If we can't extract matchers, skip validation
		return nil
	}

	// Validate each matcher group
	for _, group := range matcherGroups {
		if err := h.validateMatcherGroup(group.Matchers, requiredLabelsByTenant, requiredNumberLabels); err != nil {
			return err
		}
	}

	return nil
}

// validateMatcherGroup validates a single group of matchers against required labels limits.
func (h *queryHandler) validateMatcherGroup(matchers []*labels.Matcher, requiredLabelsByTenant map[string][]string, requiredNumberLabels int) error {
	actual := make(map[string]struct{}, len(matchers))
	var present []string
	for _, m := range matchers {
		actual[m.Name] = struct{}{}
		present = append(present, m.Name)
	}

	// Enforce RequiredLabels limit per tenant.
	for _, required := range requiredLabelsByTenant {
		var missing []string
		for _, label := range required {
			if _, found := actual[label]; !found {
				missing = append(missing, label)
			}
		}

		if len(missing) > 0 {
			return httpgrpc.Errorf(http.StatusBadRequest,
				"stream selector is missing required matchers [%s], labels present in the query were [%s]",
				strings.Join(missing, ", "), strings.Join(present, ", "))
		}
	}

	// Enforce RequiredNumberLabels limit.
	if requiredNumberLabels > 0 && len(present) < requiredNumberLabels {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"stream selector has less label matchers than required: (present: [%s], number_present: %d, required_number_label_matchers: %d)",
			strings.Join(present, ", "), len(present), requiredNumberLabels)
	}

	return nil
}

func (h *queryHandler) execute(ctx context.Context, logger log.Logger, params logql.Params) (logqlmodel.Result, error) {
	if err := h.validateTimeRange(params); err != nil {
		return logqlmodel.Result{}, httpgrpc.Error(http.StatusNotImplemented, err.Error())
	}

	if h.retentionChecker != nil {
		checkResult := h.retentionChecker.Validate(ctx, params)
		if checkResult.Error != nil {
			return logqlmodel.Result{}, checkResult.Error
		}

		if checkResult.EmptyResponse {
			return emptyResult(ctx, params)
		}

		// continue with adjusted params
		params = checkResult.Params
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

	if err := h.validateInstantRequest(ctx, req); err != nil {
		return logqlmodel.Result{}, err
	}

	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	return h.execute(ctx, logger, params)
}

// validateInstantRequest validates all limits for an instant query request.
func (h *queryHandler) validateInstantRequest(ctx context.Context, req *queryrange.LokiInstantRequest) error {
	if err := h.validateRequiredLabels(ctx, req.Plan.AST); err != nil {
		return err
	}

	if err := h.validateMaxQueryRange(ctx, req.Plan.AST); err != nil {
		return err
	}

	// For instant queries, we check if the query time is within the lookback period
	_, err := h.validateMaxQueryLookback(ctx, req.TimeTs, req.TimeTs)
	return err
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
