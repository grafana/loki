package wal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"

	pushtypes "github.com/grafana/loki/pkg/push"
)

// attrs returns push.LabelsAdapter rather than a bare slice so that it can be compared against
// Entry.StructuredMetadata directly: require.Equal is strict about named types.
func attrs(pairs ...string) pushtypes.LabelsAdapter {
	out := make(pushtypes.LabelsAdapter, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, logproto.LabelAdapter{Name: pairs[i], Value: pairs[i+1]})
	}
	return out
}

func sets(attrSets ...pushtypes.LabelsAdapter) []logproto.SharedStructuredMetadataSet {
	out := make([]logproto.SharedStructuredMetadataSet, 0, len(attrSets))
	for _, a := range attrSets {
		out = append(out, logproto.SharedStructuredMetadataSet{Attrs: a})
	}
	return out
}

func roundTrip(t *testing.T, rec *Record, version RecordType) *Record {
	t.Helper()

	decoded := &Record{}
	require.NoError(t, DecodeRecord(rec.EncodeEntries(version, nil), decoded))

	return decoded
}

// TestEntriesV4RoundTrip checks a V4 record preserves the stream's pool and every entry's own
// structured metadata and references, which is what makes a replay reconstruct the push exactly.
func TestEntriesV4RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  RefEntries
	}{
		{
			name: "pool with resource and scope sets",
			ref: RefEntries{
				Ref:                          456,
				Counter:                      7,
				SharedStructuredMetadataSets: sets(attrs("service_name", "checkout"), attrs("scope_name", "otelhttp")),
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a", StructuredMetadata: attrs("traceID", "1"), SharedResourceRef: 1, SharedScopeRef: 2},
					{Timestamp: time.Unix(2, 0), Line: "b", SharedResourceRef: 1},
					{Timestamp: time.Unix(3, 0), Line: "c", StructuredMetadata: attrs("spanID", "9"), SharedScopeRef: 2},
				},
			},
		},
		{
			name: "entries referencing nothing while a pool exists",
			ref: RefEntries{
				Ref:                          1,
				SharedStructuredMetadataSets: sets(attrs("service_name", "checkout")),
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a"},
				},
			},
		},
		{
			name: "empty pool",
			ref: RefEntries{
				Ref: 2,
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a", StructuredMetadata: attrs("traceID", "1")},
				},
			},
		},
		{
			name: "set with no attributes at all",
			ref: RefEntries{
				Ref:                          3,
				SharedStructuredMetadataSets: sets(nil),
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a", SharedResourceRef: 1},
				},
			},
		},
		{
			name: "many sets, exercising multi byte varint references",
			ref: func() RefEntries {
				var (
					pool    []logproto.SharedStructuredMetadataSet
					entries []logproto.Entry
				)
				for i := 0; i < 200; i++ {
					pool = append(pool, logproto.SharedStructuredMetadataSet{Attrs: attrs("k", string(rune('a'+i%26)))})
				}
				for i := 0; i < 200; i++ {
					entries = append(entries, logproto.Entry{
						Timestamp:         time.Unix(int64(i+1), 0),
						Line:              "line",
						SharedResourceRef: uint32(i + 1),
						SharedScopeRef:    uint32(200 - i),
					})
				}
				return RefEntries{Ref: 4, SharedStructuredMetadataSets: pool, Entries: entries}
			}(),
		},
		{
			name: "out of range reference survives the round trip",
			ref: RefEntries{
				// Resolving a bad reference is the reader's business (push.Stream.SharedFor
				// treats it as no set); the encoding must not quietly rewrite it.
				Ref:                          5,
				SharedStructuredMetadataSets: sets(attrs("service_name", "checkout")),
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a", SharedResourceRef: 99, SharedScopeRef: 42},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &Record{UserID: "123", RefEntries: []RefEntries{tc.ref}}

			decoded := roundTrip(t, rec, WALRecordEntriesV4)
			require.Equal(t, "123", decoded.UserID)
			require.Equal(t, rec.RefEntries, decoded.RefEntries)
		})
	}
}

// TestEntriesVersionGate pins that the pool and the references only exist from V4 on: encoding a
// pooled record as V3 drops them, which is exactly why such a record must be written as V4.
func TestEntriesVersionGate(t *testing.T) {
	rec := &Record{
		UserID: "123",
		RefEntries: []RefEntries{{
			Ref:                          456,
			Counter:                      1,
			SharedStructuredMetadataSets: sets(attrs("service_name", "checkout")),
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(1, 0), Line: "a", StructuredMetadata: attrs("traceID", "1"), SharedResourceRef: 1, SharedScopeRef: 1},
			},
		}},
	}

	t.Run("V3 carries neither the pool nor the references", func(t *testing.T) {
		decoded := roundTrip(t, rec, WALRecordEntriesV3)

		require.Empty(t, decoded.RefEntries[0].SharedStructuredMetadataSets)
		require.Zero(t, decoded.RefEntries[0].Entries[0].SharedResourceRef)
		require.Zero(t, decoded.RefEntries[0].Entries[0].SharedScopeRef)
		// The entry's own structured metadata is still there, and is all a V3 replay will see.
		require.Equal(t, attrs("traceID", "1"), decoded.RefEntries[0].Entries[0].StructuredMetadata)
	})

	t.Run("V4 carries both", func(t *testing.T) {
		decoded := roundTrip(t, rec, WALRecordEntriesV4)
		require.Equal(t, rec.RefEntries, decoded.RefEntries)
	})

	t.Run("a V3 record stays byte identical", func(t *testing.T) {
		// Whatever V4 adds must sit behind the version check, so that a record written as V3
		// is the same bytes it has always been.
		poolLess := &Record{
			UserID: "123",
			RefEntries: []RefEntries{{
				Ref:     456,
				Counter: 1,
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a", StructuredMetadata: attrs("traceID", "1")},
				},
			}},
		}
		require.Equal(t, poolLess.EncodeEntries(WALRecordEntriesV3, nil), rec.EncodeEntries(WALRecordEntriesV3, nil))
	})
}

// TestEntriesVersionSelection pins the emission policy: a record only moves to V4 when it
// actually carries a pool, so segments for tenants that share nothing stay readable by an
// ingester that predates V4.
func TestEntriesVersionSelection(t *testing.T) {
	entries := []logproto.Entry{{Timestamp: time.Unix(1, 0), Line: "a"}}

	t.Run("no entries at all", func(t *testing.T) {
		require.Equal(t, CurrentEntriesRec, (&Record{}).EntriesVersion())
	})

	t.Run("pool-less", func(t *testing.T) {
		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 0, nil, entries...)
		require.Equal(t, CurrentEntriesRec, rec.EntriesVersion())
		require.Equal(t, WALRecordEntriesV3, rec.EntriesVersion())
	})

	t.Run("pooled", func(t *testing.T) {
		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 0, sets(attrs("service_name", "checkout")), entries...)
		require.Equal(t, WALRecordEntriesV4, rec.EntriesVersion())
	})

	t.Run("an empty pool is not a pool", func(t *testing.T) {
		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 0, []logproto.SharedStructuredMetadataSet{}, entries...)
		require.Equal(t, CurrentEntriesRec, rec.EntriesVersion())
	})

	t.Run("one pooled stream among pool-less ones", func(t *testing.T) {
		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 0, nil, entries...)
		rec.AddEntries(2, 0, sets(attrs("service_name", "checkout")), entries...)
		rec.AddEntries(3, 0, nil, entries...)
		require.Equal(t, WALRecordEntriesV4, rec.EntriesVersion())
	})
}

// TestAddEntriesPoolIsolation covers the merging rule in AddEntries: entries whose references
// index different pools must never end up sharing one RefEntries, or the references of one push
// would be resolved against the other's pool.
func TestAddEntriesPoolIsolation(t *testing.T) {
	entry := func(line string, resourceRef uint32) logproto.Entry {
		return logproto.Entry{Timestamp: time.Unix(1, 0), Line: line, SharedResourceRef: resourceRef}
	}

	t.Run("pool-less pushes for one fingerprint still merge", func(t *testing.T) {
		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 1, nil, entry("a", 0))
		rec.AddEntries(1, 2, nil, entry("b", 0))

		require.Len(t, rec.RefEntries, 1)
		require.Len(t, rec.RefEntries[0].Entries, 2)
		require.Equal(t, int64(2), rec.RefEntries[0].Counter)
	})

	t.Run("pooled pushes for one fingerprint stay separate", func(t *testing.T) {
		poolA := sets(attrs("service_name", "checkout"))
		poolB := sets(attrs("service_name", "payments"))

		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 1, poolA, entry("a", 1))
		rec.AddEntries(1, 2, poolB, entry("b", 1))

		require.Len(t, rec.RefEntries, 2)
		require.Equal(t, poolA, rec.RefEntries[0].SharedStructuredMetadataSets)
		require.Equal(t, poolB, rec.RefEntries[1].SharedStructuredMetadataSets)

		// Both survive the round trip with their own pool, so reference 1 still means
		// "checkout" for the first and "payments" for the second.
		decoded := roundTrip(t, rec, WALRecordEntriesV4)
		require.Equal(t, rec.RefEntries, decoded.RefEntries)
	})

	t.Run("a pool-less push never merges into a pooled RefEntries", func(t *testing.T) {
		rec := &Record{entryIndexMap: map[uint64]int{}}
		rec.AddEntries(1, 1, nil, entry("a", 0))
		rec.AddEntries(1, 2, sets(attrs("service_name", "checkout")), entry("b", 1))
		rec.AddEntries(1, 3, nil, entry("c", 0))

		require.Len(t, rec.RefEntries, 2)
		// The two pool-less pushes merged with each other, not with the pooled one.
		require.Empty(t, rec.RefEntries[0].SharedStructuredMetadataSets)
		require.Len(t, rec.RefEntries[0].Entries, 2)
		require.Len(t, rec.RefEntries[1].SharedStructuredMetadataSets, 1)
		require.Len(t, rec.RefEntries[1].Entries, 1)
	})
}

// TestResetReleasesPool makes sure a record going back to the pool stops referencing the push it
// came from: the shared sets and the entries alias the caller's memory.
func TestResetReleasesPool(t *testing.T) {
	rec := &Record{entryIndexMap: map[uint64]int{}}
	rec.AddEntries(1, 0, sets(attrs("service_name", "checkout")), logproto.Entry{Timestamp: time.Unix(1, 0), Line: "a", SharedResourceRef: 1})

	retained := rec.RefEntries[:1]
	rec.Reset()

	require.Nil(t, retained[0].SharedStructuredMetadataSets)
	require.Nil(t, retained[0].Entries)
	require.True(t, rec.IsEmpty())
}

// TestDecodeUnknownRecordType is the path an ingester that predates a record version takes: the
// record is rejected rather than misread, and the caller counts it as a WAL corruption.
func TestDecodeUnknownRecordType(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    []byte
	}{
		{name: "a version this build does not know", b: []byte{byte(WALRecordEntriesV4 + 1)}},
		{name: "the zero record type", b: []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := DecodeRecord(tc.b, &Record{})
			require.Error(t, err)
			require.Contains(t, err.Error(), "unknown record type")
		})
	}

	t.Run("a truncated V4 record errors rather than being half applied", func(t *testing.T) {
		rec := &Record{
			UserID: "123",
			RefEntries: []RefEntries{{
				Ref:                          1,
				SharedStructuredMetadataSets: sets(attrs("service_name", "checkout")),
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1, 0), Line: "a", SharedResourceRef: 1},
				},
			}},
		}
		full := rec.EncodeEntries(WALRecordEntriesV4, nil)

		// Truncating must never yield a partially applied RefEntries: either the decode is
		// rejected, or nothing was decoded yet (a cut short of the first entry leaves an empty
		// record, which applies nothing), or the whole record came through. What must not
		// happen is entries surfacing with references pointing into a pool that was itself cut
		// short.
		for cut := 1; cut < len(full); cut++ {
			decoded := &Record{}
			if err := DecodeRecord(full[:cut], decoded); err != nil {
				continue
			}
			if len(decoded.RefEntries) == 0 {
				continue
			}
			require.Equal(t, rec.RefEntries, decoded.RefEntries,
				"a truncated record that decodes without error must be the whole record (cut at %d)", cut)
		}
	})
}
