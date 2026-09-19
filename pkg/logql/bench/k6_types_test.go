package bench

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestConvertTestCaseToK6(t *testing.T) {
	tests := []struct {
		name     string
		tc       TestCase
		tenantID int
		length   time.Duration
		buffer   time.Duration
		expected K6TestCase
	}{
		{
			name: "log query backward",
			tc: TestCase{
				Query:     `{service_name="loki"}`,
				Direction: logproto.BACKWARD,
				Source:    "fast/basic.yaml:3",
				QueryDesc: "Basic selector",
			},
			tenantID: 29,
			length:   24 * time.Hour,
			buffer:   4 * time.Hour,
			expected: K6TestCase{
				Name:      "fast/basic.yaml:3 - Basic selector [BACKWARD]",
				Query:     `{service_name="loki"}`,
				TenantID:  29,
				Start:     "-28h0m",
				End:       "-4h0m",
				Step:      "",
				Direction: "backward",
				Kind:      "log",
				Source:    "fast/basic.yaml:3",
			},
		},
		{
			name: "metric query with step (no direction suffix in name)",
			tc: TestCase{
				Query:     `sum(count_over_time({service_name="loki"}[5m]))`,
				Direction: logproto.FORWARD,
				Step:      time.Minute,
				Source:    "exhaustive/agg.yaml:12",
				QueryDesc: "Sum count_over_time",
			},
			tenantID: 29,
			length:   24 * time.Hour,
			buffer:   4 * time.Hour,
			expected: K6TestCase{
				Name:      "exhaustive/agg.yaml:12 - Sum count_over_time",
				Query:     `sum(count_over_time({service_name="loki"}[5m]))`,
				TenantID:  29,
				Start:     "-28h0m",
				End:       "-4h0m",
				Step:      "1m",
				Direction: "forward",
				Kind:      "metric",
				Source:    "exhaustive/agg.yaml:12",
			},
		},
		{
			name: "log query forward",
			tc: TestCase{
				Query:     `{service_name="loki"} |= "error"`,
				Direction: logproto.FORWARD,
				Source:    "fast/filters.yaml:7",
				QueryDesc: "Line filter",
			},
			tenantID: 42,
			length:   1 * time.Hour,
			buffer:   2 * time.Hour,
			expected: K6TestCase{
				Name:      "fast/filters.yaml:7 - Line filter [FORWARD]",
				Query:     `{service_name="loki"} |= "error"`,
				TenantID:  42,
				Start:     "-3h0m",
				End:       "-2h0m",
				Step:      "",
				Direction: "forward",
				Kind:      "log",
				Source:    "fast/filters.yaml:7",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertTestCaseToK6(tt.tc, tt.tenantID, tt.length, tt.buffer)
			require.Equal(t, tt.expected, result)
		})
	}
}
