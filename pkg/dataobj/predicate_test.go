package dataobj

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
)

type fakeColumn struct{ dataset.Column }

var (
	fakeMinColumn = &fakeColumn{}
	fakeMaxColumn = &fakeColumn{}
)

func TestMatchStreamsTimeRangePredicate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		stream   streams.Stream
		pred     Predicate
		expected bool
	}{
		{
			name: "stream fully inside range inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream fully inside range exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream overlaps start inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream overlaps start exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream overlaps end inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream overlaps end exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream encompasses range inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream encompasses range exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(-1 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream before range inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(-2 * time.Hour),
				MaxTimestamp: now.Add(-1 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "stream after range inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(4 * time.Hour),
				MaxTimestamp: now.Add(5 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "stream exactly at start inclusive",
			stream: streams.Stream{
				MinTimestamp: now,
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream exactly at start exclusive",
			stream: streams.Stream{
				MinTimestamp: now,
				MaxTimestamp: now.Add(1 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream exactly at end inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(3 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream exactly at end exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(2 * time.Hour),
				MaxTimestamp: now.Add(3 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now,
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   false,
			},
			expected: true,
		},
		{
			name: "stream end at start inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: true,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream end at start exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(1 * time.Hour),
				MaxTimestamp: now.Add(2 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: false,
		},
		{
			name: "stream start at end inclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(3 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(3 * time.Hour),
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: true,
		},
		{
			name: "stream start at end exclusive",
			stream: streams.Stream{
				MinTimestamp: now.Add(3 * time.Hour),
				MaxTimestamp: now.Add(4 * time.Hour),
			},
			pred: TimeRangePredicate[StreamsPredicate]{
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
			predicate := convertStreamsTimePredicate(tt.pred.(TimeRangePredicate[StreamsPredicate]), fakeMinColumn, fakeMaxColumn)
			result := evaluateStreamsPredicate(predicate, tt.stream)
			require.Equal(t, tt.expected, result, "matchStreamsPredicate returned unexpected result")
		})
	}
}

func evaluateStreamsPredicate(p dataset.Predicate, s streams.Stream) bool {
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

func TestMatchTimestamp(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		ts       time.Time
		pred     TimeRangePredicate[LogsPredicate]
		expected bool
	}{
		{
			name: "timestamp inside range inclusive",
			ts:   now.Add(1 * time.Hour),
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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
			pred: TimeRangePredicate[LogsPredicate]{
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

func TestPredicateString(t *testing.T) {
	// Test time values
	startTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	testCases := []struct {
		name     string
		pred     Predicate
		expected string
	}{
		{
			name:     "LabelMatcherPredicate",
			pred:     LabelMatcherPredicate{Name: "app", Value: "frontend"},
			expected: "Label(app=frontend)",
		},
		{
			name:     "LabelFilterPredicate",
			pred:     LabelFilterPredicate{Name: "env", Desc: `env=~"(prod|dev)"`},
			expected: `LabelFilter(env, description="env=~\"(prod|dev)\"")`,
		},
		{
			name:     "MetadataMatcherPredicate",
			pred:     MetadataMatcherPredicate{Key: "trace_id", Value: "abc123"},
			expected: "Metadata(trace_id=abc123)",
		},
		{
			name:     "MetadataFilterPredicate",
			pred:     MetadataFilterPredicate{Key: "span_id", Desc: `span_id=~"123"`},
			expected: `MetadataFilter(span_id, description="span_id=~\"123\"")`,
		},
		{
			name:     "LogMessageFilterPredicate",
			pred:     LogMessageFilterPredicate{Desc: `line=~"error"`},
			expected: `LogMessageFilter(description="line=~\"error\"")`,
		},
		{
			name: "TimeRangePredicate - inclusive start, exclusive end",
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    startTime,
				EndTime:      endTime,
				IncludeStart: true,
				IncludeEnd:   false,
			},
			expected: "TimeRange[2023-01-01T00:00:00Z, 2023-01-02T00:00:00Z)",
		},
		{
			name: "TimeRangePredicate - exclusive start, inclusive end",
			pred: TimeRangePredicate[StreamsPredicate]{
				StartTime:    startTime,
				EndTime:      endTime,
				IncludeStart: false,
				IncludeEnd:   true,
			},
			expected: "TimeRange(2023-01-01T00:00:00Z, 2023-01-02T00:00:00Z]",
		},
		{
			name: "NotPredicate",
			pred: NotPredicate[StreamsPredicate]{
				Inner: LabelMatcherPredicate{Name: "app", Value: "frontend"},
			},
			expected: "NOT(Label(app=frontend))",
		},
		{
			name: "AndPredicate",
			pred: AndPredicate[StreamsPredicate]{
				Left:  LabelMatcherPredicate{Name: "app", Value: "frontend"},
				Right: LabelMatcherPredicate{Name: "env", Value: "prod"},
			},
			expected: "(Label(app=frontend) AND Label(env=prod))",
		},
		{
			name: "OrPredicate",
			pred: OrPredicate[StreamsPredicate]{
				Left:  LabelMatcherPredicate{Name: "app", Value: "frontend"},
				Right: LabelMatcherPredicate{Name: "app", Value: "backend"},
			},
			expected: "(Label(app=frontend) OR Label(app=backend))",
		},
		{
			name: "Complex nested predicate",
			pred: OrPredicate[StreamsPredicate]{
				Left: AndPredicate[StreamsPredicate]{
					Left:  LabelMatcherPredicate{Name: "app", Value: "service"},
					Right: LabelMatcherPredicate{Name: "env", Value: "prod"},
				},
				Right: AndPredicate[StreamsPredicate]{
					Left: LabelFilterPredicate{Name: "region", Desc: `region=~"us-west"`},
					Right: TimeRangePredicate[StreamsPredicate]{
						StartTime:    startTime,
						EndTime:      endTime,
						IncludeStart: true,
						IncludeEnd:   false,
					},
				},
			},
			expected: `((Label(app=service) AND Label(env=prod)) OR (LabelFilter(region, description="region=~\"us-west\"") AND TimeRange[2023-01-01T00:00:00Z, 2023-01-02T00:00:00Z)))`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.pred.String()
			if result != tc.expected {
				t.Errorf("Expected: %s, Got: %s", tc.expected, result)
			}
		})
	}
}
