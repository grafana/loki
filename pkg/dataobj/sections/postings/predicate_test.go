package postings

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/scalar"
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

func TestShardBucketRangePredicate(t *testing.T) {
	minCol := &Column{Type: ColumnTypeMinShardBucket}
	maxCol := &Column{Type: ColumnTypeMaxShardBucket}

	// Keep rows whose [min,max] overlaps [from=3, to=7]: min <= 7 AND max >= 3, expressed with the strict
	// operators the postings set provides as NOT(min > 7) AND NOT(max < 3).
	p := shardBucketRangePredicate(minCol, maxCol, 3, 7)

	and, ok := p.(AndPredicate)
	require.True(t, ok, "top-level predicate is AND")

	// Left branch: NOT(min > to).
	leftNot, ok := and.Left.(NotPredicate)
	require.True(t, ok, "left branch is NOT")
	gt, ok := leftNot.Inner.(GreaterThanPredicate)
	require.True(t, ok, "left inner is GreaterThan")
	require.Same(t, minCol, gt.Column, "the > branch checks the min column")
	require.Equal(t, scalar.NewInt64Scalar(7), gt.Value, "min compared against to")

	// Right branch: NOT(max < from).
	rightNot, ok := and.Right.(NotPredicate)
	require.True(t, ok, "right branch is NOT")
	lt, ok := rightNot.Inner.(LessThanPredicate)
	require.True(t, ok, "right inner is LessThan")
	require.Same(t, maxCol, lt.Column, "the < branch checks the max column")
	require.Equal(t, scalar.NewInt64Scalar(3), lt.Value, "max compared against from")
}
