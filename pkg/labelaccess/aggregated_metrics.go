package labelaccess

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// isQueryRequest checks if the request is a Loki query
func isQueryRequest(r *http.Request) bool {
	path := r.URL.Path
	isQuery := strings.Contains(path, "/loki/api/v1/query") ||
		strings.Contains(path, "/loki/api/v1/query_range")

	return isQuery
}

// ModifyAggregatedMetricsQuery modifies Loki queries for aggregated metrics
// to include label-based access control (LBAC) filters.
//
// For aggregated metrics, labels are stored inside the log content rather than
// as stream labels, so we need to apply LBAC filters after parsing the log content
// with the logfmt parser. This function:
//  1. Identifies queries targeting aggregated metrics
//  2. Extracts LBAC policies for the tenant
//  3. Builds filter expressions combining all policies (ORed together)
//  4. Injects a logfmt parser and label filter into the FRONT of query pipeline
//  5. Updates the request URL with the modified query
//
// The resulting query will contain a pipeline that parses log lines with logfmt
// and filters out lines that don't match the LBAC policies, ensuring that
// users only see aggregated metrics they have permission to access.
func ModifyAggregatedMetricsQuery(r *http.Request, matchers LabelPolicySet) error {
	// Get the query parameter
	query := r.URL.Query().Get("query")
	if query == "" {
		return nil
	}

	// Extract the OrgID from the request
	orgID := r.Header.Get("X-Scope-OrgID")
	if orgID == "" {
		return nil
	}

	// Get the policies for this tenant
	policies, exists := matchers[orgID]
	if !exists || len(policies) == 0 {
		return nil
	}

	// Build filter expressions for each policy
	policyFilterer, err := buildPolicyFilter(policies)
	if err != nil {
		return err
	}

	logfmtParser := syntax.MultiStageExpr{
		&syntax.LogfmtParserExpr{},
		&syntax.LabelFilterExpr{
			LabelFilterer: policyFilterer,
		},
	}

	// Parse the original query using the LogQL parser
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		_ = level.Error(util_log.Logger).Log(
			"msg", "failed to parse aggregated metrics query",
			"query", query,
			"error", err,
		)
		return err
	}

	// Check if this is a query for aggregated metrics streams
	if !isAggregatedMetricQueryString(expr) {
		return nil
	}

	var modifiedQuery string
	switch e := expr.(type) {
	case syntax.SampleExpr:
		// For sample expressions (metric queries), we need to identify the log selector
		// and add our filter to that part before any range operations
		modifiedQuery, err = modifySampleExpr(e, logfmtParser)
		if err != nil {
			return err
		}
	case syntax.LogSelectorExpr:
		modifiedQuery, err = modifyLogSelectorExpr(e, logfmtParser)
		if err != nil {
			return err
		}
	}

	if modifiedQuery != "" && modifiedQuery != query {
		// Update the query parameter
		q := r.URL.Query()
		q.Set("query", modifiedQuery)
		r.URL.RawQuery = q.Encode()

		_ = level.Info(util_log.Logger).Log(
			"msg", "modified query for aggregated metrics to respect LBAC",
			"original", query,
			"modified", modifiedQuery,
			"orgID", orgID,
			"policyCount", len(policies),
		)
	}

	return nil
}

func prependPipelineExpr(e *syntax.PipelineExpr, logfmtParser syntax.MultiStageExpr) (*syntax.PipelineExpr, error) {
	e.MultiStages = append(logfmtParser, e.MultiStages...)
	return e, nil
}

// findRangeAggregationExpr attempts to find a RangeAggregationExpr within a SampleExpr
// This helps us locate where to insert our filter in complex metric queries
func findRangeAggregationExpr(expr syntax.SampleExpr) (*syntax.RangeAggregationExpr, bool) {
	switch e := expr.(type) {
	case *syntax.RangeAggregationExpr:
		return e, true
	case *syntax.VectorAggregationExpr:
		return findRangeAggregationExpr(e.Left)
	default:
		return nil, false
	}
}

// isAggregatedMetricQueryString checks if a query string targets aggregated metrics
func isAggregatedMetricQueryString(expr syntax.Expr) bool {
	var matchers []*labels.Matcher
	switch e := expr.(type) {
	case syntax.SampleExpr:
		selector, err := e.Selector()
		if err != nil {
			return false
		}

		matchers = selector.Matchers()
	case syntax.LogSelectorExpr:
		matchers = e.Matchers()
	default:
		return false
	}

	if len(matchers) == 0 {
		return false
	}

	for _, matcher := range matchers {
		if matcher.Name == constants.AggregatedMetricLabel {
			return true
		}
	}

	return false
}

// modifySampleExpr modifies a SampleExpr to add LBAC filtering via a logfmt parser
// This is used for metric queries based on aggregated metrics
func modifySampleExpr(expr syntax.SampleExpr, logfmtParser syntax.MultiStageExpr) (string, error) {
	rangeExpr, ok := findRangeAggregationExpr(expr)
	if !ok {
		return "", fmt.Errorf("unsupported aggregated metric expression")
	}

	// Extract the log selector from the range expression
	logSelector, err := rangeExpr.Selector()
	if err != nil {
		return "", err
	}

	// Create a new pipeline expression with our modified pipeline
	var pipelineExpr *syntax.PipelineExpr

	if existingPipeline, ok := logSelector.(*syntax.PipelineExpr); ok {
		// If there's already a pipeline, prepend our stages
		pipelineExpr, err = prependPipelineExpr(existingPipeline, logfmtParser)
		if err != nil {
			return "", err
		}
	} else if matchersExpr, ok := logSelector.(*syntax.MatchersExpr); ok {
		// If it's just matchers, create a new pipeline
		pipelineExpr = &syntax.PipelineExpr{
			Left:        matchersExpr,
			MultiStages: logfmtParser,
		}
	} else {
		return "", fmt.Errorf("unsupported log selector type %T", logSelector)
	}

	// Update the log selector in the range expression
	if pipelineExpr != nil {
		rangeExpr.Left.Left = pipelineExpr
	}

	return expr.String(), nil
}

// modifyLogSelectorExpr modifies a LogSelectorExpr to add LBAC filtering via a logfmt parser
// This is used for log queries of aggregated metrics
func modifyLogSelectorExpr(expr syntax.LogSelectorExpr, logfmtParser syntax.MultiStageExpr) (string, error) {
	var pipelineExpr syntax.LogSelectorExpr

	switch e := expr.(type) {
	case *syntax.MatchersExpr:
		pipelineExpr = &syntax.PipelineExpr{
			Left:        e,
			MultiStages: logfmtParser,
		}
	case *syntax.PipelineExpr:
		var err error
		pipelineExpr, err = prependPipelineExpr(e, logfmtParser)
		if err != nil {
			return "", err
		}
	default:
		_ = level.Error(util_log.Logger).Log(
			"msg", "unexpected LogSelectorExpr type",
			"type", fmt.Sprintf("%T", e),
		)
		return "", fmt.Errorf("unexpected LogSelectorExpr type %T", e)
	}

	return pipelineExpr.String(), nil
}

// buildPolicyFilter creates a LabelFilterer from a list of label policies
// The filter will pass logs that match any of the policies (policies are ORed),
// while conditions within a policy must all match (conditions are ANDed)
func buildPolicyFilter(policies []*types.LabelPolicy) (logql_log.LabelFilterer, error) {
	var policyFilterer logql_log.LabelFilterer

	for _, policy := range policies {
		var labelFilterer logql_log.LabelFilterer

		for i := range policy.Selector {
			matcher, err := types.LabelMatcherToPromLabel(policy.Selector[i])
			if err != nil {
				return nil, err
			}

			if labelFilterer == nil {
				labelFilterer = logql_log.NewStringLabelFilter(matcher)
			} else {
				labelFilterer = logql_log.NewAndLabelFilter(labelFilterer, logql_log.NewStringLabelFilter(matcher))
			}
		}

		if policyFilterer == nil {
			policyFilterer = labelFilterer
		} else {
			policyFilterer = logql_log.NewOrLabelFilter(policyFilterer, labelFilterer)
		}
	}

	// policyFilter can be nil if the customer's x-prom-label-policy is an empty label matcher (e.g. {})
	// this condition may leave the policy.Selector array empty, which means we don't set the policyFilterer
	if policyFilterer == nil {
		return nil, errors.New("failed to parse policy filter from prometheus label matchers")
	}

	return policyFilterer, nil
}
