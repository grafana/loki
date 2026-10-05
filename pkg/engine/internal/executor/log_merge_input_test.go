package executor

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

func TestMergeInputTracker(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := func(streamID int64, ts time.Time, line string, metadata ...string) logs.Record {
		return logs.Record{StreamID: streamID, Timestamp: ts, Line: []byte(line), Metadata: labels.FromStrings(metadata...)}
	}

	tests := []struct {
		name           string
		records        []logs.Record
		wantBytes      int64
		wantDuplicates int
	}{
		{
			name:    "counts no duplicates when there are no records",
			records: nil,
		},
		{
			name: "counts line bytes and metadata value bytes but not metadata names",
			records: []logs.Record{
				record(1, ts, "hello", "trace_id", "abc", "user", "z"),
			},
			wantBytes: 5 + 3 + 1,
		},
		{
			name: "counts an adjacent identical record as a duplicate",
			records: []logs.Record{
				record(1, ts, "a", "k", "v"),
				record(1, ts, "a", "k", "v"),
			},
			wantBytes:      4,
			wantDuplicates: 1,
		},
		{
			name: "counts a duplicate separated by a different line in the same group",
			records: []logs.Record{
				record(1, ts, "a"),
				record(1, ts, "b"),
				record(1, ts, "a"),
			},
			wantBytes:      3,
			wantDuplicates: 1,
		},
		{
			name: "counts each extra copy of a record as one duplicate",
			records: []logs.Record{
				record(1, ts, "a"),
				record(1, ts, "a"),
				record(1, ts, "a"),
			},
			wantBytes:      3,
			wantDuplicates: 2,
		},
		{
			name: "does not count records with different metadata as duplicates",
			records: []logs.Record{
				record(1, ts, "a", "k", "1"),
				record(1, ts, "a", "k", "2"),
				record(1, ts, "a"),
			},
			wantBytes: 5,
		},
		{
			name: "does not count records with different timestamps as duplicates",
			records: []logs.Record{
				record(1, ts.Add(time.Nanosecond), "a"),
				record(1, ts, "a"),
			},
			wantBytes: 2,
		},
		{
			name: "does not count records in different streams as duplicates",
			records: []logs.Record{
				record(1, ts, "a"),
				record(2, ts, "a"),
			},
			wantBytes: 2,
		},
		{
			name: "does not count a record as a duplicate after its group ends",
			records: []logs.Record{
				record(1, ts, "a"),
				record(2, ts, "a"),
				record(1, ts, "a"),
			},
			wantBytes: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tracker mergeInputTracker
			for _, rec := range tt.records {
				tracker.observe(rec)
			}
			require.Equal(t, tt.wantBytes, tracker.bytes)
			require.Equal(t, tt.wantDuplicates, tracker.duplicates)
		})
	}
}
