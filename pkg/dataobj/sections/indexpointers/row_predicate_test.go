package indexpointers

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

type fakeColumn struct{ dataset.Column }

var (
	fakeMinTimestampColumn = &fakeColumn{
		Column: &dataset.MemColumn{
			Info: dataset.ColumnInfo{
				Name: "min_timestamp",
			},
		},
	}
	fakeMaxTimestampColumn = &fakeColumn{
		Column: &dataset.MemColumn{
			Info: dataset.ColumnInfo{
				Name: "max_timestamp",
			},
		},
	}
)

func TestWithInTimeRangePredicate(t *testing.T) {
	tests := []struct {
		name     string
		pointer  IndexPointer
		pred     RowPredicate
		expected bool
	}{
		{
			name: "in time range",
			pointer: IndexPointer{
				StartTs: unixTime(10),
				EndTs:   unixTime(20),
			},
			pred: TimeRangeRowPredicate{
				Start: unixTime(10),
				End:   unixTime(20),
			},
			expected: true,
		},
		{
			name: "not in time range",
			pointer: IndexPointer{
				StartTs: unixTime(10),
				EndTs:   unixTime(20),
			},
			pred: TimeRangeRowPredicate{
				Start: unixTime(21),
				End:   unixTime(30),
			},
			expected: false,
		},
		{
			name: "min timestamp too early",
			pointer: IndexPointer{
				StartTs: unixTime(5),
				EndTs:   unixTime(20),
			},
			pred: TimeRangeRowPredicate{
				Start: unixTime(10),
				End:   unixTime(20),
			},
			expected: false,
		},
		{
			name: "max timestamp too late",
			pointer: IndexPointer{
				StartTs: unixTime(10),
				EndTs:   unixTime(25),
			},
			pred: TimeRangeRowPredicate{
				Start: unixTime(10),
				End:   unixTime(20),
			},
			expected: false,
		},
		{
			name: "within time range",
			pointer: IndexPointer{
				StartTs: unixTime(15),
				EndTs:   unixTime(25),
			},
			pred: TimeRangeRowPredicate{
				Start: unixTime(5),
				End:   unixTime(25),
			},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := translateIndexPointersPredicate(tt.pred.(TimeRangeRowPredicate), []dataset.Column{fakeMinTimestampColumn, fakeMaxTimestampColumn})
			actual := evaluateTimeRangePredicate(predicate, tt.pointer)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func evaluateTimeRangePredicate(p dataset.Predicate, s IndexPointer) bool {
	switch p := p.(type) {
	case dataset.AndPredicate:
		return evaluateStartPredicate(p.Left, s) && evaluateEndPredicate(p.Right, s)
	default:
		panic(fmt.Sprintf("unexpected predicate type %T", p))
	}
}

func evaluateStartPredicate(p dataset.Predicate, s IndexPointer) bool {
	switch p := p.(type) {
	case dataset.NotPredicate:
		return !evaluateStartPredicate(p.Inner, s)
	case dataset.LessThanPredicate:
		return s.StartTs.UnixNano() < p.Value.Int64()

	default:
		panic(fmt.Sprintf("unexpected row predicate type %T", p))
	}
}

func evaluateEndPredicate(p dataset.Predicate, s IndexPointer) bool {
	switch p := p.(type) {
	case dataset.NotPredicate:
		return !evaluateEndPredicate(p.Inner, s)
	case dataset.GreaterThanPredicate:
		return s.EndTs.UnixNano() > p.Value.Int64()

	default:
		panic(fmt.Sprintf("unexpected row predicate type %T", p))
	}
}
