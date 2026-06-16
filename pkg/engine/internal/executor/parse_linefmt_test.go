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
	schema := arrow.NewSchema(
		[]arrow.Field{
			semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
			semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
			semconv.FieldFromIdent(semconv.NewIdentifier("namespace", types.ColumnTypeMetadata, types.Loki.String), true),
		},
		nil, // No metadata
	)
	tests := []struct {
		name          string
		line          string
		timeVal       arrow.Timestamp
		namespace     string
		nullNamespace bool // when true, append NULL for namespace instead of the namespace string
		lineFmt       string
		want          string
	}{
		{
			name:      "simple replacement",
			line:      "this is my test line of logs",
			timeVal:   arrow.Timestamp(timestamp.UnixNano()),
			namespace: "dev",
			lineFmt:   "{{.namespace}}",
			want:      "dev",
		},
		{
			name:      "replacement plus addition",
			line:      "this is my test line of logs",
			timeVal:   arrow.Timestamp(timestamp.UnixNano()),
			namespace: "dev",
			lineFmt:   "{{.namespace}} foo",
			want:      "dev foo",
		},
		{
			name:      "timestamp",
			line:      "this is my test line of logs",
			timeVal:   arrow.Timestamp(timestamp.UnixNano()),
			namespace: "dev",
			lineFmt:   "{{.timestamp}} foo",
			want:      timestamp.In(time.UTC).Format("2006-01-02T15:04:05.999999999Z") + " foo",
		},
		{
			// Null entries on a parsed/metadata column must render as "" to match v1 engine
			name:          "simple-key path on null entry renders as empty",
			line:          "line",
			timeVal:       arrow.Timestamp(timestamp.UnixNano()),
			nullNamespace: true,
			lineFmt:       "{{.namespace}}",
			want:          "",
		},
		{
			// Same intent as above but exercises the general template path
			// (mixed text + field reference).
			name:          "general template path on null entry renders as empty",
			line:          "line",
			timeVal:       arrow.Timestamp(timestamp.UnixNano()),
			nullNamespace: true,
			lineFmt:       "{{.namespace}} foo",
			want:          " foo",
		},
		{
			// `{{.foo}}` references a key that is not in the schema (no column
			// named "foo" was ever created). Simple-key path previously raised a
			// "missing key" parser error which surfaced as an __error__ label on
			// the row; v1 silently renders "" via Go template's missingkey=zero.
			// Must render "" with no error.
			name:      "simple-key path on missing key renders as empty",
			line:      "line",
			timeVal:   arrow.Timestamp(timestamp.UnixNano()),
			namespace: "dev",
			lineFmt:   "{{.foo}}",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create builders for each column
			logBuilder := array.NewStringBuilder(memory.DefaultAllocator)
			tsBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
			namespaceBuilder := array.NewStringBuilder(memory.DefaultAllocator)

			logBuilder.Append(tt.line)
			tsBuilder.Append(tt.timeVal)
			if tt.nullNamespace {
				namespaceBuilder.AppendNull()
			} else {
				namespaceBuilder.Append(tt.namespace)
			}

			// Build the arrays
			logArray := logBuilder.NewArray()
			tsArray := tsBuilder.NewArray()
			namespaceArray := namespaceBuilder.NewArray()
			columns := []arrow.Array{logArray, tsArray, namespaceArray}
			recordBatch := array.NewRecordBatch(schema, columns, 1)

			parser, err := NewFormatter(tt.lineFmt)
			require.NoError(t, err)
			var result = map[string]string{}
			output, processErr := parser.Process(tt.line, recordBatch, result)
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
