package dataset_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestReader(t *testing.T) {
	t.Run("reads without a filter", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		dset := writeDataset(t, &alloc, &store,
			buildRecord(t, &alloc, 1000, map[string]string{"env": "prod", "service": "web"}, "first"),
			buildRecord(t, &alloc, 2000, map[string]string{"env": "dev", "service": "api"}, "second"),
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
					columnartest.Field("env", types.KindUTF8, "prod", "dev"),
					columnartest.Field("service", types.KindUTF8, "web", "api"),
				),
			),
			columnartest.Field("line", types.KindUTF8, "first", "second"),
		)
		columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
	})

	t.Run("returns EOF for an empty dataset", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		dset := writeDataset(t, &alloc, &store)

		_, err := readDataset(t, &alloc, &store, dataset.ReaderOptions{
			Dataset:    dset,
			Projection: &expr.Identity{},
		})
		require.ErrorIs(t, err, io.EOF)
	})
}
