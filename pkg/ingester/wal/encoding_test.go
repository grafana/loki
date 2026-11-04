package wal

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/encoding"
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

func TestEncodingSharedMetadata(t *testing.T) {
	rec := recordPool.GetRecord()
	defer recordPool.PutRecord(rec)
	rec.UserID = "tenant"
	at := time.Unix(1000, 123)
	attrs := []logproto.LabelAdapter{{Name: "resource", Value: "shared"}, {Name: "override", Value: "scope"}}
	entries := []logproto.Entry{
		{Timestamp: at, Line: "first"},
		{Timestamp: at.Add(-time.Second), Line: "out of order", StructuredMetadata: []logproto.LabelAdapter{{Name: "override", Value: ""}}},
	}
	groups := []MetadataGroup{{Start: 0, End: 2, Attrs: attrs}}
	// Repeated fingerprints, native gaps, and multiple streams share one WAL record.
	rec.AddEntries(1, 1, logproto.Entry{Timestamp: at, Line: "native"})
	rec.AddEntriesWithMetadata(1, 3, entries, groups)
	rec.AddEntries(1, 4, logproto.Entry{Timestamp: at, Line: "gap"})
	rec.AddEntriesWithMetadata(1, 6, entries, groups)
	rec.AddEntriesWithMetadata(2, 2, entries, groups)
	require.Equal(t, []MetadataGroup{{Start: 0, End: 2, Attrs: attrs}}, groups, "appending must not shift the caller's ranges")
	require.Equal(t, 6, len(rec.RefEntries[0].Entries))
	require.Equal(t, int64(6), rec.RefEntries[0].Counter)
	require.Equal(t, []MetadataGroup{{Start: 1, End: 3, Attrs: attrs}, {Start: 4, End: 6, Attrs: attrs}}, rec.RefEntries[0].MetadataGroups)

	for _, version := range []RecordType{WALRecordEntriesV3, WALRecordEntriesV4} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			encoded := rec.EncodeEntries(version, nil)
			var decoded Record
			require.NoError(t, DecodeRecord(encoded, &decoded))
			require.Equal(t, rec.UserID, decoded.UserID)
			if version == WALRecordEntriesV4 {
				require.Equal(t, rec.RefEntries, decoded.RefEntries)
				// Lines and shared attributes must outlive the reader's reusable buffer.
				clear(encoded)
				require.Equal(t, rec.RefEntries, decoded.RefEntries)
			} else {
				for i, ref := range rec.RefEntries {
					want := append([]logproto.Entry(nil), ref.Entries...)
					for _, group := range ref.MetadataGroups {
						for j := group.Start; j < group.End; j++ {
							want[j].StructuredMetadata = logproto.AppendMetadata(nil, want[j].StructuredMetadata, group.Attrs)
						}
					}
					require.Equal(t, want, decoded.RefEntries[i].Entries)
					require.Empty(t, decoded.RefEntries[i].MetadataGroups)
				}
			}
		})
	}
}

func TestSharedMetadataWALRejectsMalformedRecords(t *testing.T) {
	entry := logproto.Entry{Timestamp: time.Unix(1000, 0), Line: "line"}
	for _, groups := range [][]MetadataGroup{
		{{Start: 0, End: 0}},
		{{Start: 1, End: 0}},
		{{Start: -1, End: 1}},
		{{Start: 0, End: 2}},
		{{Start: 0, End: 1}, {Start: 0, End: 1}},
	} {
		rec := Record{RefEntries: []RefEntries{{Entries: []logproto.Entry{entry}, MetadataGroups: groups}}}
		require.Error(t, DecodeRecord(rec.EncodeEntries(WALRecordEntriesV4, nil), &Record{}))
	}

	rec := Record{UserID: "tenant", RefEntries: []RefEntries{{
		Ref: 1, Counter: 1, Entries: []logproto.Entry{entry},
		MetadataGroups: []MetadataGroup{{Start: 0, End: 1, Attrs: []logproto.LabelAdapter{{Name: "a", Value: "b"}}}},
	}}}
	encoded := rec.EncodeEntries(WALRecordEntriesV4, nil)
	// A header alone or a header plus base timestamp also encodes an empty record.
	// Every truncation within the stream must instead fail.
	streamStart := 1 + 1 + len(rec.UserID) + 8
	for n := streamStart + 1; n < len(encoded); n++ {
		require.Error(t, DecodeRecord(encoded[:n], &Record{}), "truncation at %d", n)
	}

	for _, bad := range []uint64{1 << 62, ^uint64(0)} {
		buf := encoding.EncWith(nil)
		buf.PutByte(byte(WALRecordEntriesV4))
		buf.PutUvarintStr("tenant")
		buf.PutBE64(0)
		buf.PutBE64(1)
		buf.PutBE64(1)
		buf.B = binary.AppendUvarint(buf.B, bad)
		require.Error(t, DecodeRecord(buf.Get(), &Record{}), "oversized entry count")
	}
}

func TestRecordPoolReleasesSharedMetadata(t *testing.T) {
	pool := NewRecordPool()
	rec := pool.GetRecord()
	rec.AddEntriesWithMetadata(1, 1, []logproto.Entry{{Line: "line"}}, []MetadataGroup{{Start: 0, End: 1, Attrs: []logproto.LabelAdapter{{Name: "a", Value: "b"}}}})
	backing := rec.RefEntries
	pool.PutRecord(rec)
	require.Equal(t, RefEntries{}, backing[0], "pooled records must release entry and attribute arrays immediately")
}

func TestSharedMetadataWALSize(t *testing.T) {
	rec := sharedMetadataWALRecord(100, 8)
	flat := rec.EncodeEntries(WALRecordEntriesV3, nil)
	nested := rec.EncodeEntries(WALRecordEntriesV4, nil)
	require.Less(t, len(nested), len(flat)/2)
	require.Equal(t, 1, bytes.Count(nested, []byte("attribute_0")))
	require.Equal(t, 100, bytes.Count(flat, []byte("attribute_0")))

	native := sharedMetadataWALRecord(100, 0)
	require.Equal(t, len(native.EncodeEntries(WALRecordEntriesV3, nil))+1, len(native.EncodeEntries(WALRecordEntriesV4, nil)), "native records add only the zero group count")
}

func sharedMetadataWALRecord(count, sharedAttrs int) *Record {
	entries := make([]logproto.Entry, count)
	for i := range entries {
		entries[i] = logproto.Entry{Timestamp: time.Unix(1000, int64(i)), Line: strings.Repeat("x", 128)}
	}
	ref := RefEntries{Ref: 1, Counter: int64(count), Entries: entries}
	if sharedAttrs > 0 {
		attrs := make([]logproto.LabelAdapter, sharedAttrs)
		for i := range attrs {
			attrs[i] = logproto.LabelAdapter{Name: fmt.Sprintf("attribute_%d", i), Value: strings.Repeat("v", 64)}
		}
		ref.MetadataGroups = []MetadataGroup{{Start: 0, End: count, Attrs: attrs}}
	}
	return &Record{UserID: "tenant", RefEntries: []RefEntries{ref}}
}

func BenchmarkSharedMetadataWAL(b *testing.B) {
	for _, attrs := range []int{0, 8, 32} {
		for _, version := range []RecordType{WALRecordEntriesV3, WALRecordEntriesV4} {
			rec := sharedMetadataWALRecord(1000, attrs)
			encoded := rec.EncodeEntries(version, nil)
			b.Run(fmt.Sprintf("attrs=%d/version=%d/encode", attrs, version), func(b *testing.B) {
				// V3 ingestion already expanded before encoding. Keep that setup out of
				// the encode measurement; the V4 encoder must not expand either.
				var decoded Record
				require.NoError(b, DecodeRecord(encoded, &decoded))
				buf := make([]byte, 0, len(encoded))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					buf = decoded.EncodeEntries(version, buf[:0])
				}
				b.ReportMetric(float64(len(buf)), "WAL-B/record")
			})
			b.Run(fmt.Sprintf("attrs=%d/version=%d/decode", attrs, version), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var decoded Record
					if err := DecodeRecord(encoded, &decoded); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func FuzzDecodeWALEntries(f *testing.F) {
	for _, version := range []RecordType{WALRecordEntriesV1, WALRecordEntriesV2, WALRecordEntriesV3, WALRecordEntriesV4} {
		f.Add(sharedMetadataWALRecord(2, 2).EncodeEntries(version, nil))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 1<<20 {
			return
		}
		switch RecordType(data[0]) {
		case WALRecordEntriesV1, WALRecordEntriesV2, WALRecordEntriesV3, WALRecordEntriesV4:
		default:
			return
		}
		var rec Record
		if err := DecodeRecord(data, &rec); err != nil {
			return
		}
		for _, ref := range rec.RefEntries {
			previousEnd := 0
			for _, group := range ref.MetadataGroups {
				require.GreaterOrEqual(t, group.Start, previousEnd)
				require.Greater(t, group.End, group.Start)
				require.LessOrEqual(t, group.End, len(ref.Entries))
				previousEnd = group.End
			}
		}
	})
}
