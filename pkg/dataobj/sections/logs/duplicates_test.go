package logs

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func testRecord(streamID int64, ts time.Time, line string, metadata ...string) Record {
	return Record{StreamID: streamID, Timestamp: ts, Line: []byte(line), Metadata: labels.FromStrings(metadata...)}
}

func TestDuplicateTracker(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		records        []Record
		wantDuplicates int
	}{
		{
			name:    "counts no duplicates when there are no records",
			records: nil,
		},
		{
			name: "counts an adjacent identical record as a duplicate",
			records: []Record{
				testRecord(1, ts, "a", "k", "v"),
				testRecord(1, ts, "a", "k", "v"),
			},
			wantDuplicates: 1,
		},
		{
			name: "counts a duplicate separated by a different line in the same group",
			records: []Record{
				testRecord(1, ts, "a"),
				testRecord(1, ts, "b"),
				testRecord(1, ts, "a"),
			},
			wantDuplicates: 1,
		},
		{
			name: "counts each extra copy of a record as one duplicate",
			records: []Record{
				testRecord(1, ts, "a"),
				testRecord(1, ts, "a"),
				testRecord(1, ts, "a"),
			},
			wantDuplicates: 2,
		},
		{
			name: "does not count records with different metadata as duplicates",
			records: []Record{
				testRecord(1, ts, "a", "k", "1"),
				testRecord(1, ts, "a", "k", "2"),
				testRecord(1, ts, "a"),
			},
		},
		{
			name: "does not count records with different timestamps as duplicates",
			records: []Record{
				testRecord(1, ts.Add(time.Nanosecond), "a"),
				testRecord(1, ts, "a"),
			},
		},
		{
			name: "does not count records in different streams as duplicates",
			records: []Record{
				testRecord(1, ts, "a"),
				testRecord(2, ts, "a"),
			},
		},
		{
			name: "does not count a record as a duplicate after its group ends",
			records: []Record{
				testRecord(1, ts, "a"),
				testRecord(2, ts, "a"),
				testRecord(1, ts, "a"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var counter DuplicateTracker
			for _, rec := range tt.records {
				counter.IsDuplicate(rec)
			}
			require.Equal(t, tt.wantDuplicates, counter.Duplicates())
		})
	}
}

// BenchmarkBuildTable_DropDuplicates builds a table of 50,000 records with
// duplicate dropping on. One record in 100 repeats an earlier record of its
// stream and timestamp.
func BenchmarkBuildTable_DropDuplicates(b *testing.B) {
	const recordCount = 50_000
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewPCG(1, 2))

	records := make([]Record, 0, recordCount)
	for i := range recordCount {
		line := make([]byte, 480)
		for j := range line {
			line[j] = byte('a' + rng.IntN(26))
		}
		if i%100 == 99 {
			line = records[i-1].Line
		}
		records = append(records, Record{
			StreamID:  int64(i % 400),
			Timestamp: ts.Add(time.Duration(i/400) * time.Second),
			Line:      line,
			Metadata:  labels.FromStrings("pod", fmt.Sprintf("pod-%d", rng.IntN(1000)), "trace_id", fmt.Sprintf("%016x", rng.Uint64())),
		})
		if i%100 == 99 {
			records[i].StreamID = records[i-1].StreamID
			records[i].Timestamp = records[i-1].Timestamp
			records[i].Metadata = records[i-1].Metadata
		}
	}

	input := make([]Record, len(records))
	var buf tableBuffer
	b.ReportAllocs()
	for b.Loop() {
		copy(input, records)
		_ = buildTable(&buf, 1<<20, 10000, nil, input, SortStreamASC, true)
	}
}

func TestBuildTable_DropDuplicates(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := func() []Record {
		return []Record{
			testRecord(1, ts, "a", "trace_id", "1"),
			testRecord(1, ts, "b", "trace_id", "2", "user", "x"),
			testRecord(1, ts, "a", "trace_id", "1"),
			testRecord(1, ts, "a", "trace_id", "3"),
			testRecord(2, ts, "a", "trace_id", "1"),
		}
	}
	want := []string{
		"1 a trace_id=1",
		"1 a trace_id=3",
		"1 b trace_id=2 user=x",
		"2 a trace_id=1",
	}

	sortOrders := map[string]SortOrder{
		"stream":    SortStreamASC,
		"timestamp": SortTimestampDESC,
		"schema":    SortSchemaASC,
	}
	for name, sortOrder := range sortOrders {
		t.Run("drops a non-adjacent duplicate and keeps each row's metadata when sorted by "+name, func(t *testing.T) {
			table := buildTable(&tableBuffer{}, pageSize, pageRows, nil, records(), sortOrder, true)
			require.ElementsMatch(t, want, readTableRows(t, table))
		})
	}

	t.Run("keeps every record when dropping is off", func(t *testing.T) {
		table := buildTable(&tableBuffer{}, pageSize, pageRows, nil, records(), SortStreamASC, false)
		require.Len(t, readTableRows(t, table), 5)
	})
}

// readTableRows returns each row of table as "<stream ID> <line> <key=value>...",
// with metadata keys in sorted order and null metadata omitted.
func readTableRows(t *testing.T, table *table) []string {
	t.Helper()

	columns := []dataset.Column{table.StreamID, table.Message}
	for _, md := range table.Metadatas {
		columns = append(columns, md)
	}
	r := dataset.NewRowReader(dataset.RowReaderOptions{Dataset: table, ProjectedColumns: columns})
	t.Cleanup(func() { _ = r.Close() })
	require.NoError(t, r.Open(context.Background()))

	rowCount := table.StreamID.Desc.RowsCount
	rows := make([]dataset.Row, rowCount)
	n, err := r.Read(context.Background(), rows)
	require.NoError(t, err)
	require.Equal(t, rowCount, n)

	out := make([]string, 0, n)
	for _, row := range rows {
		fields := []string{fmt.Sprint(row.Values[0].Int64()), string(row.Values[1].Binary())}
		var metadata []string
		for i, md := range table.Metadatas {
			if v := row.Values[2+i]; !v.IsNil() {
				metadata = append(metadata, md.Desc.Tag+"="+string(v.Binary()))
			}
		}
		slices.Sort(metadata)
		out = append(out, strings.Join(append(fields, metadata...), " "))
	}
	return out
}
