package dataobjread

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// benchSink keeps the benchmark loop from being optimised away.
var benchSink int

func BenchmarkLogReader(b *testing.B) {
	const (
		streamCount      = 8
		entriesPerStream = 5000
	)

	levels := []string{"info", "error", "warn"}
	environments := []string{"prod", "dev"}

	streams := make([]logproto.Stream, 0, streamCount)
	for s := range streamCount {
		entries := make([]push.Entry, 0, entriesPerStream)
		for i := range entriesPerStream {
			metadata := []string{"trace_id", fmt.Sprintf("trace-%d", i%97)}
			if i%3 == 0 {
				metadata = append(metadata, "env", environments[i%2])
			}
			line := fmt.Sprintf("ts=%d level=%s method=GET path=/api/v1/items/%d status=%d duration=%dms", i, levels[i%3], i%50, 200+(i%5)*100, i%900)
			entries = append(entries, entry(b, 1+i%90, line, metadata...))
		}
		streams = append(streams, logproto.Stream{Labels: fmt.Sprintf(`{app="a%d"}`, s), Entries: entries})
	}

	// A range aggregation without an outer vector aggregation keeps every metadata key as an
	// output label, so the read returns all metadata. A sum keeps only the labels it groups by,
	// so the read returns only those metadata keys.
	cases := []struct {
		name  string
		query string
	}{
		{"sum with no line and no metadata", `sum(count_over_time({app=~".+"}[1m]))`},
		{"sum by one metadata key", `sum by (trace_id) (count_over_time({app=~".+"}[1m]))`},
		{"sum with a pushed line filter", `sum(count_over_time({app=~".+"} |= "level=error" [1m]))`},
		{"all metadata and no line", `count_over_time({app=~".+"}[1m])`},
		{"parsed line and all metadata", `count_over_time({app=~".+"} | logfmt [1m])`},
	}

	metrics := NewMetrics(nil)
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			fixture := newReaderFixture(b, tc.query, streams...)

			b.ReportAllocs()
			b.ResetTimer()

			var rows int
			for range b.N {
				reader := NewLogReader(b.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)

				rows = 0
				for reader.Next() {
					record := reader.At()
					benchSink += len(record.line) + record.metadata.Len() + int(record.timestamp&1)
					rows++
				}
				if err := reader.Err(); err != nil {
					b.Fatal(err)
				}
				if err := reader.Close(); err != nil {
					b.Fatal(err)
				}
				if rows == 0 {
					b.Fatal("the read returned no row")
				}
			}
			b.ReportMetric(float64(rows), "rows/op")
		})
	}
}
