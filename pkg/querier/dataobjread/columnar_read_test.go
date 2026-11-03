package dataobjread

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

func TestColumnarRead(t *testing.T) {
	metrics := NewMetrics(nil)

	// streams holds several streams with metadata that only some of the entries carry.
	var streams []logproto.Stream
	for s := range 6 {
		var entries []push.Entry
		for i := range 60 {
			var metadata []string
			if i%3 == 0 {
				metadata = append(metadata, "trace_id", fmt.Sprintf("trace-%d", i%7))
			}
			if i%5 == 0 {
				metadata = append(metadata, "env", []string{"prod", "dev"}[i%2])
			}
			entries = append(entries, entry(t, 1+i, fmt.Sprintf("stream=%d line=%d level=%s", s, i, []string{"info", "error", "warn"}[i%3]), metadata...))
		}
		streams = append(streams, logproto.Stream{Labels: fmt.Sprintf(`{app="a%d"}`, s), Entries: entries})
	}

	queries := map[string]string{
		"line returned":           `bytes_over_time({app=~".+"}[1m])`,
		"line filter":             `count_over_time({app=~".+"} |= "level=error" [1m])`,
		"negative line filter":    `count_over_time({app=~".+"} != "level=error" [1m])`,
		"regex line filter":       `count_over_time({app=~".+"} |~ "line=1[0-9]" [1m])`,
		"metadata filter":         `count_over_time({app=~".+"} | env="prod" [1m])`,
		"metadata negation":       `count_over_time({app=~".+"} | env!="prod" [1m])`,
		"metadata and line":       `count_over_time({app=~".+"} | trace_id="trace-3" |= "stream=2" [1m])`,
		"grouped by metadata key": `sum by (trace_id) (count_over_time({app=~".+"} | trace_id!="" [1m]))`,
	}

	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			fixture := newReaderFixture(t, query, streams...)

			read := func(columnar bool, batchSize int) []string {
				defer SetColumnarRead(SetColumnarRead(columnar))

				reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, metrics)
				var out []string
				for reader.Next() {
					rec := reader.At()
					out = append(out, fmt.Sprintf("%d|%d|%s|%s|%s", rec.streamHash, rec.timestamp, rec.streamLabels, rec.metadata, rec.line))
				}
				require.NoError(t, reader.Err())
				require.NoError(t, reader.Close())
				slices.Sort(out)
				return out
			}

			for _, batchSize := range []int{1, 7, DefaultReadBatchSize} {
				want := read(false, batchSize)
				got := read(true, batchSize)
				require.NotEmpty(t, want)
				require.Equal(t, want, got, "batch size %d", batchSize)
			}
		})
	}
}
