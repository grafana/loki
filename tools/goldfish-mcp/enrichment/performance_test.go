package enrichment

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

func TestCalculatePerformanceComparison(t *testing.T) {
	tests := []struct {
		name      string
		cellA     goldfish.QueryStats
		cellB     goldfish.QueryStats
		expected  PerformanceComparison
		tolerance float64 // for floating point comparison
	}{
		{
			name: "basic comparison with all values",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     100,
				BytesProcessed: 1000,
				LinesProcessed: 500,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     200,
				BytesProcessed: 2000,
				LinesProcessed: 1000,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     100,
				CellBExecTimeMs:     200,
				ExecTimeDeltaMs:     100,
				ExecTimeRatio:       2.0,
				BytesProcessedRatio: 2.0,
				LinesProcessedRatio: 2.0,
			},
			tolerance: 0.001,
		},
		{
			name: "cell B faster than cell A",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     200,
				BytesProcessed: 2000,
				LinesProcessed: 1000,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     100,
				BytesProcessed: 1000,
				LinesProcessed: 500,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     200,
				CellBExecTimeMs:     100,
				ExecTimeDeltaMs:     -100,
				ExecTimeRatio:       0.5,
				BytesProcessedRatio: 0.5,
				LinesProcessedRatio: 0.5,
			},
			tolerance: 0.001,
		},
		{
			name: "zero exec time in cell A - avoid division by zero",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     0,
				BytesProcessed: 0,
				LinesProcessed: 0,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     100,
				BytesProcessed: 1000,
				LinesProcessed: 500,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     0,
				CellBExecTimeMs:     100,
				ExecTimeDeltaMs:     100,
				ExecTimeRatio:       0,
				BytesProcessedRatio: 0,
				LinesProcessedRatio: 0,
			},
			tolerance: 0.001,
		},
		{
			name: "both cells have zero values",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     0,
				BytesProcessed: 0,
				LinesProcessed: 0,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     0,
				BytesProcessed: 0,
				LinesProcessed: 0,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     0,
				CellBExecTimeMs:     0,
				ExecTimeDeltaMs:     0,
				ExecTimeRatio:       0,
				BytesProcessedRatio: 0,
				LinesProcessedRatio: 0,
			},
			tolerance: 0.001,
		},
		{
			name: "fractional ratios",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     300,
				BytesProcessed: 1500,
				LinesProcessed: 750,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     450,
				BytesProcessed: 2250,
				LinesProcessed: 1125,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     300,
				CellBExecTimeMs:     450,
				ExecTimeDeltaMs:     150,
				ExecTimeRatio:       1.5,
				BytesProcessedRatio: 1.5,
				LinesProcessedRatio: 1.5,
			},
			tolerance: 0.001,
		},
		{
			name: "only bytes processed differs",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     100,
				BytesProcessed: 1000,
				LinesProcessed: 500,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     100,
				BytesProcessed: 3000,
				LinesProcessed: 500,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     100,
				CellBExecTimeMs:     100,
				ExecTimeDeltaMs:     0,
				ExecTimeRatio:       1.0,
				BytesProcessedRatio: 3.0,
				LinesProcessedRatio: 1.0,
			},
			tolerance: 0.001,
		},
		{
			name: "cell A has bytes but cell B has zero bytes",
			cellA: goldfish.QueryStats{
				ExecTimeMs:     100,
				BytesProcessed: 1000,
				LinesProcessed: 500,
			},
			cellB: goldfish.QueryStats{
				ExecTimeMs:     50,
				BytesProcessed: 0,
				LinesProcessed: 0,
			},
			expected: PerformanceComparison{
				CellAExecTimeMs:     100,
				CellBExecTimeMs:     50,
				ExecTimeDeltaMs:     -50,
				ExecTimeRatio:       0.5,
				BytesProcessedRatio: 0,
				LinesProcessedRatio: 0,
			},
			tolerance: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculatePerformanceComparison(tt.cellA, tt.cellB)

			// Check integer fields
			if result.CellAExecTimeMs != tt.expected.CellAExecTimeMs {
				t.Errorf("CellAExecTimeMs: got %d, want %d", result.CellAExecTimeMs, tt.expected.CellAExecTimeMs)
			}
			if result.CellBExecTimeMs != tt.expected.CellBExecTimeMs {
				t.Errorf("CellBExecTimeMs: got %d, want %d", result.CellBExecTimeMs, tt.expected.CellBExecTimeMs)
			}
			if result.ExecTimeDeltaMs != tt.expected.ExecTimeDeltaMs {
				t.Errorf("ExecTimeDeltaMs: got %d, want %d", result.ExecTimeDeltaMs, tt.expected.ExecTimeDeltaMs)
			}

			// Check float fields with tolerance
			if !floatEqual(result.ExecTimeRatio, tt.expected.ExecTimeRatio, tt.tolerance) {
				t.Errorf("ExecTimeRatio: got %f, want %f", result.ExecTimeRatio, tt.expected.ExecTimeRatio)
			}
			if !floatEqual(result.BytesProcessedRatio, tt.expected.BytesProcessedRatio, tt.tolerance) {
				t.Errorf("BytesProcessedRatio: got %f, want %f", result.BytesProcessedRatio, tt.expected.BytesProcessedRatio)
			}
			if !floatEqual(result.LinesProcessedRatio, tt.expected.LinesProcessedRatio, tt.tolerance) {
				t.Errorf("LinesProcessedRatio: got %f, want %f", result.LinesProcessedRatio, tt.expected.LinesProcessedRatio)
			}
		})
	}
}

// floatEqual compares two floats with tolerance
func floatEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < tolerance
}
