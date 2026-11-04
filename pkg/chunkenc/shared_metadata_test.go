package chunkenc

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestAppendWithSharedMatchesExpandedChunks(t *testing.T) {
	shared := []logproto.LabelAdapter{{Name: "z_resource", Value: "resource"}, {Name: "override", Value: "scope"}, {Name: "detected_level", Value: "info"}}
	for _, format := range []byte{ChunkFormatV3, ChunkFormatV4} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			interner := NewSharedMetadata(shared)
			// Reuse a group over chunk changes; old symbols must remain immutable.
			var previous []*MemChunk
			for cycle := range 3 {
				flat := NewMemChunk(format, compression.GZIP, UnorderedWithStructuredMetadataHeadBlockFmt, 100, 0)
				nested := NewMemChunk(format, compression.GZIP, UnorderedWithStructuredMetadataHeadBlockFmt, 100, 0)
				for i := range 20 {
					own := []logproto.LabelAdapter(nil)
					if i%3 == 0 {
						own = []logproto.LabelAdapter{{Name: "override", Value: ""}, {Name: "a_entry", Value: "entry"}}
					}
					entry := logproto.Entry{Timestamp: time.Unix(int64(cycle*100+i), 0), Line: fmt.Sprint(i), StructuredMetadata: own}
					expanded := entry
					expanded.StructuredMetadata = logproto.AppendMetadata(nil, own, shared)
					metadataSize := 0
					for _, attr := range expanded.StructuredMetadata {
						metadataSize += len(attr.Name) + len(attr.Value)
					}
					require.Equal(t, flat.SpaceFor(&expanded), nested.SpaceForWithMetadataSize(&entry, metadataSize))
					dupFlat, errFlat := flat.Append(&expanded)
					dupNested, errNested := nested.AppendWithShared(&entry, interner)
					require.Equal(t, errFlat, errNested)
					require.Equal(t, dupFlat, dupNested)
					// Same timestamp and line must use effective metadata for deduplication.
					dupFlat, errFlat = flat.Append(&expanded)
					dupNested, errNested = nested.AppendWithShared(&entry, interner)
					require.Equal(t, errFlat, errNested)
					require.Equal(t, dupFlat, dupNested)
				}
				require.NoError(t, flat.Close())
				require.NoError(t, nested.Close())
				want, err := flat.Bytes()
				require.NoError(t, err)
				got, err := nested.Bytes()
				require.NoError(t, err)
				require.Equal(t, want, got)
				previous = append(previous, nested)
			}
			for _, c := range previous {
				_, err := c.Bytes()
				require.NoError(t, err)
			}
		})
	}
}

func BenchmarkAppendSharedMetadata(b *testing.B) {
	for _, count := range []int{0, 8, 32} {
		shared := make([]logproto.LabelAdapter, count)
		for i := range shared {
			shared[i] = logproto.LabelAdapter{Name: fmt.Sprintf("resource_%02d", i), Value: "shared-attribute-value"}
		}
		for _, deferred := range []bool{false, true} {
			b.Run(fmt.Sprintf("attrs=%d/deferred=%t", count, deferred), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					chunk := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, 1<<20, 0)
					interner := NewSharedMetadata(shared)
					for i := range 64 {
						entry := logproto.Entry{Timestamp: time.Unix(int64(i), 0), Line: "a log line"}
						var err error
						if deferred {
							_, err = chunk.AppendWithShared(&entry, interner)
						} else {
							entry.StructuredMetadata = logproto.AppendMetadata(nil, entry.StructuredMetadata, shared)
							_, err = chunk.Append(&entry)
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func TestSharedMetadataAfterChunkReorder(t *testing.T) {
	shared := []logproto.LabelAdapter{{Name: "resource", Value: "shared"}}
	interner := NewSharedMetadata(shared)
	flat := NewMemChunk(ChunkFormatV4, compression.GZIP, UnorderedWithStructuredMetadataHeadBlockFmt, 1, 0)
	nested := NewMemChunk(ChunkFormatV4, compression.GZIP, UnorderedWithStructuredMetadataHeadBlockFmt, 1, 0)
	for _, timestamp := range []int64{3, 1, 2} {
		entry := logproto.Entry{Timestamp: time.Unix(timestamp, 0), Line: "test"}
		if timestamp == 3 {
			entry.StructuredMetadata = []logproto.LabelAdapter{{Name: "a_entry", Value: "changes-symbol-order"}}
		}
		expanded := entry
		expanded.StructuredMetadata = logproto.AppendMetadata(nil, entry.StructuredMetadata, shared)
		_, err := flat.Append(&expanded)
		require.NoError(t, err)
		_, err = nested.AppendWithShared(&entry, interner)
		require.NoError(t, err)
	}
	originalTable := nested.symbolizer
	require.NoError(t, flat.Close())
	require.NoError(t, nested.Close())
	require.NotSame(t, originalTable, nested.symbolizer, "unordered blocks should rebuild the symbol table")
	entry := logproto.Entry{Timestamp: time.Unix(4, 0), Line: "after close"}
	expanded := entry
	expanded.StructuredMetadata = shared
	_, err := flat.Append(&expanded)
	require.NoError(t, err)
	_, err = nested.AppendWithShared(&entry, interner)
	require.NoError(t, err)
	require.NoError(t, flat.Close())
	require.NoError(t, nested.Close())
	want, err := flat.Bytes()
	require.NoError(t, err)
	got, err := nested.Bytes()
	require.NoError(t, err)
	require.Equal(t, want, got)
}
