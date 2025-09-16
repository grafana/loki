package logs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func TestMatchTimestamp(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		ts       time.Time
		pred     TimeRangeRowPredicate
		expected bool
	}{
		{
			name: "timestamp inside range inclusive",
			ts:   now.Add(1 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "timestamp inside range exclusive",
			ts:   now.Add(1 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "timestamp before range inclusive",
			ts:   now.Add(-1 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "timestamp after range inclusive",
			ts:   now.Add(4 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   false,
			},
			expected: false,
		},
		{
			name: "timestamp exactly at start inclusive",
			ts:   now,
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "timestamp exactly at start exclusive",
			ts:   now,
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: false,
		},
		{
			name: "timestamp exactly at end inclusive",
			ts:   now.Add(3 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "timestamp exactly at end exclusive",
			ts:   now.Add(3 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: false,
		},
		{
			name: "timestamp exactly at both bounds inclusive",
			ts:   now,
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now,
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "timestamp exactly at both bounds exclusive",
			ts:   now,
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now,
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: false,
		},
		{
			name: "timestamp exactly at start with mixed bounds",
			ts:   now,
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "timestamp exactly at end with mixed bounds",
			ts:   now.Add(3 * time.Hour),
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := convertLogsTimePredicate(tt.pred, nil)
			result := evaluateRecordPredicate(predicate, tt.ts)
			require.Equal(t, tt.expected, result, "matchTimestamp returned unexpected result")
		})
	}
}

func evaluateRecordPredicate(p dataset.Predicate, ts time.Time) bool {
	switch p := p.(type) {
	case dataset.AndPredicate:
		return evaluateRecordPredicate(p.Left, ts) && evaluateRecordPredicate(p.Right, ts)
	case dataset.OrPredicate:
		return evaluateRecordPredicate(p.Left, ts) || evaluateRecordPredicate(p.Right, ts)
	case dataset.NotPredicate:
		return !evaluateRecordPredicate(p.Inner, ts)
	case dataset.GreaterThanPredicate:
		return ts.After(time.Unix(0, p.Value.Int64()))
	case dataset.LessThanPredicate:
		return ts.Before(time.Unix(0, p.Value.Int64()))
	case dataset.EqualPredicate:
		return ts.Equal(time.Unix(0, p.Value.Int64()))

	default:
		panic("unexpected predicate")
	}
}
