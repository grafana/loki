package logs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestGetPredicateSelectivity(t *testing.T) {
	tests := []struct {
		name      string
		predicate dataset.Predicate
		want      selectivityScore
	}{
		{
			name: "Equal on low cardinality column should be less selective",
			predicate: dataset.EqualPredicate{
				Column: (&testColumn{
					rowCount:    1000,
					valueCount:  1000,
					cardinality: 10,
					min:         0,
					max:         100,
				}).ToMemColumn(t),
				Value: dataset.Int64Value(50),
			},
			want: selectivityScore(0.1), // expects ~100 matches out of 1000 rows
		},
		{
			name: "Equal on high cardinality column should be more selective",
			predicate: dataset.EqualPredicate{
				Column: (&testColumn{
					rowCount:    1000,
					valueCount:  1000,
					cardinality: 1000,
					min:         0,
					max:         1000,
				}).ToMemColumn(t),
				Value: dataset.Int64Value(50),
			},
			want: selectivityScore(0.001), // expects ~1 matches out of 1000 rows
		},
		{
			name: "Equal with sparse data should have higher selectivity",
			predicate: dataset.EqualPredicate{
				Column: (&testColumn{
					rowCount:    1000,
					valueCount:  50,
					cardinality: 10, // 5 matches out of 50 non-null values
					min:         0,
					max:         100,
				}).ToMemColumn(t),
				Value: dataset.Int64Value(50),
			},
			want: selectivityScore(0.005),
		},
		{
			name: "Equal with out of range value should have zero selectivity",
			predicate: dataset.EqualPredicate{
				Column: (&testColumn{
					cardinality: 10,
					min:         0,
					max:         50,
				}).ToMemColumn(t),
				Value: dataset.Int64Value(100),
			},
			want: selectivityScore(0.0),
		},
		{
			name: "GreaterThan selectivity using min/max range",
			predicate: dataset.GreaterThanPredicate{Column: (&testColumn{
				min: 0,
				max: 100,
			}).ToMemColumn(t),
				Value: dataset.Int64Value(70),
			},
			want: selectivityScore(0.3),
		},
		{
			name: "GreaterThan with operand greater than max value should have zero selectivity",
			predicate: dataset.GreaterThanPredicate{Column: (&testColumn{
				min: 0,
				max: 50,
			}).ToMemColumn(t),
				Value: dataset.Int64Value(51),
			},
			want: selectivityScore(0.0),
		},
		{
			name: "GreaterThan with operand less than min value should match all rows",
			predicate: dataset.GreaterThanPredicate{Column: (&testColumn{
				min: 10,
				max: 50,
			}).ToMemColumn(t),
				Value: dataset.Int64Value(0),
			},
			want: selectivityScore(1.0),
		},
		{
			name: "LessThan selectivity using min/max range",
			predicate: dataset.LessThanPredicate{Column: (&testColumn{
				min: 0,
				max: 100,
			}).ToMemColumn(t),
				Value: dataset.Int64Value(70),
			},
			want: selectivityScore(0.7),
		},
		{
			name: "LessThan with operand less than min value should have zero selectivity",
			predicate: dataset.LessThanPredicate{Column: (&testColumn{
				min: 20,
				max: 50,
			}).ToMemColumn(t),
				Value: dataset.Int64Value(10),
			},
			want: selectivityScore(0.0),
		},
		{
			name: "LessThan with operand greater than max value should match all rows",
			predicate: dataset.LessThanPredicate{Column: (&testColumn{
				min: 0,
				max: 50,
			}).ToMemColumn(t),
				Value: dataset.Int64Value(60),
			},
			want: selectivityScore(1.0),
		},
		{
			name: "Not should invert the selectivity",
			predicate: dataset.NotPredicate{
				Inner: dataset.EqualPredicate{
					Column: (&testColumn{
						rowCount:    1000,
						valueCount:  1000,
						cardinality: 10, // 100 matches out of 1000 rows
						min:         0,
						max:         1000,
					}).ToMemColumn(t),
					Value: dataset.Int64Value(50),
				},
			},
			want: selectivityScore(0.9), // 1 - 0.1
		},
		{
			name: "In should add up selectivity for valid values",
			predicate: dataset.InPredicate{
				Column: (&testColumn{
					rowCount:    1000,
					valueCount:  1000,
					cardinality: 10, // 100 matches out of 1000 rows
					min:         25,
					max:         75,
				}).ToMemColumn(t),
				ValuesMap: map[interface{}]dataset.Value{
					20: dataset.Int64Value(20),
					50: dataset.Int64Value(50),
					60: dataset.Int64Value(60),
					80: dataset.Int64Value(80),
				}, // 2 values in range. ~200 matching rows
			},
			want: selectivityScore(0.2), // 0.1 + 0.1
		},
		{
			name: "And should take the minimum selectivity of the two predicates",
			predicate: dataset.AndPredicate{
				Left: dataset.EqualPredicate{
					Column: (&testColumn{
						rowCount:    1000,
						valueCount:  1000,
						cardinality: 10, // 100 matches out of 1000 rows
						min:         0,
						max:         1000,
					}).ToMemColumn(t),
					Value: dataset.Int64Value(50),
				},
				Right: dataset.EqualPredicate{
					Column: (&testColumn{
						rowCount:    1000,
						valueCount:  1000,
						cardinality: 20, // 50 matches out of 1000 rows
						min:         0,
						max:         1000,
					}).ToMemColumn(t),
					Value: dataset.Int64Value(50),
				},
			},
			want: selectivityScore(0.05),
		},
		{
			name: "Or should add up selectivity for valid values",
			predicate: dataset.OrPredicate{
				Left: dataset.EqualPredicate{
					Column: (&testColumn{
						rowCount:    1000,
						valueCount:  1000,
						cardinality: 10, // 100 matches out of 1000 rows
						min:         0,
						max:         1000,
					}).ToMemColumn(t),
					Value: dataset.Int64Value(50),
				},
				Right: dataset.EqualPredicate{
					Column: (&testColumn{
						rowCount:    1000,
						valueCount:  1000,
						cardinality: 20, // 50 matches out of 1000 rows
						min:         0,
						max:         1000,
					}).ToMemColumn(t),
					Value: dataset.Int64Value(50),
				},
			},
			want: selectivityScore(0.15),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPredicateSelectivity(tt.predicate)
			require.InDelta(t, float64(got), float64(tt.want), 1e-10)
		})
	}
}

func TestRowEvaluationCost(t *testing.T) {
	uniqueColumn := (&testColumn{
		size: 111,
	}).ToMemColumn(t)

	tests := []struct {
		name      string
		predicate dataset.Predicate
		want      int64
	}{
		{
			name: "Cost of a single column",
			predicate: dataset.EqualPredicate{
				Column: (&testColumn{
					size: 100,
				}).ToMemColumn(t),
			},
			want: 100,
		},
		{
			name: "Cost of reading multiple columns",
			predicate: dataset.AndPredicate{
				Left: dataset.EqualPredicate{
					Column: (&testColumn{
						size: 100,
					}).ToMemColumn(t),
				},
				Right: dataset.OrPredicate{
					Left: dataset.EqualPredicate{
						Column: (&testColumn{
							size: 25,
						}).ToMemColumn(t),
					},
					Right: dataset.EqualPredicate{
						Column: (&testColumn{
							size: 13,
						}).ToMemColumn(t),
					},
				},
			},
			want: 100 + 25 + 13,
		},
		{
			name: "Cost of reading multiple columns with one repeated column",
			predicate: dataset.OrPredicate{
				Left: dataset.EqualPredicate{
					Column: (&testColumn{
						size: 100,
					}).ToMemColumn(t),
				},
				Right: dataset.AndPredicate{
					Left: dataset.EqualPredicate{
						Column: uniqueColumn,
					},
					Right: dataset.EqualPredicate{
						Column: uniqueColumn,
					},
				},
			},
			want: 100 + 111,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRowEvaluationCost(tt.predicate)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOrderPredicates(t *testing.T) {
	equalCol1 := (&testColumn{
		rowCount:    1000,
		valueCount:  1000,
		cardinality: 10,
		size:        128,
		min:         0,
		max:         100,
	}).ToMemColumn(t)

	equalCol2 := (&testColumn{
		rowCount:    1000,
		valueCount:  1000,
		cardinality: 10,
		size:        1024,
		min:         0,
		max:         100,
	}).ToMemColumn(t)

	rangeCol := (&testColumn{
		rowCount:   1000,
		valueCount: 1000,
		size:       128,
		min:        0,
		max:        100,
	}).ToMemColumn(t)

	// Pre-define all predicates
	equalPred1 := dataset.EqualPredicate{Column: equalCol1, Value: dataset.Int64Value(50)}      // selectivity 0.1, cost 128
	equalPred2 := dataset.EqualPredicate{Column: equalCol2, Value: dataset.Int64Value(50)}      // selectivity 0.1, cost 1024
	rangePred := dataset.GreaterThanPredicate{Column: rangeCol, Value: dataset.Int64Value(50)}  // selectivity 0.5, cost 128
	zeroSelectPred := dataset.EqualPredicate{Column: equalCol1, Value: dataset.Int64Value(200)} // selectivity 0.0
	andPred := dataset.AndPredicate{
		Left:  equalPred1,
		Right: equalPred2,
	} // selectivity 0.1, cost 1152

	tests := []struct {
		name       string
		predicates []dataset.Predicate
		want       []dataset.Predicate
	}{
		{
			name:       "Order by selectivity - equality before range",
			predicates: []dataset.Predicate{rangePred, equalPred1},
			want:       []dataset.Predicate{equalPred1, rangePred},
		},
		{
			name:       "Order by cost when selectivity is similar",
			predicates: []dataset.Predicate{equalPred2, equalPred1},
			want:       []dataset.Predicate{equalPred1, equalPred2},
		},
		{
			name:       "Zero selectivity predicates come first",
			predicates: []dataset.Predicate{equalPred1, zeroSelectPred},
			want:       []dataset.Predicate{zeroSelectPred, equalPred1},
		},
		{
			name:       "Order by selectivity - equality before AND",
			predicates: []dataset.Predicate{andPred, equalPred2},
			want:       []dataset.Predicate{equalPred2, andPred},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orderPredicates(tt.predicates)
			require.Equal(t, tt.want, got)
		})
	}
}

func makeTestInt64Value(t *testing.T, v int64) []byte {
	t.Helper()
	b, err := dataset.Int64Value(v).MarshalBinary()
	if err != nil {
		t.Fatalf("failed to marshal int64 value %d: %v", v, err)
	}
	return b
}

type testColumn struct {
	name        string
	rowCount    int
	valueCount  int
	cardinality int
	size        int
	min         int64
	max         int64
}

func (c *testColumn) ToMemColumn(t *testing.T) *dataset.MemColumn {
	return &dataset.MemColumn{
		Info: dataset.ColumnInfo{
			Name:             c.name,
			RowsCount:        c.rowCount,
			ValuesCount:      c.valueCount,
			UncompressedSize: c.size,
			Statistics: &datasetmd.Statistics{
				CardinalityCount: uint64(c.cardinality),
				MinValue:         makeTestInt64Value(t, c.min),
				MaxValue:         makeTestInt64Value(t, c.max),
			},
		},
	}
}
