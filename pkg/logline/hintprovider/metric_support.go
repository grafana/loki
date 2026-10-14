package hintprovider

import "github.com/grafana/loki/v3/pkg/logql/syntax"

// SupportedMetricQuery returns the logline needles for a metric query whose
// single logs stage is eligible. Metric queries are not supported yet, so
// this returns nil. metric_support_test.go is the spec.
func SupportedMetricQuery(_ syntax.Expr, _ int) []string {
	return nil
}
