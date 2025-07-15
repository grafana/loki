package pointers

import (
	"testing"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

type fakeColumn struct{ dataset.Column }

var (
	fakePodColumn = &fakeColumn{
		Column: &dataset.MemColumn{
			Info: dataset.ColumnInfo{
				Name: "values_bloom_filter",
			},
		},
	}
	fakeNameColumn = &fakeColumn{
		Column: &dataset.MemColumn{
			Info: dataset.ColumnInfo{
				Name: "column_name",
			},
		},
	}
)

func TestMatchBloomExistencePredicate(t *testing.T) {

	bf := bloom.New(100, 100)
	bf.AddString("testValuePresent")
	bfBytes, err := bf.MarshalBinary()
	require.NoError(t, err)

	tests := []struct {
		name     string
		pointer  SectionPointer
		pred     RowPredicate
		expected bool
	}{
		{
			name: "bloom filter contains value",
			pointer: SectionPointer{
				Path:              "testPath1",
				ColumnName:        "pod",
				ValuesBloomFilter: bfBytes,
			},
			pred: BloomExistencePredicate{
				Name:  "pod",
				Value: "testValuePresent",
			},
			expected: true,
		},
		{
			name: "bloom filter does not contain value", // our false positive rate is very low for this test, so it should never return true
			pointer: SectionPointer{
				Path:              "testPath1",
				ColumnName:        "pod",
				ValuesBloomFilter: bfBytes,
			},
			pred: BloomExistencePredicate{
				Name:  "pod",
				Value: "testValueAbsent",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := convertBloomExistencePredicate(tt.pred.(BloomExistencePredicate), fakeNameColumn, fakePodColumn)
			result := evaluateBloomExistencePredicate(predicate, tt.pointer)
			require.Equal(t, tt.expected, result, "matchBloomExistencePredicate returned unexpected result")
		})
	}
}

func evaluateBloomExistencePredicate(p dataset.Predicate, s SectionPointer) bool {
	switch p := p.(type) {
	case dataset.AndPredicate:
		return evaluateBloomExistencePredicate(p.Left, s) && evaluateBloomExistencePredicate(p.Right, s)
	case dataset.EqualPredicate:
		return s.ColumnName == unsafeString(p.Value.ByteArray())
	case dataset.FuncPredicate:
		return p.Keep(p.Column, dataset.ByteArrayValue(s.ValuesBloomFilter))

	default:
		panic("unexpected predicate")
	}
}
