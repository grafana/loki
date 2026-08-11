package wirecodec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestManifest(t *testing.T) {
	t.Run("round trips its binary representation", func(t *testing.T) {
		expected := wirecodec.Manifest{
			TypeBuffer:       1,
			LayoutBuffer:     2,
			FileStatsBuffer:  3,
			DictionaryBuffer: 4,
		}

		data, err := expected.MarshalBinary()
		require.NoError(t, err)
		var actual wirecodec.Manifest
		require.NoError(t, actual.UnmarshalBinary(data))

		require.Equal(t, expected, actual)
	})

	t.Run("exposes layout and array metadata", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore
		root, _ := writeTestStruct(t, &alloc, &store)
		manifest, err := wirecodec.Build(t.Context(), root, &store)
		require.NoError(t, err)

		metadata, err := manifest.LayoutMetadata(t.Context(), &store, manifest.LayoutBuffer)
		require.NoError(t, err)

		require.Equal(t, "dataset.struct", metadata.Encoding)
		require.Equal(t, uint64(4), metadata.Rows)
		require.Empty(t, metadata.Buffers)
		require.Len(t, metadata.Children, 3)

		idField := metadata.Children[0]
		require.Equal(t, "dataset.chunked", idField.Encoding)
		require.Equal(t, uint64(4), idField.Rows)
		require.Len(t, idField.Children, 2)

		messageField := metadata.Children[1]
		require.Equal(t, "dataset.chunked", messageField.Encoding)
		require.Equal(t, uint64(4), messageField.Rows)
		require.Len(t, messageField.Children, 2)

		validity := metadata.Children[2]
		require.Equal(t, "dataset.array", validity.Encoding)
		require.Equal(t, uint64(4), validity.Rows)
		require.Len(t, validity.Buffers, 1)

		firstMessageChunk := messageField.Children[0]
		require.Equal(t, "dataset.array", firstMessageChunk.Encoding)
		require.Len(t, firstMessageChunk.Buffers, 1)
		messageArray, err := manifest.ArrayMetadata(t.Context(), &store, firstMessageChunk.Buffers[0])
		require.NoError(t, err)

		require.Equal(t, "dataset.binary", messageArray.Encoding)
		require.Len(t, messageArray.Buffers, 1)
		require.Equal(t, &array.Stats{NullCount: 1}, messageArray.Stats)
		require.Len(t, messageArray.Children, 2)
		require.Equal(t, "dataset.plain", messageArray.Children[0].Encoding)
		require.Equal(t, "dataset.bool", messageArray.Children[1].Encoding)
	})

	t.Run("discovers every buffer", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore
		root, _ := writeTestStruct(t, &alloc, &store)
		manifest, err := wirecodec.Build(t.Context(), root, &store)
		require.NoError(t, err)

		metadataBuffers := map[buffer.ID]struct{}{
			manifest.TypeBuffer:       {},
			manifest.LayoutBuffer:     {},
			manifest.DictionaryBuffer: {},
		}
		dataBuffers := map[buffer.ID]struct{}{}

		rootMetadata, err := manifest.LayoutMetadata(t.Context(), &store, manifest.LayoutBuffer)
		require.NoError(t, err)
		collectManifestBuffers(t, manifest, &store, rootMetadata, metadataBuffers, dataBuffers)

		for i := range store.Len() {
			id := buffer.ID(i + 1)
			_, isMetadata := metadataBuffers[id]
			_, isData := dataBuffers[id]
			require.True(t, isMetadata || isData, "buffer ID %d was not discovered", id)
			require.False(t, isMetadata && isData, "buffer ID %d is both metadata and data", id)
		}
	})
}

func collectManifestBuffers(t *testing.T, manifest wirecodec.Manifest, source buffer.Source, root wirecodec.Layout, metadataBuffers, dataBuffers map[buffer.ID]struct{}) {
	t.Helper()

	for _, id := range root.Buffers {
		metadataBuffers[id] = struct{}{}
	}
	if root.Encoding == "dataset.array" {
		require.Len(t, root.Buffers, 1)
		metadata, err := manifest.ArrayMetadata(t.Context(), source, root.Buffers[0])
		require.NoError(t, err)
		collectArrayBuffers(metadata, dataBuffers)
	}
	for _, child := range root.Children {
		collectManifestBuffers(t, manifest, source, child, metadataBuffers, dataBuffers)
	}
}

func collectArrayBuffers(root wirecodec.Array, buffers map[buffer.ID]struct{}) {
	for _, id := range root.Buffers {
		buffers[id] = struct{}{}
	}
	for _, child := range root.Children {
		collectArrayBuffers(child, buffers)
	}
}
