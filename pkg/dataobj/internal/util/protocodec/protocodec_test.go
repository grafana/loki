package protocodec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestDecode(t *testing.T) {
	t.Run("decodes a message larger than the size that Decode reserves", func(t *testing.T) {
		want := &datasetmd.ColumnMetadata{Pages: []*datasetmd.PageDesc{{
			Statistics: &datasetmd.Statistics{MinValue: make([]byte, maxDecodeSizeHint+1)},
		}}}
		var buf bytes.Buffer
		require.NoError(t, Encode(&buf, want))
		var got datasetmd.ColumnMetadata

		require.NoError(t, Decode(&buf, &got))

		require.Equal(t, want, &got)
	})

	t.Run("returns an error, and does not panic, when the size is larger than the stream", func(t *testing.T) {
		var got datasetmd.ColumnMetadata
		const size = 1 << 62

		err := Decode(bytes.NewReader(binary.AppendUvarint(nil, size)), &got)

		require.EqualError(t, err, "read proto message: got=0 want=4611686018427387904")
	})
}

func TestDecodeBytes(t *testing.T) {
	want := &datasetmd.ColumnMetadata{}
	for i := range 3 {
		want.Pages = append(want.Pages, &datasetmd.PageDesc{
			UncompressedSize: 1 << 20,
			RowsCount:        10000,
			DataOffset:       uint64(i) * 300000,
			DataSize:         300000 + uint64(i),
			Statistics: &datasetmd.Statistics{
				MinValue: []byte("0123456789abcdef"),
				MaxValue: []byte("fedcba9876543210"),
			},
		})
	}

	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, want))
	encoded := buf.Bytes()

	t.Run("decodes the message that Encode wrote", func(t *testing.T) {
		var got datasetmd.ColumnMetadata

		require.NoError(t, DecodeBytes(encoded, &got))

		require.Equal(t, want, &got)
	})

	t.Run("ignores the bytes after the message", func(t *testing.T) {
		var got datasetmd.ColumnMetadata

		require.NoError(t, DecodeBytes(append(bytes.Clone(encoded), "trailing bytes"...), &got))

		require.Equal(t, want, &got)
	})

	t.Run("decodes a message that does not change when the input changes", func(t *testing.T) {
		input := bytes.Clone(encoded)
		var got datasetmd.ColumnMetadata
		require.NoError(t, DecodeBytes(input, &got))

		for i := range input {
			input[i] = 0xff
		}

		require.Equal(t, want, &got)
	})

	t.Run("returns an error when the size is invalid", func(t *testing.T) {
		tests := []struct {
			name  string
			input []byte
		}{
			{name: "is missing", input: nil},
			{name: "does not fit in 64 bits", input: bytes.Repeat([]byte{0xff}, 11)},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var got datasetmd.ColumnMetadata

				err := DecodeBytes(tc.input, &got)

				require.EqualError(t, err, "read proto message size: the size is truncated or too large")
			})
		}
	})

	t.Run("returns an error when the message is shorter than its size", func(t *testing.T) {
		var got datasetmd.ColumnMetadata
		size, _ := binary.Uvarint(encoded)

		err := DecodeBytes(encoded[:len(encoded)-1], &got)

		require.EqualError(t, err, fmt.Sprintf("read proto message: got=%d want=%d", size-1, size))
	})

	t.Run("returns an error, and does not panic, when the size is the largest uint64", func(t *testing.T) {
		var got datasetmd.ColumnMetadata

		err := DecodeBytes(binary.AppendUvarint(nil, math.MaxUint64), &got)

		require.EqualError(t, err, "read proto message: got=0 want=18446744073709551615")
	})

	t.Run("returns an error when the message does not decode", func(t *testing.T) {
		var got datasetmd.ColumnMetadata
		truncatedField := []byte{2, 0x0a, 0x0a}

		err := DecodeBytes(truncatedField, &got)

		require.EqualError(t, err, "unmarshal proto message: unexpected EOF")
	})
}
