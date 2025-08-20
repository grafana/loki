package streams

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

type fakeColumn struct{ dataset.Column }

var (
	fakeMinColumn = &fakeColumn{}
	fakeMaxColumn = &fakeColumn{}
)

func TestMatchStreamsTimeRangeRowPredicate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		stream   Stream
		pred     RowPredicate
		expected bool
	}{
		{
			name: "stream fully inside range inclusive",
			stream: Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream fully inside range exclusive",
			stream: Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream overlaps start inclusive",
			stream: Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream overlaps start exclusive",
			stream: Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream overlaps end inclusive",
			stream: Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream overlaps end exclusive",
			stream: Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream encompasses range inclusive",
			stream: Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream encompasses range exclusive",
			stream: Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream before range inclusive",
			stream: Stream{
				MinTimestamp: now.Add(-2 * time.Hour),
				MaxTimestamp: now.Add(-1 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "stream after range inclusive",
			stream: Stream{
				MinTimestamp: now.Add(4 * time.Hour),
				MaxTimestamp: now.Add(5 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "stream exactly at start inclusive",
			stream: Stream{
				MinTimestamp: now,
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream exactly at start exclusive",
			stream: Stream{
				MinTimestamp: now,
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream exactly at end inclusive",
			stream: Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(3 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream exactly at end exclusive",
			stream: Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(3 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream end at start inclusive",
			stream: Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream end at start exclusive",
			stream: Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "stream start at end inclusive",
			stream: Stream{
				MinTimestamp: now.Add(3 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream start at end exclusive",
			stream: Stream{
				MinTimestamp: now.Add(3 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangeRowPredicate{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := convertStreamsTimePredicate(tt.pred.(TimeRangeRowPredicate), fakeMinColumn, fakeMaxColumn)
			result := evaluateStreamsPredicate(predicate, tt.stream)
			require.Equal(t, tt.expected, result, "matchStreamsPredicate returned unexpected result")
		})
	}
}

func evaluateStreamsPredicate(p dataset.Predicate, s Stream) bool {
	switch p := p.(type) {
	case dataset.AndPredicate:
		return evaluateStreamsPredicate(p.Left, s) && evaluateStreamsPredicate(p.Right, s)
	case dataset.OrPredicate:
		return evaluateStreamsPredicate(p.Left, s) || evaluateStreamsPredicate(p.Right, s)
	case dataset.NotPredicate:
		return !evaluateStreamsPredicate(p.Inner, s)
	case dataset.GreaterThanPredicate:
		if p.Column == fakeMinColumn {
			return s.MinTimestamp.After(time.Unix(0, p.Value.Int64()).UTC())
		} else if p.Column == fakeMaxColumn {
			return s.MaxTimestamp.After(time.Unix(0, p.Value.Int64()).UTC())
		}
		panic("unexpected column")

	case dataset.LessThanPredicate:
		if p.Column == fakeMinColumn {
			return s.MinTimestamp.Before(time.Unix(0, p.Value.Int64()).UTC())
		} else if p.Column == fakeMaxColumn {
			return s.MaxTimestamp.Before(time.Unix(0, p.Value.Int64()).UTC())
		}
		panic("unexpected column")

	default:
		panic("unexpected predicate")
	}
}
