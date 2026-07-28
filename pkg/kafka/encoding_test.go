package kafka

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

func TestEncoderDecoder(t *testing.T) {
	tests := []struct {
		name        string
		stream      logproto.Stream
		maxSize     int
		expectSplit bool
	}{
		{
			name:        "Small stream, no split",
			stream:      generateStream(10, 100),
			maxSize:     1024 * 1024,
			expectSplit: false,
		},
		{
			name:        "Large stream, expect split",
			stream:      generateStream(1000, 1000),
			maxSize:     1024 * 10,
			expectSplit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder, err := NewDecoder()
			require.NoError(t, err)

			records, err := Encode(0, "test-tenant", tt.stream, tt.maxSize)
			require.NoError(t, err)

			if tt.expectSplit {
				require.Greater(t, len(records), 1)
			} else {
				require.Equal(t, 1, len(records))
			}

			var decodedEntries []logproto.Entry
			var decodedLabels labels.Labels

			for _, record := range records {
				stream, ls, err := decoder.Decode(record.Value)
				require.NoError(t, err)
				decodedEntries = append(decodedEntries, stream.Entries...)
				if decodedLabels.IsEmpty() {
					decodedLabels = ls
				} else {
					require.Equal(t, decodedLabels, ls)
				}
			}

			require.Equal(t, tt.stream.Labels, decodedLabels.String())
			require.Equal(t, len(tt.stream.Entries), len(decodedEntries))
			for i, entry := range tt.stream.Entries {
				require.Equal(t, entry.Timestamp.UTC(), decodedEntries[i].Timestamp.UTC())
				require.Equal(t, entry.Line, decodedEntries[i].Line)
			}
		})
	}
}

func TestEncoderSingleEntryTooLarge(t *testing.T) {
	stream := generateStream(1, 1000)

	_, err := Encode(0, "test-tenant", stream, 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "single entry size")
}

func TestDecoderInvalidData(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	_, _, err = decoder.Decode([]byte("invalid data"))
	require.Error(t, err)
}

func TestEncoderDecoderEmptyStream(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels: `{app="test"}`,
	}

	records, err := Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	decodedStream, decodedLabels, err := decoder.Decode(records[0].Value)
	require.NoError(t, err)
	require.Equal(t, stream.Labels, decodedLabels.String())
	require.Empty(t, decodedStream.Entries)
}

func TestEncoderDecoderSharedStructuredMetadata(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	stream := generateStream(10, 100)
	stream.SharedStructuredMetadataSets = generateSharedSets(2, 5, 32)
	setRefs(&stream, 1, 2)

	records, err := Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	decoded, ls, err := decoder.Decode(records[0].Value)
	require.NoError(t, err)
	require.Equal(t, stream.Labels, ls.String())
	require.Equal(t, stream.SharedStructuredMetadataSets, decoded.SharedStructuredMetadataSets)
	require.Len(t, decoded.Entries, len(stream.Entries))
	require.NoError(t, decoded.ValidateSharedRefs())

	// The shared sets ride at the stream level only, they must not have leaked into the
	// entries, and every entry must keep the references it was given.
	for i := range decoded.Entries {
		require.Empty(t, decoded.Entries[i].StructuredMetadata)
		require.Equal(t, uint32(1), decoded.Entries[i].SharedResourceRef)
		require.Equal(t, uint32(2), decoded.Entries[i].SharedScopeRef)

		resource, scope := decoded.SharedFor(&decoded.Entries[i])
		require.Equal(t, push.LabelsAdapter(stream.SharedStructuredMetadataSets[0].Attrs), resource)
		require.Equal(t, push.LabelsAdapter(stream.SharedStructuredMetadataSets[1].Attrs), scope)
	}

	// DecodeWithoutLabels allocates a fresh stream per call, so it has nothing to reset,
	// but it must still surface the pool.
	withoutLabels, err := decoder.DecodeWithoutLabels(records[0].Value)
	require.NoError(t, err)
	require.Equal(t, stream.SharedStructuredMetadataSets, withoutLabels.SharedStructuredMetadataSets)
}

// TestEncoderSharedStructuredMetadataSplit asserts that when a stream is split across
// several records, every record carries the full shared structured metadata pool, so that the
// references of the entries it holds keep resolving, and still respects the size limit on its
// own.
func TestEncoderSharedStructuredMetadataSplit(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	const maxSize = 1024 * 10

	stream := generateStream(1000, 1000)
	// Own structured metadata on the entries, to make sure it survives alongside the
	// shared sets and is not confused with them.
	for i := range stream.Entries {
		stream.Entries[i].StructuredMetadata = push.LabelsAdapter{
			{Name: "entry_id", Value: fmt.Sprintf("%d", i)},
		}
	}
	stream.SharedStructuredMetadataSets = generateSharedSets(3, 20, 64)
	// Spread the references over the pool so that most records end up holding entries that
	// only reference part of it.
	for i := range stream.Entries {
		stream.Entries[i].SharedResourceRef = uint32(i%3) + 1
		stream.Entries[i].SharedScopeRef = uint32((i+1)%3) + 1
	}

	records, err := Encode(0, "test-tenant", stream, maxSize)
	require.NoError(t, err)
	require.Greater(t, len(records), 1)

	var decodedEntries []logproto.Entry
	for _, record := range records {
		// Each record must independently fit within the limit, pool included.
		require.LessOrEqual(t, len(record.Value), maxSize)

		decoded, _, err := decoder.Decode(record.Value)
		require.NoError(t, err)
		// Every record repeats the whole pool so it is self-contained, even for the sets
		// none of its own entries reference.
		require.Equal(t, stream.SharedStructuredMetadataSets, decoded.SharedStructuredMetadataSets)
		require.NotEmpty(t, decoded.Entries)
		require.NoError(t, decoded.ValidateSharedRefs())

		decodedEntries = append(decodedEntries, decoded.Entries...)
	}

	require.Len(t, decodedEntries, len(stream.Entries))
	for i, entry := range stream.Entries {
		require.Equal(t, entry.Line, decodedEntries[i].Line)
		// Entries keep their own structured metadata and their references, and nothing
		// else: the shared sets are not expanded into them.
		require.Equal(t, entry.StructuredMetadata, decodedEntries[i].StructuredMetadata)
		require.Equal(t, entry.SharedResourceRef, decodedEntries[i].SharedResourceRef)
		require.Equal(t, entry.SharedScopeRef, decodedEntries[i].SharedScopeRef)
	}
}

// TestEncoderNoSharedStructuredMetadataNoRegression asserts that a stream without a shared
// structured metadata pool is encoded exactly as it was before the pool existed: no extra
// bytes on the wire and no extra records.
func TestEncoderNoSharedStructuredMetadataNoRegression(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	const maxSize = 1024 * 10

	stream := generateStream(1000, 1000)
	require.Empty(t, stream.SharedStructuredMetadataSets)

	records, err := Encode(0, "test-tenant", stream, maxSize)
	require.NoError(t, err)
	require.Greater(t, len(records), 1)

	for _, record := range records {
		decoded, _, err := decoder.Decode(record.Value)
		require.NoError(t, err)
		require.Empty(t, decoded.SharedStructuredMetadataSets)
		for _, entry := range decoded.Entries {
			require.Zero(t, entry.SharedResourceRef)
			require.Zero(t, entry.SharedScopeRef)
		}

		// Re-marshalling the record without the pool must produce the exact same bytes,
		// which proves no field 5 was written and that the record did not grow.
		withoutShared := logproto.Stream{
			Labels:  decoded.Labels,
			Hash:    decoded.Hash,
			Entries: decoded.Entries,
		}
		expected, err := withoutShared.Marshal()
		require.NoError(t, err)
		require.Equal(t, expected, record.Value)
	}
}

// TestEncoderSharedStructuredMetadataTooLarge asserts that the shared structured metadata pool
// is charged to the per-record budget: if it leaves no room for a single entry, encoding
// fails rather than emitting an oversized record.
func TestEncoderSharedStructuredMetadataTooLarge(t *testing.T) {
	stream := generateStream(10, 100)
	stream.SharedStructuredMetadataSets = generateSharedSets(2, 10, 100)
	setRefs(&stream, 1, 2)

	// Base cost of a record for this stream: labels plus the shared structured metadata pool.
	base := logproto.Stream{
		Labels:                       stream.Labels,
		SharedStructuredMetadataSets: stream.SharedStructuredMetadataSets,
	}
	// Leaves plenty of room for an entry if the pool is ignored, but none once it is charged
	// to the record.
	maxSize := base.Size() + 50

	_, err := Encode(0, "test-tenant", stream, maxSize)
	require.Error(t, err)
	require.Contains(t, err.Error(), "single entry size")
	require.Contains(t, err.Error(), "shared structured metadata pool")
}

// TestEncoderSharedStructuredMetadataLeavesNoRoom asserts that a shared structured metadata
// pool big enough to fill the per-record budget on its own fails loudly: no amount of
// splitting can produce records that fit, since every record repeats it.
func TestEncoderSharedStructuredMetadataLeavesNoRoom(t *testing.T) {
	stream := generateStream(10, 100)
	stream.SharedStructuredMetadataSets = generateSharedSets(2, 10, 100)
	setRefs(&stream, 1, 2)

	base := logproto.Stream{
		Labels:                       stream.Labels,
		Hash:                         stream.Hash,
		SharedStructuredMetadataSets: stream.SharedStructuredMetadataSets,
	}
	// Not even an empty entry fits alongside the base.
	maxSize := base.Size() + 1

	_, err := Encode(0, "test-tenant", stream, maxSize)
	require.Error(t, err)
	require.Contains(t, err.Error(), "leaves no room for a single entry")
	require.Contains(t, err.Error(), "shared structured metadata pool")
}

// TestEncoderLargeEntryAfterTheFirstOne is a regression test: the per-entry size check used to
// only compare the first entry against the per-record base, so a bigger entry later in the
// stream could be flushed into a record of its own that was over the limit.
func TestEncoderLargeEntryAfterTheFirstOne(t *testing.T) {
	const maxSize = 2048

	stream := generateStream(4, 100)
	stream.SharedStructuredMetadataSets = generateSharedSets(2, 5, 100)
	setRefs(&stream, 1, 2)

	base := logproto.Stream{
		Labels:                       stream.Labels,
		Hash:                         stream.Hash,
		SharedStructuredMetadataSets: stream.SharedStructuredMetadataSets,
	}
	baseSize := base.Size()
	require.Less(t, baseSize, maxSize, "the base must leave room for the small entries")

	// An entry that fits within maxSize on its own but not alongside the per-record base.
	stream.Entries[2].Line = generateRandomString(maxSize - baseSize + 64)
	require.Less(t, stream.Entries[2].Size(), maxSize)

	_, err := Encode(0, "test-tenant", stream, maxSize)
	require.Error(t, err)
	require.Contains(t, err.Error(), "single entry size")
}

// TestEncoderEveryRecordFitsWithLargeSharedMetadata is the positive counterpart: with a pool
// that eats most of the budget, encoding still succeeds and no record exceeds the limit.
func TestEncoderEveryRecordFitsWithLargeSharedMetadata(t *testing.T) {
	const maxSize = 4096

	stream := generateStream(200, 200)
	stream.SharedStructuredMetadataSets = generateSharedSets(2, 10, 100)
	setRefs(&stream, 1, 2)

	records, err := Encode(0, "test-tenant", stream, maxSize)
	require.NoError(t, err)
	require.Greater(t, len(records), 1)
	for _, record := range records {
		require.LessOrEqual(t, len(record.Value), maxSize)
	}
}

// TestDecoderReuseResetsSharedStructuredMetadata is a regression test: the Decoder reuses
// its logproto.Stream across calls and proto unmarshalling appends to repeated fields, so
// without an explicit reset a record would inherit the shared structured metadata pool of the
// previously decoded record, which would also silently change what the references of its
// entries resolve to.
func TestDecoderReuseResetsSharedStructuredMetadata(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	sets := generateSharedSets(2, 3, 16)

	withShared := generateStream(2, 50)
	withShared.SharedStructuredMetadataSets = sets
	setRefs(&withShared, 1, 2)

	withoutShared := generateStream(2, 50)

	records, err := Encode(0, "test-tenant", withShared, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)
	decoded, _, err := decoder.Decode(records[0].Value)
	require.NoError(t, err)
	require.Equal(t, sets, decoded.SharedStructuredMetadataSets)

	// Decoding the same record again must not accumulate the pool sets.
	decoded, _, err = decoder.Decode(records[0].Value)
	require.NoError(t, err)
	require.Equal(t, sets, decoded.SharedStructuredMetadataSets)

	// A record without a pool must not inherit the previous one, which would otherwise make
	// its zero references look like they resolve to something.
	records, err = Encode(0, "test-tenant", withoutShared, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)
	decoded, _, err = decoder.Decode(records[0].Value)
	require.NoError(t, err)
	require.Empty(t, decoded.SharedStructuredMetadataSets)
}

func BenchmarkEncodeDecode(b *testing.B) {
	decoder, _ := NewDecoder()
	stream := generateStream(1000, 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		records, err := Encode(0, "test-tenant", stream, 10<<20)
		if err != nil {
			b.Fatal(err)
		}
		for _, record := range records {
			_, _, err := decoder.Decode(record.Value)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Helper function to generate a test stream
func generateStream(entries, lineLength int) logproto.Stream {
	stream := logproto.Stream{
		Labels:  `{app="test", env="prod"}`,
		Entries: make([]logproto.Entry, entries),
	}

	for i := 0; i < entries; i++ {
		stream.Entries[i] = logproto.Entry{
			Timestamp: time.Now(),
			Line:      generateRandomString(lineLength),
		}
	}

	return stream
}

// Helper function to generate a stream-level shared structured metadata pool
func generateSharedSets(sets, pairs, valueLength int) []logproto.SharedStructuredMetadataSet {
	pool := make([]logproto.SharedStructuredMetadataSet, 0, sets)
	for s := 0; s < sets; s++ {
		attrs := make([]push.LabelAdapter, 0, pairs)
		for i := 0; i < pairs; i++ {
			attrs = append(attrs, push.LabelAdapter{
				Name:  fmt.Sprintf("set_%d_attr_%d", s, i),
				Value: generateRandomString(valueLength),
			})
		}
		pool = append(pool, logproto.SharedStructuredMetadataSet{Attrs: attrs})
	}
	return pool
}

// setRefs points every entry of the stream at the same resource and scope set.
func setRefs(stream *logproto.Stream, resourceRef, scopeRef uint32) {
	for i := range stream.Entries {
		stream.Entries[i].SharedResourceRef = resourceRef
		stream.Entries[i].SharedScopeRef = scopeRef
	}
}

// Helper function to generate a random string
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
