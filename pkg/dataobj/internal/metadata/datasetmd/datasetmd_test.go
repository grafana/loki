package datasetmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestPageDesc_Unmarshal(t *testing.T) {
	t.Run("skips the compressed_size field that older writers wrote", func(t *testing.T) {
		want := &PageDesc{
			UncompressedSize: 1 << 20,
			RowsCount:        10000,
			DataOffset:       300000,
			DataSize:         300001,
			Statistics:       &Statistics{MinValue: []byte("a"), MaxValue: []byte("z")},
		}
		encoded, err := want.Marshal()
		require.NoError(t, err)
		const compressedSizeField = 2
		encoded = protowire.AppendTag(encoded, compressedSizeField, protowire.VarintType)
		encoded = protowire.AppendVarint(encoded, 300001)
		var got PageDesc

		require.NoError(t, got.Unmarshal(encoded))

		require.Equal(t, want, &got)
	})
}
