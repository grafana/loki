package dataobj

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
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
			result := matchStreamsPredicate(tt.pred, tt.stream)
			require.Equal(t, tt.expected, result, "matchStreamsPredicate returned unexpected result")
		})
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
			result := matchTimestamp(tt.pred, tt.ts)
			require.Equal(t, tt.expected, result, "matchTimestamp returned unexpected result")
		})
	}
}
