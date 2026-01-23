package engine

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/validation"
)

// RetentionLimits provides access to tenant retention settings.
type RetentionLimits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
}

// retentionChecker checks the incoming query against tenant's retention settings
// and decides how to handle it. This is a temporary solution until storage
// supports retention directly.
//
// The following section outlines how to handle retention period:
//
// 1. Only global retention_period configured (no stream retention):
//
//	retentionBoundary = now - retentionPeriod (data older than this is expired)
//
// ┌─────────────────────────────────────┬──────────────────────────────────────────────────────────────┐
// │ query entirely within retention     │ Execute as-is                                                │
// │ (queryStart >= retentionBoundary)   │                                                              │
// ├─────────────────────────────────────┼──────────────────────────────────────────────────────────────┤
// │ query overlaps retention boundary   │ Snap query start to retention boundary to avoid reading      │
// │ (queryStart < retentionBoundary     │ data out of retention                                        │
// │  AND queryEnd > retentionBoundary)  │                                                              │
// ├─────────────────────────────────────┼──────────────────────────────────────────────────────────────┤
// │ query entirely out of retention     │ Entirely out of retention. Return empty response             │
// │ (queryEnd <= retentionBoundary)     │                                                              │
// └─────────────────────────────────────┴──────────────────────────────────────────────────────────────┘
//
// 2. Both global retention_period and per-stream retention configured:
//
//	globalBoundary = now - globalRetentionPeriod
//	streamBoundary = now - smallestStreamRetentionPeriod (most restrictive)
//
// ┌─────────────────────────────────────┬──────────────────────────────────────────────────────────────┐
// │ query entirely within retention     │ Execute as-is                                                │
// │ (queryStart >= globalBoundary AND   │                                                              │
// │  queryStart >= streamBoundary)      │                                                              │
// ├─────────────────────────────────────┼──────────────────────────────────────────────────────────────┤
// │ query overlaps either boundary      │ Return 501 Not Implemented error                             │
// │ (queryStart < globalBoundary OR     │                                                              │
// │  queryStart < streamBoundary)       │                                                              │
// └─────────────────────────────────────┴──────────────────────────────────────────────────────────────┘
type retentionChecker struct {
	limits RetentionLimits
	logger log.Logger
	clock  quartz.Clock
}

// newRetentionChecker creates a retention checker to validate queries against retention limits.
func newRetentionChecker(limits RetentionLimits, logger log.Logger) *retentionChecker {
	return &retentionChecker{
		limits: limits,
		logger: logger,
		clock:  quartz.NewReal(),
	}
}

// RetentionCheckResult represents the result of a retention check.
type RetentionCheckResult struct {
	// If the query start was snapped to the retention boundary, this will
	// contain adjusted params. Otherwise, it returns the original params.
	Params logql.Params

	// EmptyResponse indicates the query should return an empty response
	// because the entire range is out of retention.
	EmptyResponse bool

	// Error is set if the query cannot be executed due to retention constraints.
	// This is used when stream retention makes the situation too complex.
	Error error
}

// Validate determines how a query should be executed based on retention limits.
// It returns a result containing potentially adjusted params, or an error/empty indicator.
func (r *retentionChecker) Validate(ctx context.Context, params logql.Params) RetentionCheckResult {
	if r.limits == nil {
		return RetentionCheckResult{Params: params}
	}

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return RetentionCheckResult{
			Error: httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error()),
		}
	}

	var (
		queryStart = params.Start()
		queryEnd   = params.End()

		// Calculate retention boundary at day granularity.
		today           = r.clock.Now().UTC().Truncate(24 * time.Hour)
		globalRetention = r.limits.RetentionPeriod(tenantID)
		streamRetention = r.limits.StreamRetention(tenantID)
	)

	// If no retention is configured, execute as-is.
	if globalRetention <= 0 && len(streamRetention) == 0 {
		return RetentionCheckResult{Params: params}
	}

	globalBoundary := today.Add(-globalRetention)
	if len(streamRetention) > 0 {
		return r.handleStreamRetention(params, tenantID, queryStart, globalRetention, globalBoundary, streamRetention, today)
	}

	// Only global retention configured.
	return r.handleGlobalRetention(params, tenantID, queryStart, queryEnd, globalRetention, globalBoundary)
}

// handleGlobalRetention handles the case where only global retention is configured.
func (r *retentionChecker) handleGlobalRetention(params logql.Params, tenantID string, queryStart, queryEnd time.Time, globalRetention time.Duration, globalBoundary time.Time) RetentionCheckResult {
	if globalRetention <= 0 {
		return RetentionCheckResult{Params: params}
	}

	// If query is entirely within retention, execute as-is.
	if !queryStart.Before(globalBoundary) {
		return RetentionCheckResult{Params: params}
	}

	// If query is entirely out of retention (query end is before or at boundary),
	// return empty response.
	if !queryEnd.After(globalBoundary) {
		level.Info(r.logger).Log(
			"msg", "query entirely out of retention, return empty result",
			"tenant", tenantID,
			"query_end", queryEnd,
			"retention_boundary", globalBoundary,
		)
		return RetentionCheckResult{EmptyResponse: true}
	}

	// Query overlaps with retention boundary - snap start to boundary.
	level.Info(r.logger).Log(
		"msg", "snapping query start to retention boundary",
		"tenant", tenantID,
		"original_start", queryStart,
		"adjusted_start", globalBoundary,
	)
	return RetentionCheckResult{
		Params: &adjustedParams{Params: params, adjustedStart: globalBoundary},
	}
}

// handleStreamRetention handles the case where stream retention is also configured.
func (r *retentionChecker) handleStreamRetention(params logql.Params, tenantID string, queryStart time.Time, globalRetention time.Duration, globalBoundary time.Time, streamRetention []validation.StreamRetention, today time.Time) RetentionCheckResult {
	// Find the smallest stream retention period (most restrictive).
	var smallestStreamRetention time.Duration
	for _, sr := range streamRetention {
		period := time.Duration(sr.Period)
		if smallestStreamRetention == 0 || period < smallestStreamRetention {
			smallestStreamRetention = period
		}
	}

	streamBoundary := today.Add(-smallestStreamRetention)

	// If either global or the most restrictive stream retention boundary is after query start,
	// we chose to not handle this - return unimplemented.
	if globalRetention > 0 && globalBoundary.After(queryStart) {
		level.Info(r.logger).Log(
			"msg", "query overlaps global retention boundary with stream retention configured, returning unimplemented",
			"tenant", tenantID,
			"query_start", queryStart,
			"global_boundary", globalBoundary,
			"stream_boundary", streamBoundary,
		)
		return RetentionCheckResult{
			Error: httpgrpc.Errorf(http.StatusNotImplemented,
				"query overlaps retention boundary and stream retention is configured - not supported by v2 engine"),
		}
	}

	if smallestStreamRetention > 0 && streamBoundary.After(queryStart) {
		level.Info(r.logger).Log(
			"msg", "query overlaps stream retention boundary, returning unimplemented",
			"tenant", tenantID,
			"query_start", queryStart,
			"global_boundary", globalBoundary,
			"stream_boundary", streamBoundary,
		)
		return RetentionCheckResult{
			Error: httpgrpc.Errorf(http.StatusNotImplemented,
				"query overlaps retention boundary and stream retention is configured - not supported by v2 engine"),
		}
	}

	// Both boundaries are before query start, so query is entirely within retention.
	return RetentionCheckResult{Params: params}
}

// adjustedParams wraps logql.Params with an adjusted start time.
type adjustedParams struct {
	logql.Params
	adjustedStart time.Time
}

func (a *adjustedParams) Start() time.Time {
	return a.adjustedStart
}
