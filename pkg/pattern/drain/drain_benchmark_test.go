package drain

import (
	"bufio"
	"os"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func BenchmarkDrain_TrainExtractsPatterns(b *testing.B) {
	tests := []struct {
		inputFile string
	}{
		{inputFile: `testdata/agent-logfmt.txt`},
		{inputFile: `testdata/ingester-logfmt.txt`},
		{inputFile: `testdata/drone-json.txt`},
		{inputFile: "testdata/distributor-logfmt.txt"},
		{inputFile: "testdata/journald.txt"},
		{inputFile: "testdata/kafka.txt"},
		{inputFile: "testdata/kubernetes.txt"},
		{inputFile: "testdata/vault.txt"},
		{inputFile: "testdata/calico.txt"},
	}

	for _, tt := range tests {
		b.Run(tt.inputFile, func(b *testing.B) {
			file, err := os.Open(tt.inputFile)
			require.NoError(b, err)
			defer file.Close()

			scanner := bufio.NewScanner(file)
			var lines []string
			for scanner.Scan() {
				line := scanner.Text()
				lines = append(lines, line)
			}
			mockWriter := &mockEntryWriter{}
			drain := New("", DefaultConfig(), &fakeLimits{}, DetectLogFormat(lines[0]), mockWriter, nil)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, line := range lines {
					drain.Train(info, line, 0, labels.EmptyLabels())
				}
			}
		})
	}
}

type mockEntryWriter struct {
	mock.Mock
}

func (m *mockEntryWriter) WriteEntry(ts time.Time, entry string, lbls labels.Labels, structuredMetadata []logproto.LabelAdapter) {
	_ = m.Called(ts, entry, lbls, structuredMetadata)
}

func (m *mockEntryWriter) Stop() {
	_ = m.Called()
}
