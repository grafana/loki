package executor

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

func testRecord(streamID int64, ts time.Time, line string, metadata ...string) logs.Record {
	return logs.Record{StreamID: streamID, Timestamp: ts, Line: []byte(line), Metadata: labels.FromStrings(metadata...)}
}

func TestDuplicateCounter(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		records        []logs.Record
		wantDuplicates int
	}{
		{
			name:    "counts no duplicates when there are no records",
			records: nil,
		},
		{
			name: "counts an adjacent identical record as a duplicate",
			records: []logs.Record{
				testRecord(1, ts, "a", "k", "v"),
				testRecord(1, ts, "a", "k", "v"),
			},
			wantDuplicates: 1,
		},
		{
			name: "counts a duplicate separated by a different line in the same group",
			records: []logs.Record{
				testRecord(1, ts, "a"),
				testRecord(1, ts, "b"),
				testRecord(1, ts, "a"),
			},
			wantDuplicates: 1,
		},
		{
			name: "counts each extra copy of a record as one duplicate",
			records: []logs.Record{
				testRecord(1, ts, "a"),
				testRecord(1, ts, "a"),
				testRecord(1, ts, "a"),
			},
			wantDuplicates: 2,
		},
		{
			name: "does not count records with different metadata as duplicates",
			records: []logs.Record{
				testRecord(1, ts, "a", "k", "1"),
				testRecord(1, ts, "a", "k", "2"),
				testRecord(1, ts, "a"),
			},
		},
		{
			name: "does not count records with different timestamps as duplicates",
			records: []logs.Record{
				testRecord(1, ts.Add(time.Nanosecond), "a"),
				testRecord(1, ts, "a"),
			},
		},
		{
			name: "does not count records in different streams as duplicates",
			records: []logs.Record{
				testRecord(1, ts, "a"),
				testRecord(2, ts, "a"),
			},
		},
		{
			name: "does not count a record as a duplicate after its group ends",
			records: []logs.Record{
				testRecord(1, ts, "a"),
				testRecord(2, ts, "a"),
				testRecord(1, ts, "a"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var counter duplicateCounter
			for _, rec := range tt.records {
				counter.observe(rec)
			}
			require.Equal(t, tt.wantDuplicates, counter.duplicates)
		})
	}
}
