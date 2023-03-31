package wal

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

var (
	recordPool = NewRecordPool()
)

func Test_Encoding_Series(t *testing.T) {
	record := &Record{
		entryIndexMap: make(map[uint64]int),
		UserID:        "123",
		Series: []record.RefSeries{
			{
				Ref: 456,
				Labels: labels.FromMap(map[string]string{
					"foo":  "bar",
					"bazz": "buzz",
				}),
			},
			{
				Ref: 789,
				Labels: labels.FromMap(map[string]string{
					"abc": "123",
					"def": "456",
				}),
			},
		},
	}

	buf := record.EncodeSeries(nil)

	decoded := recordPool.GetRecord()

	err := DecodeRecord(buf, decoded)
	require.Nil(t, err)

	// Since we use a pool, there can be subtle differentiations between nil slices and len(0) slices.
	// Both are valid, so check length.
	require.Equal(t, 0, len(decoded.RefEntries))
	decoded.RefEntries = nil
	require.Equal(t, record, decoded)
}

func Test_Encoding_Entries(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		rec     *Record
		version RecordType
	}{
		{
			desc: "v1",
			rec: &Record{
				entryIndexMap: make(map[uint64]int),
				UserID:        "123",
				RefEntries: []RefEntries{
					{
						Ref: 456,
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(1000, 0),
								Line:      "first",
							},
							{
								Timestamp: time.Unix(2000, 0),
								Line:      "second",
							},
						},
					},
					{
						Ref: 789,
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(3000, 0),
								Line:      "third",
							},
							{
								Timestamp: time.Unix(4000, 0),
								Line:      "fourth",
							},
						},
					},
				},
			},
			version: WALRecordEntriesV1,
		},
		{
			desc: "v2",
			rec: &Record{
				entryIndexMap: make(map[uint64]int),
				UserID:        "123",
				RefEntries: []RefEntries{
					{
						Ref:     456,
						Counter: 1, // v2 uses counter for WAL replay
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(1000, 0),
								Line:      "first",
							},
							{
								Timestamp: time.Unix(2000, 0),
								Line:      "second",
							},
						},
					},
					{
						Ref:     789,
						Counter: 2, // v2 uses counter for WAL replay
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(3000, 0),
								Line:      "third",
							},
							{
								Timestamp: time.Unix(4000, 0),
								Line:      "fourth",
							},
						},
					},
				},
			},
			version: WALRecordEntriesV2,
		},
	} {
		decoded := recordPool.GetRecord()
		buf := tc.rec.EncodeEntries(tc.version, nil)
		err := DecodeRecord(buf, decoded)
		require.Nil(t, err)
		require.Equal(t, tc.rec, decoded)

	}
}

func Benchmark_EncodeEntries(b *testing.B) {
	var entries []logproto.Entry
	for i := int64(0); i < 10000; i++ {
		entries = append(entries, logproto.Entry{
			Timestamp: time.Unix(0, i),
			Line:      fmt.Sprintf("long line with a lot of data like a log %d", i),
		})
	}
	record := &Record{
		entryIndexMap: make(map[uint64]int),
		UserID:        "123",
		RefEntries: []RefEntries{
			{
				Ref:     456,
				Entries: entries,
			},
			{
				Ref:     789,
				Entries: entries,
			},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	buf := recordPool.GetBytes()
	defer recordPool.PutBytes(buf)

	for n := 0; n < b.N; n++ {
		*buf = record.EncodeEntries(CurrentEntriesRec, *buf)
	}
}

func Benchmark_DecodeWAL(b *testing.B) {
	var entries []logproto.Entry
	for i := int64(0); i < 10000; i++ {
		entries = append(entries, logproto.Entry{
			Timestamp: time.Unix(0, i),
			Line:      fmt.Sprintf("long line with a lot of data like a log %d", i),
		})
	}
	record := &Record{
		entryIndexMap: make(map[uint64]int),
		UserID:        "123",
		RefEntries: []RefEntries{
			{
				Ref:     456,
				Entries: entries,
			},
			{
				Ref:     789,
				Entries: entries,
			},
		},
	}

	buf := record.EncodeEntries(CurrentEntriesRec, nil)
	rec := recordPool.GetRecord()
	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		require.NoError(b, DecodeRecord(buf, rec))
	}
}
