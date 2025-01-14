package dataset_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

func TestSort(t *testing.T) {
	var (
		in     = []int64{1, 5, 3, 2, 9, 6, 8, 4, 7}
		expect = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9}
	)

	colBuilder, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: 1, // Generate a ton of pages.

		Value:       datasetmd.VALUE_TYPE_INT64,
		Encoding:    datasetmd.ENCODING_TYPE_DELTA,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
	require.NoError(t, err)

	for i, v := range in {
		require.NoError(t, colBuilder.Append(i, dataset.Int64Value(v)))
	}
	col, err := colBuilder.Flush()
	require.NoError(t, err)

	dset := dataset.FromMemory([]*dataset.MemColumn{col})
	dset, err = dataset.Sort(context.Background(), dset, []dataset.Column{col}, 1024)
	require.NoError(t, err)

	newColumns, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	var actual []int64
	for entry := range dataset.Iter(context.Background(), newColumns) {
		row, err := entry.Value()
		require.NoError(t, err)
		require.Equal(t, len(actual), row.Index)
		require.Len(t, row.Values, 1)
		require.Equal(t, datasetmd.VALUE_TYPE_INT64, row.Values[0].Type())

		actual = append(actual, row.Values[0].Int64())
	}
	require.Equal(t, expect, actual)
}

func TestSort_MultipleFields(t *testing.T) {
	type Person struct {
		Name     string
		Age      int
		Location string
	}
	people := []Person{
		{"Bob", 25, "New York"},
		{"David", 35, "Chicago"},
		{"Eve", 30, "Los Angeles"},
		{"Alice", 30, "San Francisco"},
		{"Charlie", 40, ""}, // Ensure NULLs are handled properly in sorting.
	}

	nameBuilder, err := dataset.NewColumnBuilder("name", dataset.BuilderOptions{
		Value:       datasetmd.VALUE_TYPE_STRING,
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
	require.NoError(t, err)

	ageBuilder, err := dataset.NewColumnBuilder("age", dataset.BuilderOptions{
		Value:       datasetmd.VALUE_TYPE_INT64,
		Encoding:    datasetmd.ENCODING_TYPE_DELTA,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
	require.NoError(t, err)

	locationBuilder, err := dataset.NewColumnBuilder("location", dataset.BuilderOptions{
		Value:       datasetmd.VALUE_TYPE_STRING,
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
	require.NoError(t, err)

	for i, p := range people {
		require.NoError(t, nameBuilder.Append(i, dataset.StringValue(p.Name)))
		require.NoError(t, ageBuilder.Append(i, dataset.Int64Value(int64(p.Age))))
		require.NoError(t, locationBuilder.Append(i, dataset.StringValue(p.Location)))
	}

	nameCol, err := nameBuilder.Flush()
	require.NoError(t, err)
	ageCol, err := ageBuilder.Flush()
	require.NoError(t, err)
	locationCol, err := locationBuilder.Flush()
	require.NoError(t, err)

	dset := dataset.FromMemory([]*dataset.MemColumn{nameCol, ageCol, locationCol})
	dset, err = dataset.Sort(context.Background(), dset, []dataset.Column{ageCol, nameCol}, 1024)
	require.NoError(t, err)

	newColumns, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	expect := []Person{
		{"Bob", 25, "New York"},
		{"Alice", 30, "San Francisco"},
		{"Eve", 30, "Los Angeles"},
		{"David", 35, "Chicago"},
		{"Charlie", 40, ""},
	}
	var actual []Person

	for result := range dataset.Iter(context.Background(), newColumns) {
		row, err := result.Value()
		require.NoError(t, err)
		require.Equal(t, len(actual), row.Index)
		require.Len(t, row.Values, 3)

		var p Person

		if !row.Values[0].IsNil() {
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, row.Values[0].Type())
			p.Name = row.Values[0].String()
		}
		if !row.Values[1].IsNil() {
			require.Equal(t, datasetmd.VALUE_TYPE_INT64, row.Values[1].Type())
			p.Age = int(row.Values[1].Int64())
		}
		if !row.Values[2].IsNil() {
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, row.Values[2].Type())
			p.Location = row.Values[2].String()
		}

		actual = append(actual, p)
	}
	require.Equal(t, expect, actual)
}
