package wal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
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
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "123",
									"userID", "a",
								)),
							},
							{
								Timestamp: time.Unix(2000, 0),
								Line:      "second",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "456",
									"userID", "b",
								)),
							},
						},
					},
					{
						Ref: 789,
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(3000, 0),
								Line:      "third",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "789",
									"userID", "c",
								)),
							},
							{
								Timestamp: time.Unix(4000, 0),
								Line:      "fourth",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "123",
									"userID", "d",
								)),
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
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "123",
									"userID", "a",
								)),
							},
							{
								Timestamp: time.Unix(2000, 0),
								Line:      "second",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "456",
									"userID", "b",
								)),
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
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "789",
									"userID", "c",
								)),
							},
							{
								Timestamp: time.Unix(4000, 0),
								Line:      "fourth",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "123",
									"userID", "d",
								)),
							},
						},
					},
				},
			},
			version: WALRecordEntriesV2,
		},
		{
			desc: "v3",
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
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "123",
									"userID", "a",
								)),
							},
							{
								Timestamp: time.Unix(2000, 0),
								Line:      "second",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "456",
									"userID", "b",
								)),
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
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "789",
									"userID", "c",
								)),
							},
							{
								Timestamp: time.Unix(4000, 0),
								Line:      "fourth",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings(
									"traceID", "123",
									"userID", "d",
								)),
							},
						},
					},
				},
			},
			version: WALRecordEntriesV3,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			decoded := recordPool.GetRecord()
			buf := tc.rec.EncodeEntries(tc.version, nil)
			err := DecodeRecord(buf, decoded)
			require.Nil(t, err)

			// If the version is less than v3, we need to remove the structured metadata.
			expectedRecords := tc.rec
			if tc.version < WALRecordEntriesV3 {
				for i := range expectedRecords.RefEntries {
					for j := range expectedRecords.RefEntries[i].Entries {
						expectedRecords.RefEntries[i].Entries[j].StructuredMetadata = nil
					}
				}
			}

			require.Equal(t, expectedRecords, decoded)
		})
	}
}

func TestEntriesRecordIngestedAt(t *testing.T) {
	ingestedAt := time.UnixMilli(1_700_000_000_123)
	newRec := func(ts time.Time) *Record {
		return &Record{
			UserID:     "123",
			IngestedAt: ts,
			RefEntries: []RefEntries{
				{Ref: 1, Counter: 1, Entries: []logproto.Entry{{Timestamp: time.Unix(1000, 0), Line: "first"}}},
				{Ref: 2, Counter: 2, Entries: []logproto.Entry{{Timestamp: time.Unix(2000, 0), Line: "second"}}},
			},
		}
	}

	t.Run("a record with a timestamp is restored on all entries", func(t *testing.T) {
		rec := newRec(ingestedAt)
		decoded := recordPool.GetRecord()
		require.NoError(t, DecodeRecord(rec.EncodeEntries(CurrentEntriesRec, nil), decoded))

		require.True(t, ingestedAt.Equal(decoded.IngestedAt))
		require.Len(t, decoded.RefEntries, 2)
		for _, re := range decoded.RefEntries {
			require.True(t, ingestedAt.Equal(re.IngestedAt))
		}
		require.Equal(t, "second", decoded.RefEntries[1].Entries[0].Line)
	})

	t.Run("a record without a timestamp decodes to a zero time", func(t *testing.T) {
		rec := newRec(time.Time{})

		decoded := recordPool.GetRecord()
		require.NoError(t, DecodeRecord(rec.EncodeEntries(CurrentEntriesRec, nil), decoded))
		require.True(t, decoded.IngestedAt.IsZero())
		for _, re := range decoded.RefEntries {
			require.True(t, re.IngestedAt.IsZero())
		}
	})

	t.Run("V4 without a timestamp decodes to a zero time", func(t *testing.T) {
		rec := newRec(time.Time{})
		decoded := recordPool.GetRecord()
		require.NoError(t, DecodeRecord(rec.EncodeEntries(WALRecordEntriesV4, nil), decoded))
		require.True(t, decoded.IngestedAt.IsZero())
	})

	t.Run("older versions decode without a timestamp", func(t *testing.T) {
		rec := newRec(ingestedAt)
		for _, v := range []RecordType{WALRecordEntriesV1, WALRecordEntriesV2, WALRecordEntriesV3} {
			decoded := recordPool.GetRecord()
			require.NoError(t, DecodeRecord(rec.EncodeEntries(v, nil), decoded))
			require.True(t, decoded.IngestedAt.IsZero())
			require.Len(t, decoded.RefEntries, 2)
		}
	})

	t.Run("reset clears it", func(t *testing.T) {
		rec := newRec(ingestedAt)
		rec.Reset()
		require.True(t, rec.IngestedAt.IsZero())
	})
}

func Benchmark_EncodeEntries(b *testing.B) {
	for _, withStructuredMetadata := range []bool{true, false} {
		b.Run(fmt.Sprintf("structuredMetadata=%t", withStructuredMetadata), func(b *testing.B) {
			var entries []logproto.Entry
			for i := int64(0); i < 10000; i++ {
				entry := logproto.Entry{
					Timestamp: time.Unix(0, i),
					Line:      fmt.Sprintf("long line with a lot of data like a log %d", i),
				}

				if withStructuredMetadata {
					entry.StructuredMetadata = logproto.FromLabelsToLabelAdapters(labels.FromStrings(
						"traceID", strings.Repeat(fmt.Sprintf("%d", i), 10),
						"userID", strings.Repeat(fmt.Sprintf("%d", i), 10),
					))
				}

				entries = append(entries, entry)
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
		})
	}
}

func Benchmark_DecodeWAL(b *testing.B) {
	for _, withStructuredMetadata := range []bool{true, false} {
		b.Run(fmt.Sprintf("structuredMetadata=%t", withStructuredMetadata), func(b *testing.B) {
			var entries []logproto.Entry
			for i := int64(0); i < 10000; i++ {
				entry := logproto.Entry{
					Timestamp: time.Unix(0, i),
					Line:      fmt.Sprintf("long line with a lot of data like a log %d", i),
				}

				if withStructuredMetadata {
					entry.StructuredMetadata = logproto.FromLabelsToLabelAdapters(labels.FromStrings(
						"traceID", strings.Repeat(fmt.Sprintf("%d", i), 10),
						"userID", strings.Repeat(fmt.Sprintf("%d", i), 10),
					))
				}

				entries = append(entries, entry)
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
		})
	}
}
