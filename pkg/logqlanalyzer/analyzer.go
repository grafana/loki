package logqlanalyzer

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/syntax"
)

type logQLAnalyzer struct {
}

func (a logQLAnalyzer) analyze(query string, logs []string) (*Result, error) {
	expr, err := syntax.ParseLogSelector(query, true)
	if err != nil {
		return nil, errors.Wrap(err, "invalid query")
	}
	streamSelector, stages, err := a.extractExpressionParts(expr)
	if err != nil {
		return nil, errors.Wrap(err, "can not extract parts of expression")
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, errors.Wrap(err, "can not create pipeline")
	}
	streamLabels, err := parser.ParseMetric(streamSelector)
	if err != nil {
		return nil, errors.Wrap(err, "can not parse labels from stream selector")
	}
	analyzer := log.NewPipelineAnalyzer(pipeline, streamLabels)
	response := &Result{StreamSelector: streamSelector, Stages: stages, Results: make([][]StageRecord, 0, len(logs))}
	for _, line := range logs {
		analysisRecords := analyzer.AnalyzeLine(line)
		response.Results = append(response.Results, mapAllToStageRecord(analysisRecords))
	}
	return response, nil
}

func (a logQLAnalyzer) extractExpressionParts(expr syntax.LogSelectorExpr) (string, []string, error) {
	switch expr := expr.(type) {
	case *syntax.PipelineExpr:
		stages := make([]string, 0, len(expr.MultiStages)+1)
		streamSelector := expr.Left.String()
		for _, stage := range expr.MultiStages {
			stages = append(stages, stage.String())
		}
		return streamSelector, stages, nil
	case *syntax.MatchersExpr:
		return expr.String(), []string{}, nil
	default:
		return "", nil, fmt.Errorf("unsupported type of expression")
	}

}

func mapAllToStageRecord(analysisRecords []log.StageAnalysisRecord) []StageRecord {
	stageRecords := make([]StageRecord, 0, len(analysisRecords))
	for _, record := range analysisRecords {
		if !record.Processed {
			break
		}
		stageRecords = append(stageRecords, StageRecord{
			LineBefore:   record.LineBefore,
			LabelsBefore: mapAllToLabelsResponse(record.LabelsBefore),
			LineAfter:    record.LineAfter,
			LabelsAfter:  mapAllToLabelsResponse(record.LabelsAfter),
			FilteredOut:  record.FilteredOut,
		})
	}
	return stageRecords
}

func mapAllToLabelsResponse(labels labels.Labels) []Label {
	result := make([]Label, 0, len(labels))
	for _, label := range labels {
		result = append(result, Label{Name: label.Name, Value: label.Value})
	}
	return result
}
