package logqlanalyzer

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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
	analyzer := NewPipelineAnalyzer(pipeline, streamLabels)
	response := &Result{StreamSelector: streamSelector, Stages: stages, Results: make([]LineResult, 0, len(logs))}
	for _, line := range logs {
		analysisRecords := analyzer.AnalyzeLine(line)
		response.Results = append(response.Results, mapAllToLineResult(line, analysisRecords))
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

func mapAllToLineResult(originLine string, analysisRecords []StageAnalysisRecord) LineResult {
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
	return LineResult{originLine, stageRecords}
}

func mapAllToLabelsResponse(labels labels.Labels) []Label {
	result := make([]Label, 0, len(labels))
	for _, label := range labels {
		result = append(result, Label{Name: label.Name, Value: label.Value})
	}
	return result
}

type PipelineAnalyzer interface {
	AnalyzeLine(line string) []StageAnalysisRecord
}
type noopPipelineAnalyzer struct {
}

func (n noopPipelineAnalyzer) AnalyzeLine(_ string) []StageAnalysisRecord {
	return []StageAnalysisRecord{}
}

type streamPipelineAnalyzer struct {
	origin       log.AnalyzablePipeline
	stagesCount  int
	streamLabels labels.Labels
}

func NewPipelineAnalyzer(origin log.Pipeline, streamLabels labels.Labels) PipelineAnalyzer {
	if o, ok := origin.(log.AnalyzablePipeline); ok {
		stagesCount := len(o.Stages())
		return &streamPipelineAnalyzer{o, stagesCount, streamLabels}
	}
	return &noopPipelineAnalyzer{}
}

func (p streamPipelineAnalyzer) AnalyzeLine(line string) []StageAnalysisRecord {
	stages := p.origin.Stages()
	stageRecorders := make([]log.Stage, 0, len(stages))
	records := make([]StageAnalysisRecord, len(stages))
	for i, stage := range stages {
		stageRecorders = append(stageRecorders, StageAnalysisRecorder{origin: stage,
			records:    records,
			stageIndex: i,
		})
	}
	stream := log.NewStreamPipeline(stageRecorders, p.origin.LabelsBuilder().ForLabels(p.streamLabels, p.streamLabels.Hash()))
	_, _, _ = stream.ProcessString(time.Now().UnixMilli(), line)
	return records
}

type StageAnalysisRecorder struct {
	log.Stage
	origin     log.Stage
	stageIndex int
	records    []StageAnalysisRecord
}

func (s StageAnalysisRecorder) Process(ts int64, line []byte, lbs *log.LabelsBuilder) ([]byte, bool) {
	lineBefore := string(line)
	labelsBefore := lbs.UnsortedLabels(nil)

	lineResult, ok := s.origin.Process(ts, line, lbs)

	s.records[s.stageIndex] = StageAnalysisRecord{
		Processed:    true,
		LabelsBefore: labelsBefore,
		LineBefore:   lineBefore,
		LabelsAfter:  lbs.UnsortedLabels(nil),
		LineAfter:    string(lineResult),
		FilteredOut:  !ok,
	}
	return lineResult, ok
}
func (s StageAnalysisRecorder) RequiredLabelNames() []string {
	return s.origin.RequiredLabelNames()
}

type StageAnalysisRecord struct {
	Processed    bool
	LineBefore   string
	LabelsBefore labels.Labels
	LineAfter    string
	LabelsAfter  labels.Labels
	FilteredOut  bool
}
