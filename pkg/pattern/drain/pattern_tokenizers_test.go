package drain

import (
	"bufio"
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func Benchmark_logfmtTokenizer_Marshal(t *testing.B) {
	tests := []struct {
		name                 string
		tokenizeInsideQuotes bool
		input                string
		want                 []string
	}{
		{
			name:                 "distributor",
			input:                "testdata/distributor-logfmt.txt",
			tokenizeInsideQuotes: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.B) {
			a := NewLogFmtTokenizer(tt.tokenizeInsideQuotes)
			file, err := os.ReadFile(tt.input)
			require.NoError(t, err)

			scanner := bufio.NewScanner(bytes.NewReader(file))
			var lines [][]byte
			for scanner.Scan() {
				lines = append(lines, []byte(scanner.Text()))
			}

			t.ResetTimer()
			t.ReportAllocs()
			for i := 0; i < t.N; i++ {
				for _, line := range lines {
					a.Marshal(line)
				}
			}
		})
	}
}
