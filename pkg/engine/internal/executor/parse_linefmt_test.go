package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestLinefmtParser_Process(t *testing.T) {
	timestamp := time.Now().In(time.UTC).Add(time.Duration(1) * time.Second)

	const realPod = "querier-1234"

	tests := []struct {
		name    string
		line    string
		timeVal arrow.Timestamp
		// cols is the list of label-like / metadata / parsed columns to
		// append after the builtin message + timestamp columns. Order is
		// preserved in the Arrow schema so duplicate-short-name cases can
		// validate the line_format result is order-independent.
		cols    []labelfmtTestColumn
		lineFmt string
		want    string
	}{
		{
			name:    "simple replacement",
			line:    "this is my test line of logs",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			lineFmt: "{{.namespace}}",
			want:    "dev",
		},
		{
			name:    "replacement plus addition",
			line:    "this is my test line of logs",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			lineFmt: "{{.namespace}} foo",
			want:    "dev foo",
		},
		{
			name:    "timestamp",
			line:    "this is my test line of logs",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			lineFmt: "{{.timestamp}} foo",
			want:    timestamp.In(time.UTC).Format("2006-01-02T15:04:05.999999999Z") + " foo",
		},
		{
			// Null entries on a parsed/metadata column must render as "" to match v1 engine.
			name:    "simple-key path on null entry renders as empty",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", null: true}},
			lineFmt: "{{.namespace}}",
			want:    "",
		},
		{
			// Same intent as above but exercises the general template path
			// (mixed text + field reference).
			name:    "general template path on null entry renders as empty",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", null: true}},
			lineFmt: "{{.namespace}} foo",
			want:    " foo",
		},
		{
			// `{{.foo}}` references a key that is not in the schema (no column
			// named "foo" was ever created). Simple-key path previously raised a
			// "missing key" parser error which surfaced as an __error__ label on
			// the row; v1 silently renders "" via Go template's missingkey=zero.
			// Must render "" with no error.
			name:    "simple-key path on missing key renders as empty",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			lineFmt: "{{.foo}}",
			want:    "",
		},
		// --- Duplicate-short-name cases ---
		// Two Arrow columns with different FQNs share a short name —
		// typically a stream-label column with a real value and a same-name
		// structured-metadata column that is NULL for this row (either
		// contributed by another stream in the same dataobj section or
		// NULL'd in place by an upstream ColumnCompat). Both the simple-key
		// fast path and the general template path must look up the label by
		// category priority (parsed > metadata > stream) and skip NULLs,
		// rather than collapsing every column to a short-name map keyed by
		// Arrow iteration order.
		{
			name:    "simple-key path, label.pod first metadata.pod NULL — renders stream value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", value: realPod},
				{fqn: "utf8.metadata.pod", null: true},
			},
			lineFmt: "{{.pod}}",
			want:    realPod,
		},
		{
			name:    "simple-key path, metadata.pod NULL first label.pod second — renders stream value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.metadata.pod", null: true},
				{fqn: "utf8.label.pod", value: realPod},
			},
			lineFmt: "{{.pod}}",
			want:    realPod,
		},
		{
			name:    "general template path, label.pod first metadata.pod NULL — renders stream value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", value: realPod},
				{fqn: "utf8.metadata.pod", null: true},
			},
			lineFmt: "pod={{.pod}}",
			want:    "pod=" + realPod,
		},
		{
			name:    "general template path, metadata.pod NULL first label.pod second — renders stream value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.metadata.pod", null: true},
				{fqn: "utf8.label.pod", value: realPod},
			},
			lineFmt: "pod={{.pod}}",
			want:    "pod=" + realPod,
		},
		{
			name:    "both NULL — renders empty (matches v1 missingkey=zero)",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", null: true},
				{fqn: "utf8.metadata.pod", null: true},
			},
			lineFmt: "{{.pod}}",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := buildLabelfmtTestRecord(t, tt.line, tt.timeVal, tt.cols)
			parser, err := NewFormatter(tt.lineFmt)
			require.NoError(t, err)
			result := map[string]string{}
			output, processErr := parser.Process(tt.line, record, result)
			require.NoError(t, processErr)
			require.Equal(t, tt.want, output)
			require.Equal(t, tt.want, result["message"])
		})
	}
}

func TestBuildLinefmtColumns_TemplateExecutionErrorProducesErrorDetails(t *testing.T) {
	timestamp := time.Now().In(time.UTC).Add(time.Second)
	schema := arrow.NewSchema(
		[]arrow.Field{
			semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
			semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		},
		nil,
	)

	logBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	tsBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
	logBuilder.Append("line")
	tsBuilder.Append(arrow.Timestamp(timestamp.UnixNano()))

	logArray := logBuilder.NewStringArray()
	tsArray := tsBuilder.NewArray()
	recordBatch := array.NewRecordBatch(schema, []arrow.Array{logArray, tsArray}, 1)

	headers, columns := buildLinefmtColumns(recordBatch, logArray, "{{.foo | now}}")
	require.Equal(t, []string{
		semconv.ColumnIdentError.ShortName(),
		semconv.ColumnIdentErrorDetails.ShortName(),
	}, headers)

	errColumn := columns[0].(*array.String)
	errDetailsColumn := columns[1].(*array.String)
	require.Equal(t, types.LinefmtParserErrorType, errColumn.Value(0))
	require.Contains(t, errDetailsColumn.Value(0), "wrong number of args for now")
}
