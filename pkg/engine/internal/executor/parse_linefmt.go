package executor

import (
	"bytes"
	"fmt"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

// parseFunc will be called once for each line, so we need to know which line of `input` corresponds to `line`
// also, sometimes input is a batch of 0 lines but we have a "line" string anyway?
func buildLinefmtColumns(input arrow.RecordBatch, sourceCol *array.String, lineFmt string) ([]string, []arrow.Array) {
	formatter, err := NewFormatter(lineFmt)
	var parseFunc func(arrow.RecordBatch, string) (map[string]string, error)
	if err != nil {
		parseErr := fmt.Errorf("unable to create line formatter with template %v", lineFmt)
		parseFunc = func(_ arrow.RecordBatch, _ string) (map[string]string, error) {
			return nil, parseErr
		}
	} else {
		parseFunc = func(row arrow.RecordBatch, line string) (map[string]string, error) {
			return tokenizeLinefmt(row, line, formatter)
		}
	}
	return buildColumns(input, sourceCol, nil, parseFunc, types.VariadicOpParseLinefmt, types.LinefmtParserErrorType)
}

// tokenizeLinefmt parses linefmt input using the standard decoder
// Returns a map of key-value pairs with first-wins semantics for duplicates
func tokenizeLinefmt(input arrow.RecordBatch, line string, formatter *LineFormatter) (map[string]string, error) {
	result := make(map[string]string)

	if _, err := formatter.Process(line, input, result); err != nil {
		return result, err
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

func (lf *LineFormatter) Process(line string, input arrow.RecordBatch, result map[string]string) (string, error) {
	var messageIdx = -1
	for i := 0; i < len(input.Columns()); i++ {
		colIdent := semconv.MustParseFQN(input.ColumnName(i)).ColumnRef().Column
		if colIdent == "message" {
			messageIdx = i
			break
		}
	}
	if messageIdx < 0 {
		return "", fmt.Errorf("message column not found")
	}

	// Bucket input columns so `{{.X}}` lookups resolve deterministically
	// when the same short name exists at multiple categories and one side
	// is NULL for this row. builtin columns are looked up separately so
	// references like `{{.timestamp}}` keep working but cannot be shadowed
	// by a NULL label-like column of the same short name.
	stream, metadata, parsed := buildLabelsFromInput(input)
	builtin := buildBuiltinColumnsFromInput(input)

	if lf.simpleKey != "" {
		val, found := lookupCategorized(lf.simpleKey, stream, metadata, parsed, builtin)
		if !found {
			// Column absent in every category for this row. Match v1 and the
			// general template path's `missingkey=zero` semantic: render ""
			// silently, do not raise a parser error / __error__ label.
			result[types.ColumnNameBuiltinMessage] = ""
			return "", nil
		}
		result[types.ColumnNameBuiltinMessage] = val
		return val, nil
	}
	var timestampIdx = -1
	for i := 0; i < len(input.Columns()); i++ {
		if input.ColumnName(i) == types.ColumnFullNameTimestamp {
			timestampIdx = i
			break
		}
	}
	if timestampIdx == -1 {
		return "", fmt.Errorf("unable to find timestamp column in inputs")
	}
	lf.buf.Reset()
	lf.currentLine = unsafeBytes(line)
	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z", input.Column(timestampIdx).ValueStr(0))
	if err != nil {
		return "", err
	}
	lf.currentTs = ts.UnixNano()

	// Merge low → high precedence so later writes win: builtin → stream →
	// metadata → parsed. Matches v1's LabelsBuilder precedence
	// (parsed > metadata > stream); builtin (timestamp, message, value) is
	// the lowest tier so a user-set label of the same name still wins.
	m := make(map[string]string, len(builtin)+stream.Len()+metadata.Len()+parsed.Len())
	for k, v := range builtin {
		m[k] = v
	}
	stream.Range(func(l labels.Label) { m[l.Name] = l.Value })
	metadata.Range(func(l labels.Label) { m[l.Name] = l.Value })
	parsed.Range(func(l labels.Label) { m[l.Name] = l.Value })

	if err := lf.Execute(lf.buf, m); err != nil {
		return line, err
	}
	result[types.ColumnNameBuiltinMessage] = lf.buf.String()

	return lf.buf.String(), nil
}

// lookupCategorized resolves `name` in v1's category precedence
// (parsed > metadata > stream), falling back to builtin (timestamp,
// message, value) for v2's `{{.timestamp}}` style references. Returns
// ("", false) when absent everywhere — matches v1 + Go template
// `missingkey=zero`.
func lookupCategorized(name string, stream, metadata, parsed labels.Labels, builtin map[string]string) (string, bool) {
	if parsed.Has(name) {
		return parsed.Get(name), true
	}
	if metadata.Has(name) {
		return metadata.Get(name), true
	}
	if stream.Has(name) {
		return stream.Get(name), true
	}
	if v, ok := builtin[name]; ok {
		return v, true
	}
	return "", false
}

// buildBuiltinColumnsFromInput returns the row's builtin Arrow columns
// (timestamp, message, value) as short-name → value pairs, skipping NULL
// cells. Short names are unique within this category so iteration order
// is safe. Supports v2's `{{.timestamp}}` references in line templates
// without mixing them into the label-like buckets.
func buildBuiltinColumnsFromInput(input arrow.RecordBatch) map[string]string {
	m := map[string]string{}
	for i := 0; i < int(input.NumCols()); i++ {
		ident, err := semconv.ParseFQN(input.ColumnName(i))
		if err != nil {
			continue
		}
		if ident.ColumnType() != types.ColumnTypeBuiltin {
			continue
		}
		col := input.Column(i)
		if col.IsNull(0) {
			continue
		}
		m[ident.ColumnRef().Column] = col.ValueStr(0)
	}
	return m
}
