package logql

import (
	"context"
	"strconv"

	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/logql/log"
)

// optimizeSampleExpr Attempt to optimize the SampleExpr to another that will run faster but will produce the same result.
func optimizeSampleExpr(expr SampleExpr) (SampleExpr, error) {
	var skip bool
	// we skip sharding AST for now, it's not easy to clone them since they are not part of the language.
	expr.Walk(func(e interface{}) {
		switch e.(type) {
		case *ConcatSampleExpr, *DownstreamSampleExpr:
			skip = true
			return
		}
	})
	if skip {
		return expr, nil
	}
	// clone the expr.
	q := expr.String()
	expr, err := ParseSampleExpr(q)
	if err != nil {
		return nil, err
	}
	removeLineformat(expr)
	return expr, nil
}

// removeLineformat removes unnecessary line_format within a SampleExpr.
func removeLineformat(expr SampleExpr) {
	expr.Walk(func(e interface{}) {
		rangeExpr, ok := e.(*RangeAggregationExpr)
		if !ok {
			return
		}
		// bytes operation count bytes of the log line so line_format changes the result.
		if rangeExpr.Operation == OpRangeTypeBytes ||
			rangeExpr.Operation == OpRangeTypeBytesRate {
			return
		}
		pipelineExpr, ok := rangeExpr.Left.Left.(*PipelineExpr)
		if !ok {
			return
		}
		temp := pipelineExpr.MultiStages[:0]
		for i, s := range pipelineExpr.MultiStages {
			_, ok := s.(*LineFmtExpr)
			if !ok {
				temp = append(temp, s)
				continue
			}
			// we found a lineFmtExpr, we need to check if it's followed by a labelParser or lineFilter
			// in which case it could be useful for further processing.
			var found bool
			for j := i; j < len(pipelineExpr.MultiStages); j++ {
				if _, ok := pipelineExpr.MultiStages[j].(*LabelParserExpr); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.MultiStages[j].(*LineFilterExpr); ok {
					found = true
					break
				}
			}
			if found {
				// we cannot remove safely the linefmtExpr.
				temp = append(temp, s)
			}
		}
		pipelineExpr.MultiStages = temp
		// transform into a matcherExpr if there's no more pipeline.
		if len(pipelineExpr.MultiStages) == 0 {
			rangeExpr.Left.Left = &MatchersExpr{
				matchers: rangeExpr.Left.Left.Matchers(),
			}
		}
	})
}

// optimizeLogSelectorExpr Attempt to optimize the v to another that will run faster but will produce the same result.
func optimizeLogSelectorExpr(ctx context.Context, expr LogSelectorExpr) (LogSelectorExpr, error) {
	var err error
	logqlExpr := expr.String()
	defer func() {
		if panicErr := recover(); panicErr != nil {
			err = errors.New("optimizeLogSelectorExpr error")
			return
		}
		if sp := ot.SpanFromContext(ctx); sp != nil {
			if logqlExpr == expr.String() {
				sp.SetTag("optimize", false)
			} else {
				sp.SetTag("optimize", true)
			}
			sp.LogFields(otlog.String("expr", logqlExpr))
			sp.LogFields(otlog.String("optimizeLogSelectorExpr", expr.String()))
		}
	}()
	var skip bool
	var hasParserTypeJSON bool
	requiredJsonLabels := labels.Labels{}
	// we skip sharding AST for now, it's not easy to clone them since they are not part of the language.
	expr.Walk(func(e interface{}) {
		switch expr := e.(type) {
		case *LabelParserExpr:
			switch expr.Op {
			case OpParserTypeJSON:
				hasParserTypeJSON = true
			}
		case *LineFmtExpr:
			//TODO:@liguozhong handle "linefmt" case logql syntax,This case is more complicated, and the next PR will handle it.
			skip = true
		case *LabelFilterExpr:
			stage, err := expr.Stage()
			if err != nil {
				return
			}
			switch labelFilterer := stage.(type) {
			case *log.DurationLabelFilter:
				requiredJsonLabels = append(requiredJsonLabels, labels.Label{Name: labelFilterer.Name, Value: labelFilterer.Value.String()})
			case *log.StringLabelFilter:
				requiredJsonLabels = append(requiredJsonLabels, labels.Label{Name: labelFilterer.Name, Value: labelFilterer.Value})
			case *log.NumericLabelFilter:
				requiredJsonLabels = append(requiredJsonLabels, labels.Label{Name: labelFilterer.Name, Value: strconv.FormatFloat(labelFilterer.Value, 'E', -1, 64)})
			case *log.BytesLabelFilter:
				requiredJsonLabels = append(requiredJsonLabels, labels.Label{Name: labelFilterer.Name, Value: strconv.FormatUint(labelFilterer.Value, 10)})
			}
		}

	})
	if skip {
		return expr, nil
	}
	if !hasParserTypeJSON {
		return expr, nil
	}
	if len(requiredJsonLabels) == 0 {
		return expr, nil
	}
	// clone the expr.
	q := expr.String()
	expr, err = ParseLogSelector(q, true)
	if err != nil {
		return nil, err
	}
	replaceFastJsonParserAndAppendJsonParser(expr, requiredJsonLabels)
	return expr, nil
}

// replaceFastJsonParserAndAppendJsonParser replace fastJsonParser and append JsonParser within a LogSelectorExpr.
func replaceFastJsonParserAndAppendJsonParser(expr LogSelectorExpr, requiredJsonLabels labels.Labels) {
	requiredParams, err := requiredJsonLabels.MarshalJSON()
	if err != nil {
		return
	}
	jsonHintParam := string(requiredParams)
	//1: replace fastJsonParser
	//2: append simple JsonParser will produce the same result
	expr.Walk(func(e interface{}) {
		pipelineExpr, ok := e.(*PipelineExpr)
		if !ok {
			return
		}
		stages := pipelineExpr.MultiStages
		var fastParseExpr *LabelParserExpr
		for _, stageExpr := range stages {
			stage, err := stageExpr.Stage()
			if err != nil {
				return
			}
			_, ok := stage.(*log.JSONParser)
			if !ok {
				continue
			}
			lpe, ok := stageExpr.(*LabelParserExpr)
			if !ok {
				continue
			}
			lpe.Hint = jsonHintParam
			if fastParseExpr == nil {
				fastParseExpr = newLabelParserExpr(lpe.Op, "")
			}
		}
		pipelineExpr.MultiStages = append(pipelineExpr.MultiStages, fastParseExpr)
	})
}
