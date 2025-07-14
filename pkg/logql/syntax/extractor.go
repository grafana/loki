package syntax

import (
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

const UnsupportedErr = "unsupported range vector aggregation operation: %s"

func (r RangeAggregationExpr) Extractors() ([]log.SampleExtractor, error) {
	ext, err := r.extractor(nil)
	if err != nil {
		return []log.SampleExtractor{}, err
	}
	return []log.SampleExtractor{ext}, nil
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

	// TODO(owen-d|cyriltovena): override grouping (i.e. from a parent `sum`)
	// technically can break the query.
	// For intance, in  `sum by (foo) (max_over_time by (bar) (...))`
	// the `by (bar)` grouping in the child is ignored in favor of the parent's `by (foo)`
	for _, grp := range []*Grouping{r.Grouping, override} {
		if grp != nil {
			groups = grp.Groups
			without = grp.Without
			noLabels = grp.Singleton()
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

func (m *MultiVariantExpr) Extractors() ([]log.SampleExtractor, error) {
	if m.err != nil {
		return nil, m.err
	}

	ext, err := m.extractor()
	if err != nil {
		return []log.SampleExtractor{}, err
	}
	return []log.SampleExtractor{ext}, nil
}

func (m *MultiVariantExpr) extractor() (log.SampleExtractor, error) {
	// Extract the common pipeline from the logRange's LogSelectorExpr
	commonPipeline, err := m.logRange.Left.Pipeline()
	if err != nil {
		return nil, fmt.Errorf("error extracting common pipeline: %w", err)
	}

	// Create variant-specific extractors
	variantExtractors := make([]log.SampleExtractor, 0, len(m.variants))

	for idx, variant := range m.variants {
		// We need to create specialized extractors for each variant that don't include the common pipeline
		if rangeAgg, ok := variant.(*RangeAggregationExpr); ok {
			// Create an extractor based on the range aggregation operation and unwrap expression
			variantExtractor, err := variantRangeAggExprExtractor(rangeAgg)
			if err != nil {
				return nil, fmt.Errorf("error creating extractor for variant %d: %w", idx, err)
			}

			// Wrap the extractor with variant index
			// variantExtractors = append(variantExtractors, log.NewVariantsSampleExtractorWrapper(idx, variantExtractor))
			variantExtractors = append(variantExtractors, variantExtractor)
		} else {
			// Fallback for other expression types - try using the regular Extractors() method
			// This might not work correctly if the variant depends on the common pipeline
			// but is not a RangeAggregationExpr
			es, err := variant.Extractors()
			if err != nil {
				return nil, fmt.Errorf("error extracting variant %d: %w", idx, err)
			}

			if len(es) != 1 {
				return nil, fmt.Errorf("expected 1 extractor for variant %d, got %d", idx, len(es))
			}

			// Wrap the extractor with variant index
			// variantExtractors = append(variantExtractors, log.NewVariantsSampleExtractorWrapper(idx, es[0]))
			variantExtractors = append(variantExtractors, es[0])
		}
	}

	// Create the consolidated extractor that uses the common pipeline and variant-specific extractors
	consolidatedExtractor := log.NewConsolidatedMultiVariantExtractor(
		commonPipeline, variantExtractors)

	return consolidatedExtractor, nil
}

func variantRangeAggExprExtractor(rangeAgg *RangeAggregationExpr) (log.SampleExtractor, error) {
	// Create specialized extractor depending on the operation type
	if rangeAgg.Left.Unwrap != nil {
		// For unwrap operations, we need a label extractor
		// Get grouping information
		var groups []string
		var without bool
		var noLabels bool

		if rangeAgg.Grouping != nil {
			groups = rangeAgg.Grouping.Groups
			without = rangeAgg.Grouping.Without
			noLabels = rangeAgg.Grouping.Singleton()
		}

		sort.Strings(groups)

		// Determine conversion operation
		var convOp string
		switch rangeAgg.Left.Unwrap.Operation {
		case OpConvBytes:
			convOp = log.ConvertBytes
		case OpConvDuration, OpConvDurationSeconds:
			convOp = log.ConvertDuration
		default:
			convOp = log.ConvertFloat
		}

		// Create label extractor without the common pipeline stages
		// The common pipeline will be applied separately
		return log.LabelExtractorWithStages(
			rangeAgg.Left.Unwrap.Identifier,
			convOp,
			groups,
			without,
			noLabels,
			nil, // No stages here - common pipeline will be applied separately
			log.ReduceAndLabelFilter(rangeAgg.Left.Unwrap.PostFilters),
		)
	}

	// For non-unwrap operations, we need a line extractor
	// Get grouping information
	var groups []string
	var without bool
	var noLabels bool

	if rangeAgg.Grouping != nil {
		groups = rangeAgg.Grouping.Groups
		without = rangeAgg.Grouping.Without
		noLabels = rangeAgg.Grouping.Singleton()
	}

	// absent_over_time cannot be grouped, set noLabels=true
	if rangeAgg.Operation == OpRangeTypeAbsent {
		noLabels = true
	}

	sort.Strings(groups)

	// Create line extractor based on operation type
	var lineExtractor log.LineExtractor
	switch rangeAgg.Operation {
	case OpRangeTypeRate, OpRangeTypeCount, OpRangeTypeAbsent:
		lineExtractor = log.CountExtractor
	case OpRangeTypeBytes, OpRangeTypeBytesRate:
		lineExtractor = log.BytesExtractor
	default:
		return nil, fmt.Errorf("unsupported range vector aggregation operation: %s", rangeAgg.Operation)
	}

	// Create a line sample extractor without the common pipeline stages
	// The common pipeline will be applied separately
	return log.NewLineSampleExtractor(
		lineExtractor,
		nil, // No stages here - common pipeline will be applied separately
		groups,
		without,
		noLabels,
	)
}
