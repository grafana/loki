package postings

import (
	"testing"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func binaryColumn() dataset.Column {
	return &dataset.MemColumn{
		Desc: dataset.ColumnDesc{
			Type: dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY},
		},
	}
}

func marshaledBloom(t *testing.T, members ...string) []byte {
	t.Helper()
	f := bloom.NewWithEstimates(100, 0.01)
	for _, m := range members {
		f.Add([]byte(m))
	}
	b, err := f.MarshalBinary()
	require.NoError(t, err)
	return b
}

func TestBloomKeep_Hit(t *testing.T) {
	col := binaryColumn()
	keep := bloomKeep(col, []byte("foo"))
	require.True(t, keep(col, dataset.BinaryValue(marshaledBloom(t, "foo"))))
	require.False(t, keep(col, dataset.BinaryValue(marshaledBloom(t, "bar"))))
}

func TestBloomKeep_MalformedValue(t *testing.T) {
	col := binaryColumn()
	keep := bloomKeep(col, []byte("foo"))
	require.False(t, keep(col, dataset.BinaryValue([]byte("not-a-bloom"))))
}

func TestBloomKeep_NilOrTypeMismatch(t *testing.T) {
	col := binaryColumn()
	keep := bloomKeep(col, []byte("foo"))
	require.False(t, keep(col, dataset.Value{}))
	require.False(t, keep(col, dataset.Int64Value(1)))
}

func TestRegexKeep_Match(t *testing.T) {
	re, err := labels.NewFastRegexMatcher("foo.*")
	require.NoError(t, err)
	col := binaryColumn()
	keep := regexKeep(col, re)
	require.True(t, keep(col, dataset.BinaryValue([]byte("foobar"))))
	require.False(t, keep(col, dataset.BinaryValue([]byte("baz"))))
}

func TestRegexKeep_NilOrTypeMismatch(t *testing.T) {
	re, err := labels.NewFastRegexMatcher("foo.*")
	require.NoError(t, err)
	col := binaryColumn()
	keep := regexKeep(col, re)
	require.False(t, keep(col, dataset.Value{}))
	require.False(t, keep(col, dataset.Int64Value(1)))
}
