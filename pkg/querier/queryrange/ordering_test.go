package queryrange

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
)

// TestMultiStreamQueryRangeSorting tests the fix for issue #19133 (https://github.com/grafana/loki/issues/19133)
// When queries are split by interval and multiple streams exist,
// the merged results should be properly sorted by timestamp
func TestMultiStreamQueryRangeSorting(t *testing.T) {
	tests := []struct {
		name      string
		direction logproto.Direction
		markers   []entries
		expected  []string // expected log lines in order
	}{
		{
			name:      "backward direction with multiple time segments",
			direction: logproto.BACKWARD,
			markers: []entries{
				// First time segment (0:00-0:15)
				{
					{Timestamp: parseTime(t, "0:05"), Line: "log at 0:05"},
					{Timestamp: parseTime(t, "0:10"), Line: "log at 0:10"},
				},
				// Second time segment (0:15-0:30)
				{
					{Timestamp: parseTime(t, "0:20"), Line: "log at 0:20"},
					{Timestamp: parseTime(t, "0:25"), Line: "log at 0:25"},
				},
			},
			expected: []string{
				"log at 0:25", // newest first
				"log at 0:20",
				"log at 0:10",
				"log at 0:05",
			},
		},
		{
			name:      "forward direction with multiple time segments",
			direction: logproto.FORWARD,
			markers: []entries{
				// First time segment
				{
					{Timestamp: parseTime(t, "0:05"), Line: "log at 0:05"},
					{Timestamp: parseTime(t, "0:10"), Line: "log at 0:10"},
				},
				// Second time segment
				{
					{Timestamp: parseTime(t, "0:20"), Line: "log at 0:20"},
					{Timestamp: parseTime(t, "0:25"), Line: "log at 0:25"},
				},
			},
			expected: []string{
				"log at 0:05", // oldest first
				"log at 0:10",
				"log at 0:20",
				"log at 0:25",
			},
		},
		{
			name:      "overlapping timestamps between segments",
			direction: logproto.BACKWARD,
			markers: []entries{
				// First segment
				{
					{Timestamp: parseTime(t, "0:05"), Line: "log at 0:05"},
					{Timestamp: parseTime(t, "0:15"), Line: "log at 0:15"},
				},
				// Second segment with overlapping time
				{
					{Timestamp: parseTime(t, "0:12"), Line: "log at 0:12"},
					{Timestamp: parseTime(t, "0:18"), Line: "log at 0:18"},
				},
			},
			expected: []string{
				"log at 0:18",
				"log at 0:15",
				"log at 0:12",
				"log at 0:05",
			},
		},
		{
			name:      "extreme case: 10 segments with heavy overlap",
			direction: logproto.FORWARD,
			markers: []entries{
				{
					{Timestamp: parseTime(t, "0:01"), Line: "segment1-0:01"},
					{Timestamp: parseTime(t, "0:15"), Line: "segment1-0:15"},
					{Timestamp: parseTime(t, "0:30"), Line: "segment1-0:30"},
				},
				{
					{Timestamp: parseTime(t, "0:02"), Line: "segment2-0:02"},
					{Timestamp: parseTime(t, "0:16"), Line: "segment2-0:16"},
					{Timestamp: parseTime(t, "0:31"), Line: "segment2-0:31"},
				},
				{
					{Timestamp: parseTime(t, "0:03"), Line: "segment3-0:03"},
					{Timestamp: parseTime(t, "0:17"), Line: "segment3-0:17"},
					{Timestamp: parseTime(t, "0:32"), Line: "segment3-0:32"},
				},
				{
					{Timestamp: parseTime(t, "0:04"), Line: "segment4-0:04"},
					{Timestamp: parseTime(t, "0:18"), Line: "segment4-0:18"},
					{Timestamp: parseTime(t, "0:33"), Line: "segment4-0:33"},
				},
				{
					{Timestamp: parseTime(t, "0:05"), Line: "segment5-0:05"},
					{Timestamp: parseTime(t, "0:19"), Line: "segment5-0:19"},
					{Timestamp: parseTime(t, "0:34"), Line: "segment5-0:34"},
				},
			},
			expected: []string{
				"segment1-0:01", "segment2-0:02", "segment3-0:03", "segment4-0:04", "segment5-0:05",
				"segment1-0:15", "segment2-0:16", "segment3-0:17", "segment4-0:18", "segment5-0:19",
				"segment1-0:30", "segment2-0:31", "segment3-0:32", "segment4-0:33", "segment5-0:34",
			},
		},
		{
			name:      "chaos test: completely random timestamps across segments",
			direction: logproto.BACKWARD,
			markers: []entries{
				{
					{Timestamp: parseTime(t, "0:45"), Line: "chaos-0:45"},
					{Timestamp: parseTime(t, "0:12"), Line: "chaos-0:12"},
					{Timestamp: parseTime(t, "0:33"), Line: "chaos-0:33"},
				},
				{
					{Timestamp: parseTime(t, "0:07"), Line: "chaos-0:07"},
					{Timestamp: parseTime(t, "0:58"), Line: "chaos-0:58"},
					{Timestamp: parseTime(t, "0:21"), Line: "chaos-0:21"},
				},
				{
					{Timestamp: parseTime(t, "0:39"), Line: "chaos-0:39"},
					{Timestamp: parseTime(t, "0:03"), Line: "chaos-0:03"},
					{Timestamp: parseTime(t, "0:51"), Line: "chaos-0:51"},
				},
			},
			expected: []string{
				"chaos-0:58", "chaos-0:51", "chaos-0:45", "chaos-0:39", "chaos-0:33",
				"chaos-0:21", "chaos-0:12", "chaos-0:07", "chaos-0:03",
			},
		},
		{
			name:      "identical timestamps with different content",
			direction: logproto.FORWARD,
			markers: []entries{
				{
					{Timestamp: parseTime(t, "0:10"), Line: "marker1-same-time"},
					{Timestamp: parseTime(t, "0:10"), Line: "marker1-same-time-2"},
				},
				{
					{Timestamp: parseTime(t, "0:10"), Line: "marker2-same-time"},
					{Timestamp: parseTime(t, "0:10"), Line: "marker2-same-time-2"},
				},
			},
			expected: []string{
				"marker1-same-time", "marker1-same-time-2", 
				"marker2-same-time", "marker2-same-time-2",
			},
		},
		{
			name:      "massive scale: 1000 entries across 10 segments",
			direction: logproto.FORWARD,
			markers:   generateMassiveTestData(t, 10, 100), // 10 segments, 100 entries each
			expected:  generateMassiveExpected(10, 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bd := byDir{
				markers:   tt.markers,
				direction: tt.direction,
				labels:    `{cluster="test", app="test"}`,
			}

			result := bd.merge()
			require.Len(t, result, len(tt.expected))

			for i, entry := range result {
				require.Equal(t, tt.expected[i], entry.Line, 
					"Entry %d: expected %s, got %s", i, tt.expected[i], entry.Line)
			}

			// Verify proper sorting
			for i := 1; i < len(result); i++ {
				if tt.direction == logproto.BACKWARD {
					require.True(t, result[i-1].Timestamp.After(result[i].Timestamp) || 
						result[i-1].Timestamp.Equal(result[i].Timestamp),
						"BACKWARD: entry %d should be after entry %d", i-1, i)
				} else {
					require.True(t, result[i-1].Timestamp.Before(result[i].Timestamp) || 
						result[i-1].Timestamp.Equal(result[i].Timestamp),
						"FORWARD: entry %d should be before entry %d", i-1, i)
				}
			}
		})
	}
}

func generateMassiveTestData(t *testing.T, segments, entriesPerSegment int) []entries {
	markers := make([]entries, segments)
	
	for s := 0; s < segments; s++ {
		entries := make(entries, entriesPerSegment)
		for e := 0; e < entriesPerSegment; e++ {
			// Create interleaved timestamps across segments
			minute := (s + e*segments) % 60
			entries[e] = logproto.Entry{
				Timestamp: parseTime(t, fmt.Sprintf("0:%02d", minute)),
				Line:      fmt.Sprintf("massive-seg%d-entry%d", s, e),
			}
		}
		markers[s] = entries
	}
	
	return markers
}

func generateMassiveExpected(segments, entriesPerSegment int) []string {
	// Generate expected results in chronological order
	timeMap := make(map[int][]string)
	
	for s := 0; s < segments; s++ {
		for e := 0; e < entriesPerSegment; e++ {
			minute := (s + e*segments) % 60
			line := fmt.Sprintf("massive-seg%d-entry%d", s, e)
			timeMap[minute] = append(timeMap[minute], line)
		}
	}
	
	var result []string
	for minute := 0; minute < 60; minute++ {
		if lines, exists := timeMap[minute]; exists {
			result = append(result, lines...)
		}
	}
	
	return result
}

func parseTime(t *testing.T, timeStr string) time.Time {
	parsed, err := time.Parse("15:04", timeStr)
	require.NoError(t, err)
	
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 
		parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
}
