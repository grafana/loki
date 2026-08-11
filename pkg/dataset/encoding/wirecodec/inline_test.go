package wirecodec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestInline(t *testing.T) {
	t.Run("round trips complete values", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore
		root, expected := writeTestStruct(t, &alloc, &store)

		data, err := wirecodec.MarshalInline(t.Context(), root, &store)
		require.NoError(t, err)
		actualLayout, source, err := wirecodec.UnmarshalInline(t.Context(), data)
		require.NoError(t, err)
		actual := readAll(t, &alloc, actualLayout, source)

		columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
	})

	t.Run("rejects invalid data", func(t *testing.T) {
		tests := []struct {
			name string
			data []byte
		}{
			{name: "empty", data: []byte{}},
			{name: "invalid protobuf", data: []byte("not a protobuf")},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				_, _, err := wirecodec.UnmarshalInline(t.Context(), tc.data)
				require.Error(t, err)
			})
		}
	})
}
