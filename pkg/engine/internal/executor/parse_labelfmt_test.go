package executor

import (
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

func TestTokenizeLabelFmt(t *testing.T) {
	timestamp := time.Now().In(time.UTC)
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
		fmts          []log.LabelFmt
		want          map[string]string
	}{
		{
			name:      "simple rename",
			line:      "this is my test line of logs",
			timeVal:   arrow.Timestamp(timestamp.UnixNano()),
			namespace: "dev",
			fmts:      []log.LabelFmt{{Name: "foo", Value: "namespace", Rename: true}},
			want:      map[string]string{"foo": "dev"},
		},
		{
			name:      "new label",
			line:      "this is my test line of logs",
			timeVal:   arrow.Timestamp(timestamp.UnixNano()),
			namespace: "dev",
			fmts:      []log.LabelFmt{{Name: "logTimeNanos", Value: "{{ __timestamp__ | unixEpochNanos }}", Rename: false}},
			want:      map[string]string{"logTimeNanos": strconv.FormatInt(timestamp.UTC().UnixNano(), 10)},
		},
		{
			name:      "rename and label",
			line:      "this is my test line of logs",
			timeVal:   arrow.Timestamp(timestamp.Add(10 * time.Second).UTC().UnixNano()),
			namespace: "dev",
			fmts:      []log.LabelFmt{{Name: "foo", Value: "namespace", Rename: true}, {Name: "logTimeNanos", Value: "{{ __timestamp__ | unixEpochNanos }}", Rename: false}},
			want:      map[string]string{"foo": "dev", "logTimeNanos": strconv.FormatInt(timestamp.Add(10*time.Second).UTC().UnixNano(), 10)},
		},
		{
			// Null entries on a parsed/metadata column must be exposed to the label
			// formatter as "" to match v1 engine
			name:          "null entry renders as empty",
			line:          "line",
			timeVal:       arrow.Timestamp(timestamp.UnixNano()),
			nullNamespace: true,
			fmts:          []log.LabelFmt{{Name: "tag", Value: "x{{.namespace}}y", Rename: false}},
			want:          map[string]string{"tag": "xy"},
		},
	}

	for _, tt := range tests {
		synctest.Test(t, func(t *testing.T) {
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
			decoder, err := log.NewLabelsFormatter(tt.fmts)
			require.NoError(t, err)
			result, err := tokenizeLabelfmt(recordBatch, tt.line, decoder, tt.fmts)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
