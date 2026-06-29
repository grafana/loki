package postings

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

func TestMapPredicate_BloomMatch(t *testing.T) {
	hit := bloom.NewWithEstimates(100, 0.01)
	hit.Add([]byte("foo"))
	hitBytes, err := hit.MarshalBinary()
	require.NoError(t, err)

	miss := bloom.NewWithEstimates(100, 0.01)
	miss.Add([]byte("bar"))
	missBytes, err := miss.MarshalBinary()
	require.NoError(t, err)

	col := &Column{Name: "bloom_filter", Type: ColumnTypeBloomFilter}
	rows := readMappedRows(t, col, [][]byte{hitBytes, missBytes, []byte("not-a-bloom")},
		BloomMatchPredicate{Column: col, Value: []byte("foo")})

	require.Len(t, rows, 1)
	require.Equal(t, hitBytes, rows[0].Values[0].Binary())
}

func TestMapPredicate_RegexMatch(t *testing.T) {
	re, err := labels.NewFastRegexMatcher("foo.*")
	require.NoError(t, err)

	col := &Column{Name: "label_value", Type: ColumnTypeLabelValue}
	rows := readMappedRows(t, col, [][]byte{[]byte("foobar"), []byte("baz"), []byte("foo")},
		RegexMatchPredicate{Column: col, Matcher: re})

	require.Len(t, rows, 2)
	require.Equal(t, []byte("foobar"), rows[0].Values[0].Binary())
	require.Equal(t, []byte("foo"), rows[1].Values[0].Binary())
}

// readMappedRows builds a single-column binary dataset from values, translates
// pred through mapPredicate, and returns the rows that survive the resulting
// dataset.FuncPredicate.
func readMappedRows(t *testing.T, col *Column, values [][]byte, pred Predicate) []dataset.Row {
	t.Helper()

	builder, err := dataset.NewColumnBuilder("predicate_test", dataset.BuilderOptions{
		Type:        dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "binary"},
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)

	for i, val := range values {
		require.NoError(t, builder.Append(i, dataset.BinaryValue(val)))
	}

	memCol, err := builder.Flush()
	require.NoError(t, err)

	dset := dataset.FromMemory([]*dataset.MemColumn{memCol})
	cols, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)
	require.Len(t, cols, 1)

	preds, err := mapPredicates([]Predicate{pred}, map[*Column]dataset.Column{col: cols[0]})
	require.NoError(t, err)

	r := dataset.NewRowReader(dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    cols,
		Predicates: preds,
	})
	defer r.Close()
	require.NoError(t, r.Open(context.Background()))

	var rows []dataset.Row
	buf := make([]dataset.Row, len(values))
	for {
		clear(buf)
		n, err := r.Read(context.Background(), buf)
		rows = append(rows, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	return rows
}
