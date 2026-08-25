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
	"github.com/grafana/loki/v3/pkg/logql/log"
)

// labelfmtTestColumn describes one column appended to the Arrow schema
// after the implicit message + timestamp builtins. Used by
// TestTokenizeLabelFmt to drive both the simple single-column cases and
// the duplicate-short-name cases from one table.
type labelfmtTestColumn struct {
	fqn   string
	value string
	null  bool
}

func TestTokenizeLabelFmt(t *testing.T) {
	timestamp := time.Now().In(time.UTC)

	const (
		realPod = "querier-1234"
		other   = "other-stream-value"
	)

	tests := []struct {
		name    string
		line    string
		timeVal arrow.Timestamp
		// cols is the list of label-like / metadata / parsed columns to
		// append after the builtin message + timestamp columns. Order is
		// preserved in the Arrow schema so duplicate-short-name cases can
		// validate that the labelfmt result is order-independent.
		cols []labelfmtTestColumn
		fmts []log.LabelFmt
		want map[string]string
	}{
		{
			name:    "simple rename",
			line:    "this is my test line of logs",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			fmts:    []log.LabelFmt{{Name: "foo", Value: "namespace", Rename: true}},
			want:    map[string]string{"foo": "dev"},
		},
		{
			name:    "new label",
			line:    "this is my test line of logs",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			fmts:    []log.LabelFmt{{Name: "logTimeNanos", Value: "{{ __timestamp__ | unixEpochNanos }}", Rename: false}},
			want:    map[string]string{"logTimeNanos": strconv.FormatInt(timestamp.UTC().UnixNano(), 10)},
		},
		{
			name:    "rename and label",
			line:    "this is my test line of logs",
			timeVal: arrow.Timestamp(timestamp.Add(10 * time.Second).UTC().UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", value: "dev"}},
			fmts:    []log.LabelFmt{{Name: "foo", Value: "namespace", Rename: true}, {Name: "logTimeNanos", Value: "{{ __timestamp__ | unixEpochNanos }}", Rename: false}},
			want:    map[string]string{"foo": "dev", "logTimeNanos": strconv.FormatInt(timestamp.Add(10*time.Second).UTC().UnixNano(), 10)},
		},
		{
			// Null entries on a parsed/metadata column must be exposed to the label
			// formatter as "" to match v1 engine.
			name:    "null entry renders as empty",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols:    []labelfmtTestColumn{{fqn: "utf8.metadata.namespace", null: true}},
			fmts:    []log.LabelFmt{{Name: "tag", Value: "x{{.namespace}}y", Rename: false}},
			want:    map[string]string{"tag": "xy"},
		},
		// --- Duplicate-short-name cases ---
		// Two Arrow columns with different FQNs share a short name —
		// typically a stream-label column with a real value and a same-name
		// structured-metadata column that is NULL for this row (either
		// contributed by another stream in the same dataobj section or
		// NULL'd in place by an upstream ColumnCompat).
		{
			name:    "label.pod first, metadata.pod NULL — rename uses stream value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", value: realPod},
				{fqn: "utf8.metadata.pod", null: true},
			},
			fmts: []log.LabelFmt{{Name: "foo", Value: "pod", Rename: true}},
			want: map[string]string{"foo": realPod},
		},
		{
			name:    "metadata.pod NULL first, label.pod second — rename uses stream value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.metadata.pod", null: true},
				{fqn: "utf8.label.pod", value: realPod},
			},
			fmts: []log.LabelFmt{{Name: "foo", Value: "pod", Rename: true}},
			want: map[string]string{"foo": realPod},
		},
		{
			name:    "label.pod and metadata.pod both set — stream value wins via base+add collision",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", value: realPod},
				{fqn: "utf8.metadata.pod", value: other},
			},
			fmts: []log.LabelFmt{{Name: "foo", Value: "pod", Rename: true}},
			want: map[string]string{"foo": realPod},
		},
		{
			// pod doesn't exist in either category for this row — rename
			// is a no-op, matching v1 where lbs.GetWithCategory returns
			// ok=false and LabelsFormatter.Process silently skips the
			// rename.
			name:    "both NULL — rename is a no-op",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", null: true},
				{fqn: "utf8.metadata.pod", null: true},
			},
			fmts: []log.LabelFmt{{Name: "foo", Value: "pod", Rename: true}},
			want: map[string]string{},
		},
		{
			name:    "label.pod set, metadata.pod NULL, metadata.pod_extracted set — rename foo=pod_extracted returns real metadata value",
			line:    "line",
			timeVal: arrow.Timestamp(timestamp.UnixNano()),
			cols: []labelfmtTestColumn{
				{fqn: "utf8.label.pod", value: realPod},
				{fqn: "utf8.metadata.pod", null: true},
				{fqn: "utf8.metadata.pod_extracted", value: "real-extracted-value"},
			},
			fmts: []log.LabelFmt{{Name: "foo", Value: "pod_extracted", Rename: true}},
			want: map[string]string{"foo": "real-extracted-value"},
		},
	}

	for _, tt := range tests {
		synctest.Test(t, func(t *testing.T) {
			record := buildLabelfmtTestRecord(t, tt.line, tt.timeVal, tt.cols)
			decoder, err := log.NewLabelsFormatter(tt.fmts)
			require.NoError(t, err)
			result, err := tokenizeLabelfmt(record, tt.line, decoder, tt.fmts)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

// buildLabelfmtTestRecord assembles a 1-row Arrow RecordBatch with the
// builtin message + timestamp columns followed by the caller's
// label/metadata/parsed columns in the order given.
func buildLabelfmtTestRecord(t *testing.T, line string, timeVal arrow.Timestamp, cols []labelfmtTestColumn) arrow.RecordBatch {
	t.Helper()
	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
	}
	arrays := []arrow.Array{
		stringArrayOf(line),
		timestampArrayOfTs(timeVal),
	}
	for _, c := range cols {
		ident, err := semconv.ParseFQN(c.fqn)
		require.NoError(t, err, "test column FQN %q", c.fqn)
		fields = append(fields, semconv.FieldFromIdent(ident, true))
		if c.null {
			arrays = append(arrays, nullStringArray())
		} else {
			arrays = append(arrays, stringArrayOf(c.value))
		}
	}
	schema := arrow.NewSchema(fields, nil)
	return array.NewRecordBatch(schema, arrays, 1)
}

func stringArrayOf(v string) *array.String {
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.Append(v)
	return b.NewStringArray()
}

func nullStringArray() *array.String {
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.AppendNull()
	return b.NewStringArray()
}

func timestampArrayOfTs(ts arrow.Timestamp) *array.Timestamp {
	b := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
	b.Append(ts)
	return b.NewTimestampArray()
}
