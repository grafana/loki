package syntax

import (
	"fmt"
	"sort"

	"github.com/grafana/loki/pkg/logql/log"
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
	if err := r.validate(); err != nil {
		return nil, err
	}
	var groups []string
	var without bool
	var noLabels bool

	if r.Grouping != nil {
		groups = r.Grouping.Groups
		without = r.Grouping.Without
		if len(groups) == 0 {
			noLabels = true
		}
	}

	// uses override if it exists
	if override != nil {
		groups = override.Groups
		without = override.Without
		if len(groups) == 0 {
			noLabels = true
		}
	}

	// absent_over_time cannot be grouped (yet?), so set noLabels=true
	// to make extraction more efficient and less likely to strip per query series limits.
	if r.Operation == OpRangeTypeAbsent {
		noLabels = true
	}

	sort.Strings(groups)

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
			convOp, groups, without, noLabels, stages,
			log.ReduceAndLabelFilter(r.Left.Unwrap.PostFilters),
		)
	}
	// otherwise we extract metrics from the log line.
	switch r.Operation {
	case OpRangeTypeRate, OpRangeTypeCount, OpRangeTypeAbsent:
		return log.NewLineSampleExtractor(log.CountExtractor, stages, groups, without, noLabels)
	case OpRangeTypeBytes, OpRangeTypeBytesRate:
		return log.NewLineSampleExtractor(log.BytesExtractor, stages, groups, without, noLabels)
	default:
		return nil, fmt.Errorf(UnsupportedErr, r.Operation)
	}
}
