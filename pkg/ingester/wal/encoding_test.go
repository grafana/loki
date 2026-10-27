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

	for _, tc := range []struct {
		desc       string
		version    RecordType
		ingestedAt time.Time
		// want is the timestamp expected on the decoded record and on each of its RefEntries.
		want time.Time
	}{
		{desc: "V4 with a timestamp restores it on all entries", version: WALRecordEntriesV4, ingestedAt: ingestedAt, want: ingestedAt},
		{desc: "V4 without a timestamp decodes to a zero time", version: WALRecordEntriesV4, ingestedAt: time.Time{}, want: time.Time{}},
		{desc: "the current version stores the timestamp", version: CurrentEntriesRec, ingestedAt: ingestedAt, want: ingestedAt},
		{desc: "V3 doesn't store the timestamp", version: WALRecordEntriesV3, ingestedAt: ingestedAt, want: time.Time{}},
		{desc: "V2 doesn't store the timestamp", version: WALRecordEntriesV2, ingestedAt: ingestedAt, want: time.Time{}},
		{desc: "V1 doesn't store the timestamp", version: WALRecordEntriesV1, ingestedAt: ingestedAt, want: time.Time{}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			rec := &Record{
				UserID:     "123",
				IngestedAt: tc.ingestedAt,
				RefEntries: []RefEntries{
					{Ref: 1, Counter: 1, Entries: []logproto.Entry{{Timestamp: time.Unix(1000, 0), Line: "first"}}},
					{Ref: 2, Counter: 2, Entries: []logproto.Entry{{Timestamp: time.Unix(2000, 0), Line: "second"}}},
				},
			}

			decoded := recordPool.GetRecord()
			require.NoError(t, DecodeRecord(rec.EncodeEntries(tc.version, nil), decoded))

			require.True(t, tc.want.Equal(decoded.IngestedAt), "record: got %s, want %s", decoded.IngestedAt, tc.want)
			require.Len(t, decoded.RefEntries, 2)
			for _, re := range decoded.RefEntries {
				require.True(t, tc.want.Equal(re.IngestedAt), "entries: got %s, want %s", re.IngestedAt, tc.want)
			}
			// The rest of the record is unaffected.
			require.Equal(t, "first", decoded.RefEntries[0].Entries[0].Line)
			require.Equal(t, "second", decoded.RefEntries[1].Entries[0].Line)
		})
	}
}

// A record without entries has no base timestamp, so it has no record timestamp either.
func TestEncodeEntriesWithoutEntries(t *testing.T) {
	rec := &Record{UserID: "123", IngestedAt: time.UnixMilli(1_700_000_000_123), RefEntries: []RefEntries{{Ref: 1}}}

	decoded := recordPool.GetRecord()
	require.NoError(t, DecodeRecord(rec.EncodeEntries(WALRecordEntriesV4, nil), decoded))
	require.Empty(t, decoded.RefEntries)
}

func TestRecordResetClearsIngestedAt(t *testing.T) {
	rec := &Record{IngestedAt: time.UnixMilli(1_700_000_000_123)}
	rec.Reset()
	require.True(t, rec.IngestedAt.IsZero())
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
