package syntax

import (
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

const UnsupportedErr = "unsupported range vector aggregation operation: %s"

func (r RangeAggregationExpr) Extractor() (log.SampleExtractor, error) {
	return r.extractor(nil)
}

// extractor creates a SampleExtractor but allows for the grouping to be overridden.
func (r RangeAggregationExpr) extractor(override *Grouping) (log.SampleExtractor, error) {
	if r.err != nil {
		return nil, r.err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}

	var (
		exprGroups []string
		without    bool
		noLabels   bool
	)

	// TODO(owen-d|cyriltovena): override grouping (i.e. from a parent `sum`)
	// technically can break the query.
	// For intance, in  `sum by (foo) (max_over_time by (bar) (...))`
	// the `by (bar)` grouping in the child is ignored in favor of the parent's `by (foo)`
	for _, grp := range []*Grouping{r.Grouping, override} {
		if grp != nil {
			exprGroups = grp.Groups
			without = grp.Without
			noLabels = grp.Singleton()
		}
	}

	// absent_over_time cannot be grouped (yet?), so set noLabels=true
	// to make extraction more efficient and less likely to strip per query series limits.
	if r.Operation == OpRangeTypeAbsent {
		noLabels = true
	}

	// We can't mutate the expression's groups in place (see Grouping.Groups doc), so we make
	// our own copy and sort it.
	sortedGroups := make([]string, len(exprGroups))
	copy(sortedGroups, exprGroups)
	sort.Strings(sortedGroups)

	stages, err := stagesFor(r.Left)
	if err != nil {
		return nil, err
	}
	// unwrap...means we want to extract metrics from labels.
	if r.Left.Unwrap != nil {
		var convOp string
		switch r.Left.Unwrap.Operation {
		case OpConvBytes:
			convOp = log.ConvertBytes
		case OpConvDuration, OpConvDurationSeconds:
			convOp = log.ConvertDuration
		default:
			convOp = log.ConvertFloat
		}

		return log.LabelExtractorWithStages(
			r.Left.Unwrap.Identifier,
			convOp, sortedGroups, without, noLabels, stages,
			log.ReduceAndLabelFilter(r.Left.Unwrap.PostFilters),
		)
	}
	// otherwise we extract metrics from the log line.
	switch r.Operation {
	case OpRangeTypeRate, OpRangeTypeCount, OpRangeTypeAbsent:
		return log.NewLineSampleExtractor(log.CountExtractor, stages, sortedGroups, without, noLabels)
	case OpRangeTypeBytes, OpRangeTypeBytesRate:
		return log.NewLineSampleExtractor(log.BytesExtractor, stages, sortedGroups, without, noLabels)
	default:
		return nil, fmt.Errorf(UnsupportedErr, r.Operation)
	}
}

func (e *LabelAggregationExpr) Extractor() (log.SampleExtractor, error) {
	if e.err != nil {
		return nil, e.err
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return distinctValueExtractor(e.Label, e.Left, e.Grouping)
}

func (e *CountDistinctSketchExpr) Extractor() (log.SampleExtractor, error) {
	if e.err != nil {
		return nil, e.err
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return distinctValueExtractor(e.Label, e.Left, e.Grouping)
}

func distinctValueExtractor(label string, left *LogRangeExpr, grouping *Grouping) (log.SampleExtractor, error) {
	var (
		groups   []string
		without  bool
		noLabels bool
	)
	switch {
	case grouping == nil:
		// Default grouping keeps stream labels, minus the counted field.
		groups = []string{label}
		without = true
	case grouping.Singleton():
		noLabels = true
	default:
		groups = grouping.Groups
	}
	// We can't mutate the expression's groups in place (see Grouping.Groups doc), so we make
	// our own copy and sort it.
	sortedGroups := make([]string, len(groups))
	copy(sortedGroups, groups)
	sort.Strings(sortedGroups)

	stages, err := stagesFor(left)
	if err != nil {
		return nil, err
	}

	return log.NewDistinctValueSampleExtractor(label, stages, sortedGroups, without, noLabels)
}

// stagesFor returns the stages of expr's pipeline, or nil when expr wraps a bare selector with
// no pipeline.
func stagesFor(expr *LogRangeExpr) ([]log.Stage, error) {
	p, ok := expr.Left.(*PipelineExpr)
	if !ok {
		return nil, nil
	}
	return p.MultiStages.stages()
}

// KeepsErroredLines reports whether expr's pipeline asks to keep the lines that carry __error__,
// so a metric query returns such a sample instead of failing. It answers for every error the
// pipeline can raise, including one that an unwrap conversion or its post-filters raise.
func KeepsErroredLines(expr *LogRangeExpr) (bool, error) {
	stages, err := stagesFor(expr)
	if err != nil {
		return false, err
	}
	if expr.Unwrap != nil {
		stages = append(stages, log.ReduceAndLabelFilter(expr.Unwrap.PostFilters))
	}
	return log.Stages(stages).Hints().KeepsErroredLines, nil
}
