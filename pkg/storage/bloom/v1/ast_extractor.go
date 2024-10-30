package v1

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// LabelMatcher represents bloom tests for key-value pairs, mapped from
// LabelFilterExprs from the AST.
type LabelMatcher interface{ isLabelMatcher() }

// UnsupportedLabelMatcher represents a label matcher which could not be
// mapped. Bloom tests for UnsupportedLabelMatchers must always pass.
type UnsupportedLabelMatcher struct{}

// PlainLabelMatcher represents a direct key-value matcher. Bloom tests
// must only pass if the key-value pair exists in the bloom.
type PlainLabelMatcher struct{ Key, Value string }

// OrLabelMatcher represents a logical OR test. Bloom tests must only pass if
// one of the Left or Right label matcher bloom tests pass.
type OrLabelMatcher struct{ Left, Right LabelMatcher }

// AndLabelMatcher represents a logical AND test. Bloom tests must only pass
// if both of the Left and Right label matcher bloom tests pass.
type AndLabelMatcher struct{ Left, Right LabelMatcher }

type seriesLabelNames map[string]struct{}

func newSeriesLabelNames(series []labels.Labels) seriesLabelNames {
	lblNames := make(seriesLabelNames, len(series))
	for _, lbls := range series {
		for _, lbl := range lbls {
			lblNames[lbl.Name] = struct{}{}
		}
	}
	return lblNames
}

func (s seriesLabelNames) contains(name string) bool {
	_, ok := s[name]
	return ok
}

// ExtractTestableLabelMatchers extracts label matchers from the label filters
// in an expression. The resulting label matchers can then be used for testing
// against bloom filters. Only label matchers before the first parse stage are
// included.
// If series are provided, labels matchers that are part of the series are not included
//
// Unsupported LabelFilterExprs map to an UnsupportedLabelMatcher, for which
// bloom tests should always pass.
func ExtractTestableLabelMatchers(expr syntax.Expr, series ...labels.Labels) []LabelMatcher {
	if expr == nil {
		return nil
	}
	filters := syntax.ExtractLabelFiltersBeforeParser(expr)

	// Only check series labels if we pass in series.
	var seriesLbs seriesLabelNames
	if len(series) > 0 {
		seriesLbs = newSeriesLabelNames(series)
	}

	return buildLabelMatchers(seriesLbs, filters)
}

func buildLabelMatchers(series seriesLabelNames, exprs []*syntax.LabelFilterExpr) []LabelMatcher {
	matchers := make([]LabelMatcher, 0, len(exprs))
	for _, expr := range exprs {
		matchers = append(matchers, buildLabelMatcher(series, expr.LabelFilterer))
	}
	return matchers
}

func buildLabelMatcher(series seriesLabelNames, filter log.LabelFilterer) LabelMatcher {
	switch filter := filter.(type) {

	case *log.LineFilterLabelFilter:
		if filter.Type != labels.MatchEqual {
			return UnsupportedLabelMatcher{}
		}

		// Do not filter by labels that are part of the series.
		if series.contains(filter.Name) {
			return UnsupportedLabelMatcher{}
		}

		return PlainLabelMatcher{
			Key:   filter.Name,
			Value: filter.Value,
		}

	case *log.StringLabelFilter:
		if filter.Type != labels.MatchEqual {
			return UnsupportedLabelMatcher{}
		}

		// Do not filter by labels that are part of the series.
		if series.contains(filter.Name) {
			return UnsupportedLabelMatcher{}
		}

		return PlainLabelMatcher{
			Key:   filter.Name,
			Value: filter.Value,
		}

	case *log.BinaryLabelFilter:
		var (
			left  = buildLabelMatcher(series, filter.Left)
			right = buildLabelMatcher(series, filter.Right)
		)

		if filter.And {
			return AndLabelMatcher{Left: left, Right: right}
		}
		return OrLabelMatcher{Left: left, Right: right}

	default:
		return UnsupportedLabelMatcher{}
	}
}

//
// Implement marker types:
//

func (UnsupportedLabelMatcher) isLabelMatcher() {}
func (PlainLabelMatcher) isLabelMatcher()       {}
func (OrLabelMatcher) isLabelMatcher()          {}
func (AndLabelMatcher) isLabelMatcher()         {}
