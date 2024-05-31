package wal

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// BenchmarkWriteChunk benchmarks the writeChunk function.
func BenchmarkWriteChunk(b *testing.B) {
	// Generate sample log entries
	entries := generateLogEntries(1000)
	// Reset the buffer for each iteration
	buf := bytes.NewBuffer(make([]byte, 0, 5<<20))

	b.ReportAllocs()
	b.ResetTimer()

	// Run the benchmark
	for n := 0; n < b.N; n++ {
		buf.Reset()
		// Call the writeChunk function
		_, err := writeChunk(buf, entries, EncodingSnappy)
		if err != nil {
			b.Fatalf("writeChunk failed: %v", err)
		}
	}
}

// generateLogEntries generates a slice of logproto.Entry with the given count.
func generateLogEntries(count int) []*logproto.Entry {
	entries := make([]*logproto.Entry, count)
	for i := 0; i < count; i++ {
		entries[i] = &logproto.Entry{
			Timestamp: time.Now(),
			Line:      "This is a sample log entry.",
		}
	}
	return entries
}
