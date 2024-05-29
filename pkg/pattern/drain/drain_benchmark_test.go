package drain

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkDrain_TrainExtractsPatterns(b *testing.B) {
	tests := []struct {
		name      string
		drain     *Drain
		inputFile string
	}{
		{
			name:      `Patterns for agent logfmt logs`,
			inputFile: `testdata/agent-logfmt.txt`,
		},
		{
			name:      `Patterns for ingester logfmt logs`,
			inputFile: `testdata/ingester-logfmt.txt`,
		},
		{
			name:      `Patterns for Drone json logs`,
			inputFile: `testdata/drone-json.txt`,
		},
		{
			name:      "Patterns for distributor logfmt logs",
			inputFile: "testdata/distributor-logfmt.txt",
		},
		{
			name:      "Patterns for journald logs",
			inputFile: "testdata/journald.txt",
		},
		{
			name:      "Patterns for kafka logs",
			inputFile: "testdata/kafka.txt",
		},
		{
			name:      "Patterns for kubernetes logs",
			inputFile: "testdata/kubernetes.txt",
		},
		{
			name:      "Patterns for vault logs",
			inputFile: "testdata/vault.txt",
		},
		{
			name:      "Patterns for calico logs",
			inputFile: "testdata/calico.txt",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			file, err := os.Open(tt.inputFile)
			require.NoError(b, err)
			defer file.Close()

			scanner := bufio.NewScanner(file)
			var lines []string
			for scanner.Scan() {
				line := scanner.Text()
				lines = append(lines, line)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, line := range lines {
					drain := New(DefaultConfig(), nil)
					drain.Train(line, 0)
				}
			}
		})
	}
}
