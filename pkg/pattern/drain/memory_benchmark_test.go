package drain

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// The benchmarks in this file are intended to validate memory work on the
// pattern ingester. They come in two flavours:
//
//  1. Allocation rate benchmarks (B/op, allocs/op normalized per log line).
//     These catch garbage created on the ingest hot path.
//
//  2. Retained heap benchmarks (retained_B/drain). These catch memory we hold
//     on to after a line has been processed, which allocation rate benchmarks
//     are blind to. Only the custom metrics are meaningful here; ns/op includes
//     forced GCs.
//
// Compare before/after with benchstat, e.g.
//
//	go test ./pkg/pattern/drain/ -run XXX -bench 'Memory|Retained' -count 6 > /tmp/before.txt
//	# apply change
//	go test ./pkg/pattern/drain/ -run XXX -bench 'Memory|Retained' -count 6 > /tmp/after.txt
//	benchstat /tmp/before.txt /tmp/after.txt

var memoryCorpora = []string{
	"testdata/agent-logfmt.txt",
	"testdata/drone-json.txt",
	"testdata/journald.txt",
	"testdata/kubernetes.txt",
	"testdata/vault.txt",
}

func readLines(tb testing.TB, path string) []string {
	tb.Helper()

	file, err := os.Open(path)
	require.NoError(tb, err)
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(tb, scanner.Err())
	require.NotEmpty(tb, lines)
	return lines
}

// liveHeap returns the number of bytes reachable from the program after a
// full GC cycle. Two collections are used so that objects that only became
// unreachable during the first cycle are also accounted for.
func liveHeap() uint64 {
	var ms runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// BenchmarkDrainMemory_Train reports the cost of training a single log line,
// so allocs/op is directly readable as "allocations per ingested line".
func BenchmarkDrainMemory_Train(b *testing.B) {
	for _, corpus := range memoryCorpora {
		b.Run(corpus, func(b *testing.B) {
			lines := readLines(b, corpus)
			d := New("fake", DefaultConfig(), &fakeLimits{}, DetectLogFormat(lines[0]), nil)

			// Warm up so we measure steady state rather than cluster creation.
			ts := time.Now().UnixNano()
			for _, line := range lines {
				d.Train(line, ts)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d.Train(lines[i%len(lines)], ts)
			}
			b.StopTimer()

			// A drain whose limiter has tripped stops doing work, which would
			// make the numbers above meaningless. Surface the cluster count so
			// a degenerate run is obvious.
			b.ReportMetric(float64(len(d.Clusters())), "clusters")
		})
	}
}

// BenchmarkDrainMemory_Retained reports the steady-state heap held by a trained
// Drain. drainsPerOp drains are built per op to amortize the GC calls; the
// pattern ingester keeps one Drain per log level (8) per stream, so
// retained_B/drain multiplied by 8 is the per-stream floor.
func BenchmarkDrainMemory_Retained(b *testing.B) {
	const drainsPerOp = 16

	for _, corpus := range memoryCorpora {
		b.Run(corpus, func(b *testing.B) {
			lines := readLines(b, corpus)
			format := DetectLogFormat(lines[0])
			ts := time.Now().UnixNano()

			var totalRetained, totalClusters uint64
			for i := 0; i < b.N; i++ {
				base := liveHeap()

				drains := make([]*Drain, 0, drainsPerOp)
				for j := 0; j < drainsPerOp; j++ {
					d := New("fake", DefaultConfig(), &fakeLimits{}, format, nil)
					for _, line := range lines {
						d.Train(line, ts)
					}
					drains = append(drains, d)
				}

				retained := liveHeap() - base
				for _, d := range drains {
					totalClusters += uint64(len(d.Clusters()))
				}
				totalRetained += retained
				runtime.KeepAlive(drains)
				drains = nil
			}

			perOp := float64(b.N * drainsPerOp)
			b.ReportMetric(float64(totalRetained)/perOp, "retained_B/drain")
			b.ReportMetric(float64(totalClusters)/perOp, "clusters/drain")
			if totalClusters > 0 {
				b.ReportMetric(float64(totalRetained)/float64(totalClusters), "retained_B/cluster")
			}
		})
	}
}

// BenchmarkDrainMemory_RetainedByDepth attributes retained memory to the prefix
// tree. Every distinct pattern walks up to maxNodeDepth (LogClusterDepth-2)
// nodes, and every node carries its own map[string]*Node, so tree cost scales
// with depth while cluster payload cost does not. Reported alongside
// clusters/drain because depth also changes how many clusters are created.
func BenchmarkDrainMemory_RetainedByDepth(b *testing.B) {
	const drainsPerOp = 16

	for _, depth := range []int{8, 15, 30} {
		b.Run("depth="+strconv.Itoa(depth), func(b *testing.B) {
			lines := readLines(b, "testdata/journald.txt")
			ts := time.Now().UnixNano()

			var totalRetained, totalClusters uint64
			for i := 0; i < b.N; i++ {
				base := liveHeap()

				drains := make([]*Drain, 0, drainsPerOp)
				for j := 0; j < drainsPerOp; j++ {
					cfg := DefaultConfig()
					cfg.LogClusterDepth = depth
					d := New("fake", cfg, &fakeLimits{}, DetectLogFormat(lines[0]), nil)
					for _, line := range lines {
						d.Train(line, ts)
					}
					drains = append(drains, d)
				}

				totalRetained += liveHeap() - base
				for _, d := range drains {
					totalClusters += uint64(len(d.Clusters()))
				}
				runtime.KeepAlive(drains)
				drains = nil
			}

			perOp := float64(b.N * drainsPerOp)
			b.ReportMetric(float64(totalRetained)/perOp, "retained_B/drain")
			b.ReportMetric(float64(totalClusters)/perOp, "clusters/drain")
			b.ReportMetric(float64(totalRetained)/float64(totalClusters), "retained_B/cluster")
		})
	}
}

// BenchmarkDrainMemory_RetainedWideLines is the same measurement using lines
// close to the default MaxAllowedLineLength. Drain keeps the tokens of the most
// recently trained line in d.tokens, and those tokens alias the line itself, so
// per-drain retention scales with line width even though the line is garbage as
// soon as Train returns. See TestDrainLineRetention.
func BenchmarkDrainMemory_RetainedWideLines(b *testing.B) {
	const drainsPerOp = 16

	lines := syntheticWideLines(200, 2800)
	ts := time.Now().UnixNano()

	var totalRetained uint64
	for i := 0; i < b.N; i++ {
		base := liveHeap()

		drains := make([]*Drain, 0, drainsPerOp)
		for j := 0; j < drainsPerOp; j++ {
			d := New("fake", DefaultConfig(), &fakeLimits{}, FormatUnknown, nil)
			for _, line := range lines {
				d.Train(line, ts)
			}
			drains = append(drains, d)
		}

		totalRetained += liveHeap() - base
		runtime.KeepAlive(drains)
		drains = nil
	}

	b.ReportMetric(float64(totalRetained)/float64(b.N*drainsPerOp), "retained_B/drain")
}

// syntheticWideLines builds count lines of roughly width bytes that share a
// handful of templates, which is what real wide log lines look like to Drain.
func syntheticWideLines(count, width int) []string {
	lines := make([]string, 0, count)
	for i := 0; i < count; i++ {
		var sb strings.Builder
		sb.Grow(width)
		sb.WriteString("ts=2024-01-01T00:00:00Z level=info caller=memory.go:1 msg=\"handling request\"")
		for f := 0; sb.Len() < width; f++ {
			sb.WriteString(" field")
			sb.WriteString(strings.Repeat("x", 1+f%3))
			sb.WriteString("=value")
			sb.WriteString(strings.Repeat("y", 1+(i+f)%17))
		}
		lines = append(lines, sb.String())
	}
	return lines
}

// TestDrainLineRetention measures how much of the last trained line a Drain
// still points at once Train has returned. The tokenizers slice the input line
// rather than copying it, and Drain caches the token slice in d.tokens, so the
// whole line stays reachable until the next line for that (stream, level)
// arrives.
//
// This is the deterministic counterpart to BenchmarkDrainMemory_RetainedWideLines:
// once Train releases its reference, flip the final Logf to
// require.Zero(t, aliasing) to keep the behaviour from regressing.
func TestDrainLineRetention(t *testing.T) {
	line := "ts=2024-01-01T00:00:00Z level=info msg=\"a line that is mostly padding\" pad=" +
		strings.Repeat("p", 2000)

	d := New("fake", DefaultConfig(), &fakeLimits{}, FormatUnknown, nil)
	require.NotNil(t, d.Train(line, time.Now().UnixNano()))

	aliasing, bytes := tokensAliasing(d.tokens, line)
	t.Logf("after Train: %d/%d cached tokens alias the input line, keeping %d of %d line bytes reachable",
		aliasing, len(d.tokens), bytes, len(line))
	t.Logf("Drain also keeps cap(d.tokens)=%d token headers (%d B) and cap(state)=%d",
		cap(d.tokens), cap(d.tokens)*int(unsafe.Sizeof("")), stateCap(d.state))

	require.NotEmpty(t, d.tokens, "sanity: expected the tokenizer to produce tokens")
}

// tokensAliasing reports how many of tokens point into line's backing array,
// and how many bytes of line those tokens cover.
func tokensAliasing(tokens []string, line string) (count, bytes int) {
	base := uintptr(unsafe.Pointer(unsafe.StringData(line))) // #nosec G103 -- test-only pointer comparison
	end := base + uintptr(len(line))
	for _, tok := range tokens {
		if len(tok) == 0 {
			continue
		}
		p := uintptr(unsafe.Pointer(unsafe.StringData(tok))) // #nosec G103 -- test-only pointer comparison
		if p >= base && p < end {
			count++
			bytes += len(tok)
		}
	}
	return count, bytes
}

func stateCap(state any) int {
	if s, ok := state.([]int); ok {
		return cap(s)
	}
	return 0
}
