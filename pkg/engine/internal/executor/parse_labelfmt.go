package executor

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

func buildLabelfmtColumns(input arrow.RecordBatch, sourceCol *array.String, labelFmts []log.LabelFmt) ([]string, []arrow.Array) {
	var parseFunc func(arrow.RecordBatch, string) (map[string]string, error)
	decoder, err := log.NewLabelsFormatter(labelFmts)
	if err != nil {
		parseErr := fmt.Errorf("unable to create label formatter with template %v", labelFmts)
		parseFunc = func(_ arrow.RecordBatch, _ string) (map[string]string, error) {
			return nil, parseErr
		}

	} else {
		parseFunc = func(row arrow.RecordBatch, line string) (map[string]string, error) {
			return tokenizeLabelfmt(row, line, decoder, labelFmts)
		}
	}
	return buildColumns(input, sourceCol, nil, parseFunc, types.VariadicOpParseLabelfmt, types.LabelfmtParserErrorType)
}

// tokenizeLabelfmt parses labelfmt input using the standard decoder.
// Returns a map of key-value pairs containing only the labelfmt targets (and
// the error/error_details builtins). Categories from the input batch are
// preserved when populating the LabelsBuilder so that GetWithCategory honours
// v1's parsed > structured-metadata > stream precedence.
func tokenizeLabelfmt(input arrow.RecordBatch, line string, decoder *log.LabelsFormatter, labelFmts []log.LabelFmt) (map[string]string, error) {
	stream, metadata, parsed := buildLabelsFromInput(input)
	var builder = log.NewBaseLabelsBuilder().ForLabels(stream, labels.StableHash(stream))
	builder.Reset()
	builder.Add(log.StructuredMetadataLabel, metadata)
	builder.Add(log.ParsedLabel, parsed)

	var timestampIdx = -1
	for i := 0; i < len(input.Columns()); i++ {
		if input.ColumnName(i) == types.ColumnFullNameTimestamp {
			timestampIdx = i
			break
		}
	}
	if timestampIdx < 0 {
		return map[string]string{}, fmt.Errorf("unable to find timestamp column in inputs")
	}
	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z", input.Column(timestampIdx).ValueStr(0))
	if err != nil {
		return map[string]string{}, fmt.Errorf("unable to convert timestamp %v", input.Column(timestampIdx).ValueStr(0))
	}

	decoder.Process(ts.UnixNano(), unsafeBytes(line), builder)
	result := builder.LabelsResult().Labels().Map()
	// result includes every single label from the input, not just the ones from labelFmts.
	// Remove the labels we don't care about and return only the new/adjusted labels.
	var relevantLabels = map[string]bool{}
	for _, label := range labelFmts {
		relevantLabels[label.Name] = true
	}
	relevantLabels[semconv.ColumnIdentError.ShortName()] = true
	relevantLabels[semconv.ColumnIdentErrorDetails.ShortName()] = true
	for labelName := range result {
		if _, ok := relevantLabels[labelName]; !ok {
			delete(result, labelName)
		}
	}
	return result, nil
}

// buildLabelsFromInput returns the row's input labels bucketed by their
// Arrow column category, matching v1's stream / structured-metadata / parsed
// triplet. NULL cells are skipped — a same-short-name column at a different
// category can be NULL for this row (whether contributed by another stream
// in the same dataobj section, or NULL'd in-place by an upstream
// ColumnCompat), and treating that NULL as an empty string would shadow the
// surviving entry and make GetWithCategory's result non-deterministic.
//
// Generated columns (e.g. __error__, __error_details__) are bucketed with
// parsed labels to match v1, where lbs.SetErr / SetErrorDetails write into
// the ParsedLabel category. Builtin (timestamp, message) and ambiguous
// columns are excluded — they are not part of the label set.
func buildLabelsFromInput(input arrow.RecordBatch) (stream, metadata, parsed labels.Labels) {
	var streamList, metadataList, parsedList []labels.Label
	for i := 0; i < int(input.NumCols()); i++ {
		ident, err := semconv.ParseFQN(input.ColumnName(i))
		if err != nil {
			continue
		}
		col := input.Column(i)
		if col.IsNull(0) {
			// Column present in the schema but no value on this row — treat
			// as label absent (matches v1, which has no NULL state).
			continue
		}
		l := labels.Label{Name: ident.ColumnRef().Column, Value: col.ValueStr(0)}
		switch ident.ColumnType() {
		case types.ColumnTypeLabel:
			streamList = append(streamList, l)
		case types.ColumnTypeMetadata:
			metadataList = append(metadataList, l)
		case types.ColumnTypeParsed, types.ColumnTypeGenerated:
			parsedList = append(parsedList, l)
		}
	}
	return labels.New(streamList...), labels.New(metadataList...), labels.New(parsedList...)
}
