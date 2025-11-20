package syntax

// Re-export LogQL syntax functionality from the main v3 module.
// NOTE: This package depends on github.com/prometheus/prometheus/model/labels
// which is a lightweight labels package (not the full Prometheus server).
// This maintains code reuse while accepting a minimal Prometheus dependency.

import (
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

// Labels is an alias for Prometheus labels.Labels.
// This maintains compatibility with the v3 module.
type Labels = labels.Labels

// EmptyLabels returns an empty Labels slice.
func EmptyLabels() Labels {
	return labels.EmptyLabels()
}

// ParseLabels parses a LogQL label selector string and returns Labels.
// This is a re-export from the v3 module to maintain code reuse.
func ParseLabels(lbs string) (Labels, error) {
	return syntax.ParseLabels(lbs)
}

// ParseExpr parses a full LogQL expression.
// This is a re-export from the v3 module.
func ParseExpr(input string) (syntax.Expr, error) {
	return syntax.ParseExpr(input)
}

// ParseExprWithoutValidation parses a LogQL expression without validation.
// This is a re-export from the v3 module.
func ParseExprWithoutValidation(input string) (syntax.Expr, error) {
	return syntax.ParseExprWithoutValidation(input)
}

// Re-export the Expr interface and related types for convenience
type (
	Expr            = syntax.Expr
	LogSelectorExpr = syntax.LogSelectorExpr
	SampleExpr      = syntax.SampleExpr
)
