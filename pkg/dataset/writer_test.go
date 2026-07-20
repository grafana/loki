package dataset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestWriter(t *testing.T) {
	t.Run("backfills newly discovered dynamic fields", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		dset := writeDataset(t, &alloc, &store,
			buildRecord(t, &alloc, 1000, map[string]string{"service": "web"}, "first"),
			buildRecord(t, &alloc, 2000, map[string]string{"env": "prod"}, "second"),
		)

		actual, err := readDataset(t, &alloc, &store, dataset.ReaderOptions{
			Dataset:    dset,
			Projection: &expr.Identity{},
		})
		require.NoError(t, err)

		expected := columnartest.Struct(t, &alloc,
			columnartest.Field("timestamp", types.KindInt64, int64(1000), int64(2000)),
			columnartest.Field("metadata", types.KindStruct,
				columnartest.Struct(t, &alloc,
					columnartest.Field("env", types.KindUTF8, nil, "prod"),
					columnartest.Field("service", types.KindUTF8, "web", nil),
				),
			),
			columnartest.Field("line", types.KindUTF8, "first", "second"),
		)
		columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
	})

	t.Run("rejects dynamic rows without keys", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		w, err := dataset.NewWriter(&alloc, &store, testSpec())
		require.NoError(t, err)
		require.NoError(t, w.Append(t.Context(), buildRecord(t, &alloc, 1000, map[string]string{}, "line")))

		_, err = w.Flush(t.Context())
		require.ErrorContains(t, err, "dynamic field metadata has 1 rows but no fields")
	})

	t.Run("returns flush errors", func(t *testing.T) {
		var alloc memory.Allocator
		var sink errorSink

		w, err := dataset.NewWriter(&alloc, sink, testSpec())
		require.NoError(t, err)
		require.NoError(t, w.Append(t.Context(), buildRecord(t, &alloc, 1000, map[string]string{"service": "web"}, "first")))

		_, err = w.Flush(t.Context())
		require.ErrorContains(t, err, "writing buffers")
	})

	t.Run("rejects nil dynamic field specs", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		w, err := dataset.NewWriter(&alloc, &store, dataset.Spec{
			Fields: []dataset.FieldSpec{&dataset.DynamicFieldSpec{
				Name:    "metadata",
				GetSpec: func(string, types.Type) layout.Spec { return nil },
			}},
		})
		require.NoError(t, err)
		input := columnartest.Struct(t, &alloc,
			columnartest.Field("metadata", types.KindStruct,
				buildMetadata(t, &alloc, map[string]string{"service": "web"}),
			),
		)

		err = w.Append(t.Context(), input)
		require.ErrorContains(t, err, "GetSpec returned nil")
	})

	t.Run("rejects missing required fields", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		w, err := dataset.NewWriter(&alloc, &store, testSpec())
		require.NoError(t, err)
		input := columnartest.Struct(t, &alloc,
			columnartest.Field("timestamp", types.KindInt64, int64(1000)),
			columnartest.Field("metadata", types.KindStruct,
				buildMetadata(t, &alloc, map[string]string{}),
			),
		)

		err = w.Append(t.Context(), input)
		require.ErrorContains(t, err, "required field line not found in input")
	})
}
