package executor

import (
	"bytes"
	"fmt"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

// parseFunc will be called once for each line, so we need to know which line of `input` corresponds to `line`
// also, sometimes input is a batch of 0 lines but we have a "line" string anyway?
func buildLinefmtColumns(input arrow.RecordBatch, sourceCol *array.String, lineFmt string) ([]string, []arrow.Array) {
	parseFunc := func(row arrow.RecordBatch, line string) (map[string]string, error) {
		return tokenizeLinefmt(row, line, lineFmt)
	}
	return buildColumns(input, sourceCol, nil, parseFunc, types.LabelfmtParserErrorType)
}

// tokenizeLinefmt parses linefmt input using the standard decoder
// Returns a map of key-value pairs with first-wins semantics for duplicates
func tokenizeLinefmt(input arrow.RecordBatch, line string, lineFmt string) (map[string]string, error) {
	result := make(map[string]string)

	formatter, err := NewFormatter(lineFmt)
	if err != nil {
		return nil, fmt.Errorf("unable to create line formatter with template %v", lineFmt)
	}
	if _, ok := formatter.Process(line, input, result); !ok {
		return nil, fmt.Errorf("unable to process line %v", line)
	}
	return result, nil
}

type LineFormatter struct {
	*template.Template
	buf *bytes.Buffer

	currentLine []byte
	currentTs   int64
	simpleKey   string
}

// NewFormatter creates a new log line formatter from a given text template.
func NewFormatter(tmpl string) (*LineFormatter, error) {
	lf := &LineFormatter{
		buf: bytes.NewBuffer(make([]byte, 4096)),
	}

	functions := log.AddLineAndTimestampFunctions(func() string {
		return unsafeString(lf.currentLine)
	}, func() int64 {
		return lf.currentTs
	})

	t, err := template.New("line").Option("missingkey=zero").Funcs(functions).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("invalid line template: %w", err)
	}
	lf.Template = t
	// determine if the template is a simple key substitution, e.g. line_format `{{.message}}`
	// if it is, save the key name and we can use it later to directly copy the string
	// bytes of the value to avoid copying and allocating a new string.
	if len(t.Root.Nodes) == 1 && t.Root.Nodes[0].Type() == parse.NodeAction {
		actionNode := t.Root.Nodes[0].(*parse.ActionNode)
		if len(actionNode.Pipe.Cmds) == 1 && len(actionNode.Pipe.Cmds[0].Args) == 1 {
			if fieldNode, ok := actionNode.Pipe.Cmds[0].Args[0].(*parse.FieldNode); ok && len(fieldNode.Ident) == 1 {
				lf.simpleKey = fieldNode.Ident[0]
			}
		}
	}

	return lf, nil
}

func (lf *LineFormatter) Process(line string, input arrow.RecordBatch, result map[string]string) (string, bool) {
	var messageIdx = -1
	for i := 0; i < len(input.Columns()); i++ {
		colIdent := semconv.MustParseFQN(input.ColumnName(i)).ColumnRef().Column
		if colIdent == "message" {
			messageIdx = i
			break
		}
	}
	if messageIdx < 0 {
		return "", false
	}
	if lf.simpleKey != "" {
		var simpleKeyIdx = -1
		for i := 0; i < len(input.Columns()); i++ {
			colIdent := semconv.MustParseFQN(input.ColumnName(i)).ColumnRef().Column
			if lf.simpleKey == colIdent {
				simpleKeyIdx = i
				break
			}
		}
		if simpleKeyIdx < 0 {
			return "", true
		}
		result[types.ColumnNameBuiltinMessage] = input.Column(simpleKeyIdx).ValueStr(0)
		return input.Column(simpleKeyIdx).ValueStr(0), true
	}
	var timestampIdx = -1
	for i := 0; i < len(input.Columns()); i++ {
		if input.ColumnName(i) == types.ColumnFullNameTimestamp {
			timestampIdx = i
			break
		}
	}
	if timestampIdx == -1 {
		panic("Unable to find timestamp column in inputs")
	}
	lf.buf.Reset()
	lf.currentLine = unsafeBytes(line)
	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z", input.Column(timestampIdx).ValueStr(0))
	if err != nil {
		panic(fmt.Sprintf("Unable to convert timestamp %v", input.Column(timestampIdx).ValueStr(0)))
	}
	lf.currentTs = ts.UnixNano()

	m := make(map[string]string)
	for i := 0; i < len(input.Columns()); i++ {
		m[semconv.MustParseFQN(input.ColumnName(i)).ColumnRef().Column] = input.Column(i).ValueStr(0)
	}

	if err := lf.Execute(lf.buf, m); err != nil {
		result[types.ColumnNameError] = "TemplateFormatErr " + err.Error()
		return line, true
	}
	result[types.ColumnNameBuiltinMessage] = lf.buf.String()

	return lf.buf.String(), true
}
