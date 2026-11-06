package watermarks

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func TestIter(t *testing.T) {
	t.Run("visits the rows of every watermarks section in object order", func(t *testing.T) {
		ob := dataobj.NewBuilder(nil)
		for i, topic := range []string{"topic-a", "topic-b"} {
			wb := NewBuilder(NewMetrics(nil), tenantID, 1024, 0)
			require.NoError(t, wb.Set(topic, int32(i), int64(i)))
			require.NoError(t, ob.Append(wb))
		}
		obj, closer, err := ob.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, []Watermark{
			{Topic: "topic-a", Partition: 0, Watermark: 0},
			{Topic: "topic-b", Partition: 1, Watermark: 1},
		}, iterAll(t, obj))
	})

	t.Run("stops when the caller stops", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic", 0, 1))
			require.NoError(t, b.Set("topic", 1, 2))
		})

		var got []Watermark
		for res := range Iter(t.Context(), obj) {
			wm, err := res.Value()
			require.NoError(t, err)
			got = append(got, wm)
			break
		}
		require.Len(t, got, 1)
	})

	t.Run("reads no rows from an object without a watermarks section", func(t *testing.T) {
		require.Empty(t, iterAll(t, buildOtherSectionObject(t)))
	})
}

func TestDecodeRow(t *testing.T) {
	columns := []*Column{
		{Type: ColumnTypeTopic},
		{Type: ColumnTypePartition},
		{Type: ColumnTypeWatermark},
	}

	t.Run("decodes a valid row", func(t *testing.T) {
		var wm Watermark
		row := dataset.Row{Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(0), dataset.Int64Value(0),
		}}

		require.NoError(t, decodeRow(columns, row, &wm))
		require.Equal(t, Watermark{Topic: "topic"}, wm)
	})

	for name, row := range map[string]dataset.Row{
		"fails when the topic is empty": {Values: []dataset.Value{
			dataset.BinaryValue(nil), dataset.Int64Value(1), dataset.Int64Value(1),
		}},
		"fails when the topic has the wrong physical type": {Values: []dataset.Value{
			dataset.Int64Value(1), dataset.Int64Value(1), dataset.Int64Value(1),
		}},
		"fails when the partition is nil": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), {}, dataset.Int64Value(1),
		}},
		"fails when the partition overflows int32": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(math.MaxInt32 + 1), dataset.Int64Value(1),
		}},
		"fails when the partition is negative": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(-1), dataset.Int64Value(1),
		}},
		"fails when the watermark is negative": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(1), dataset.Int64Value(-1),
		}},
		"fails when the watermark is nil": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(1), {},
		}},
		"fails when the watermark has the wrong physical type": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(1), dataset.BinaryValue([]byte("x")),
		}},
		"fails when the row has more values than columns": {Values: []dataset.Value{
			dataset.BinaryValue([]byte("topic")), dataset.Int64Value(1), dataset.Int64Value(1), dataset.Int64Value(1),
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var wm Watermark
			require.Error(t, decodeRow(columns, row, &wm))
		})
	}
}
