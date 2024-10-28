package deletion

import (
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func SetupPipeline(req logql.SelectLogParams, p log.Pipeline) (log.Pipeline, error) {
	if len(req.Deletes) == 0 {
		return p, nil
	}

	filters, err := deleteFilters(req.Deletes)
	if err != nil {
		return nil, err
	}

	return log.NewFilteringPipeline(filters, p), nil
}

func SetupExtractor(req logql.QueryParams, se log.SampleExtractor) (log.SampleExtractor, error) {
	if len(req.GetDeletes()) == 0 {
		return se, nil
	}

	filters, err := deleteFilters(req.GetDeletes())
	if err != nil {
		return nil, err
	}

	return log.NewFilteringSampleExtractor(filters, se), nil
}

func deleteFilters(deletes []*logproto.Delete) ([]log.PipelineFilter, error) {
	var filters []log.PipelineFilter
	for _, d := range deletes {
		expr, err := syntax.ParseLogSelector(d.Selector, true)
		if err != nil {
			return nil, err
		}

		pipeline, err := expr.Pipeline()
		if err != nil {
			return nil, err
		}

		filters = append(filters, log.PipelineFilter{
			Start:    d.Start,
			End:      d.End,
			Matchers: expr.Matchers(),
			Pipeline: pipeline,
		})
	}

	return filters, nil
}
