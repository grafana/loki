package executor

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/prometheus/prometheus/model/labels"
)

func buildLabelfmtColumns(input arrow.RecordBatch, sourceCol *array.String, labelFmts []log.LabelFmt) ([]string, []arrow.Array) {
	parseFunc := func(row arrow.RecordBatch, line string) (map[string]string, error) {
		return tokenizeLabelfmt(input, line, labelFmts)
	}
	return buildColumns(input, sourceCol, nil, parseFunc, types.LabelfmtParserErrorType)
}

// tokenizeLabelfmt parses labelfmt input using the standard decoder
// Returns a map of key-value pairs with first-wins semantics for duplicates
func tokenizeLabelfmt(input arrow.RecordBatch, line string, labelFmts []log.LabelFmt) (map[string]string, error) {
	decoder, err := log.NewLabelsFormatter(labelFmts)
	if err != nil {
		return nil, fmt.Errorf("unable to create label formatter with formats %v", labelFmts)
	}
	lbls := buildLabelsFromInput(input)
	var builder = log.NewBaseLabelsBuilder().ForLabels(lbls, labels.StableHash(lbls))
	builder.Reset()
	builder.Add(log.StructuredMetadataLabel, buildLabelsFromInput(input))

	var timestampIdx = -1
	for i := 0; i < len(input.Columns()); i++ {
		if input.ColumnName(i) == "timestamp_ns.builtin.timestamp" {
			timestampIdx = i
			break
		}
	}
	if timestampIdx < 0 {
		return map[string]string{}, fmt.Errorf("unable to find timestamp column in inputs")
	}
	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z", input.Column(timestampIdx).ValueStr(0))
	if err != nil {
		panic(fmt.Sprintf("Unable to convert timestamp %v", input.Column(timestampIdx).ValueStr(0)))
	}

	decoder.Process(ts.UnixNano(), unsafeBytes(line), builder)
	result := builder.LabelsResult().Labels().Map()
	// result includes every single label from the input, not just the ones from labelFmts.
	// Remove the labels we don't care about and return only the new/adjusted labels.
	var relevantLabels = map[string]bool{}
	for _, label := range labelFmts {
		relevantLabels[label.Name] = true
	}
	for labelName := range result {
		if _, ok := relevantLabels[labelName]; !ok {
			delete(result, labelName)
		}
	}
	return result, nil
}

func buildLabelsFromInput(input arrow.RecordBatch) labels.Labels {
	var labelList []labels.Label
	for i := 0; i < int(input.NumCols()); i++ {
		labelList = append(labelList, labels.Label{Name: semconv.MustParseFQN(input.ColumnName(i)).ColumnRef().Column, Value: input.Column(i).ValueStr(0)})
	}
	return labels.New(labelList...)
}
