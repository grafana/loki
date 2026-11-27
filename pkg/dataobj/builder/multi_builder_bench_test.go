package builder

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// newBenchStream returns a stream of count entries that end at end and are
// one second apart.
func newBenchStream(count int, end time.Time) logproto.Stream {
	entries := make([]push.Entry, count)
	for i := range entries {
		entries[i] = push.Entry{
			Timestamp: end.Add(-time.Duration(count-i) * time.Second),
			Line:      fmt.Sprintf("level=info msg=\"request handled\" status=200 duration=%dms", i),
		}
	}
	return logproto.Stream{Labels: `{app="bench", cluster="a"}`, Entries: entries}
}

// ageEntries moves every n-th entry of stream two hours into the past.
func ageEntries(stream logproto.Stream, n int) logproto.Stream {
	aged := logproto.Stream{Labels: stream.Labels, Entries: slices.Clone(stream.Entries)}
	for i := 0; i < len(aged.Entries); i += n {
		aged.Entries[i].Timestamp = aged.Entries[i].Timestamp.Add(-2 * time.Hour)
	}
	return aged
}

func BenchmarkMultiObjectBuilder_Append(b *testing.B) {
	recTime := testWindow1.Add(6 * time.Hour)
	inTime := newBenchStream(100, recTime)

	for _, bc := range []struct {
		name      string
		overrides TenantOverrides
		stream    logproto.Stream
	}{
		{name: "separation off, in-time records", overrides: testOverrides{}, stream: inTime},
		{name: "separation off, 5% late lines", overrides: testOverrides{}, stream: ageEntries(inTime, 20)},
		{name: "separation on, in-time records", overrides: constThreshold(testLateThreshold), stream: inTime},
		{name: "separation on, 5% late lines", overrides: constThreshold(testLateThreshold), stream: ageEntries(inTime, 20)},
		{name: "separation on, 100% late lines", overrides: constThreshold(testLateThreshold), stream: ageEntries(inTime, 1)},
	} {
		b.Run(bc.name, func(b *testing.B) {
			m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newTestWatermarks(), bc.overrides)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				if _, err := m.Append(b.Context(), "tenant", bc.stream, recTime, int64(i)); err != nil {
					b.Fatal(err)
				}
				if i%1000 == 999 {
					m.Reset(int64(i))
				}
			}
		})
	}
}
