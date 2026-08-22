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

	var stages []log.Stage
	if p, ok := r.Left.Left.(*PipelineExpr); ok {
		// if the expression is a pipeline then take all stages into account first.
		st, err := p.MultiStages.stages()
		if err != nil {
			return nil, err
		}
		stages = st
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
	var groups []string
	if grouping != nil {
		groups = grouping.Groups
	}
	// We can't mutate the expression's groups in place (see Grouping.Groups doc), so we make
	// our own copy and sort it.
	sortedGroups := make([]string, len(groups))
	copy(sortedGroups, groups)
	sort.Strings(sortedGroups)

	var stages []log.Stage
	if p, ok := left.Left.(*PipelineExpr); ok {
		st, err := p.MultiStages.stages()
		if err != nil {
			return nil, err
		}
		stages = st
	}

	return log.NewDistinctValueSampleExtractor(label, stages, sortedGroups)
}
