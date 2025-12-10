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
			semconv.FieldFromIdent(semconv.NewIdentifier("namespace", types.ColumnTypeMetadata, types.Loki.String), false),
		},
		nil, // No metadata
	)
	tests := []struct {
		name      string
		line      string
		timeVal   arrow.Timestamp
		namespace string
		fmts      []log.LabelFmt
		want      map[string]string
	}{
		{
			"simple rename",
			"this is my test line of logs",
			arrow.Timestamp(timestamp.UnixNano()),
			"dev",
			[]log.LabelFmt{{Name: "foo", Value: "namespace", Rename: true}},
			map[string]string{"foo": "dev"},
		},
		{
			"new label",
			"this is my test line of logs",
			arrow.Timestamp(timestamp.UnixNano()),
			"dev",
			[]log.LabelFmt{{Name: "logTimeNanos", Value: "{{ __timestamp__ | unixEpochNanos }}", Rename: false}},
			map[string]string{"logTimeNanos": strconv.FormatInt(timestamp.UTC().UnixNano(), 10)},
		},
		{
			"rename and label",
			"this is my test line of logs",
			arrow.Timestamp(timestamp.Add(10 * time.Second).UTC().UnixNano()),
			"dev",
			[]log.LabelFmt{{Name: "foo", Value: "namespace", Rename: true}, {Name: "logTimeNanos", Value: "{{ __timestamp__ | unixEpochNanos }}", Rename: false}},
			map[string]string{"foo": "dev", "logTimeNanos": strconv.FormatInt(timestamp.Add(10*time.Second).UTC().UnixNano(), 10)},
		},
	}

	for _, tt := range tests {
		synctest.Test(t, func(t *testing.T) {
			// Create builders for each column
			logBuilder := array.NewStringBuilder(memory.DefaultAllocator)
			tsBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
			namespaceBuilder := array.NewStringBuilder(memory.DefaultAllocator)

			// Append data to the builders
			logs := make([]string, 1)
			ts := make([]arrow.Timestamp, 1)
			namespaces := make([]string, 1)

			logs[0] = tt.line
			ts[0] = tt.timeVal
			namespaces[0] = tt.namespace

			tsBuilder.AppendValues(ts, nil)
			logBuilder.AppendValues(logs, nil)
			namespaceBuilder.AppendValues(namespaces, nil)

			// Build the arrays
			logArray := logBuilder.NewArray()
			tsArray := tsBuilder.NewArray()
			namespaceArray := namespaceBuilder.NewArray()
			columns := []arrow.Array{logArray, tsArray, namespaceArray}
			recordBatch := array.NewRecordBatch(schema, columns, 1)
			result, err := tokenizeLabelfmt(recordBatch, tt.line, tt.fmts)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
