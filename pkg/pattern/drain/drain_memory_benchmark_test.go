package drain

import (
	"fmt"
	"runtime"
	"testing"
)

// liveBytes approximates the live heap. Two GC cycles let the second one see a
// fully swept heap, after which HeapAlloc counts reachable objects only; it is
// finer grained than HeapInuse, which rounds up to whole spans and hides the
// per-node deltas this package cares about.
func liveBytes() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

var memoryBenchmarkFiles = []string{
	"testdata/agent-logfmt.txt",
	"testdata/journald.txt",
	"testdata/calico.txt",
	"testdata/kubernetes.txt",
	"testdata/grafana-ruler.txt",
}

// BenchmarkDrainMemory_Retained reports the heap a trained Drain holds onto,
// normalized per drain, per cluster and per prefix tree node. The input corpus
// is loaded once and shared, so the reported bytes cover Drain-owned state:
// the prefix tree, the cluster cache and the cluster templates.
func BenchmarkDrainMemory_Retained(b *testing.B) {
	for _, file := range memoryBenchmarkFiles {
		b.Run(file, func(b *testing.B) {
			lines := readTestdataLines(b, file)
			format := DetectLogFormat(lines[0])

			var totalRetained, totalNodes, totalClusters uint64
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				base := liveBytes()

				d := New(testTenant, DefaultConfig(), &fakeLimits{}, format, nil)
				for _, line := range lines {
					d.Train(line, 0)
				}

				totalRetained += liveBytes() - base
				totalNodes += uint64(countNodes(d.rootNode))
				totalClusters += uint64(len(d.Clusters()))
				runtime.KeepAlive(d)
			}

			n := float64(b.N)
			b.ReportMetric(float64(totalRetained)/n, "retained_B/drain")
			b.ReportMetric(float64(totalRetained)/float64(max(totalClusters, 1)), "retained_B/cluster")
			b.ReportMetric(float64(totalRetained)/float64(max(totalNodes, 1)), "retained_B/node")
			b.ReportMetric(float64(totalNodes)/n, "nodes/drain")
		})
	}
}

// BenchmarkDrainMemory_RetainedByDepth varies LogClusterDepth: deeper trees mean
// more internal nodes per cluster, which is exactly where per-node child storage
// overhead compounds.
func BenchmarkDrainMemory_RetainedByDepth(b *testing.B) {
	const file = "testdata/journald.txt"

	for _, depth := range []int{8, 15, 30} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			lines := readTestdataLines(b, file)
			format := DetectLogFormat(lines[0])

			var totalRetained, totalNodes uint64
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cfg := DefaultConfig()
				cfg.LogClusterDepth = depth

				base := liveBytes()

				d := New(testTenant, cfg, &fakeLimits{}, format, nil)
				for _, line := range lines {
					d.Train(line, 0)
				}

				totalRetained += liveBytes() - base
				totalNodes += uint64(countNodes(d.rootNode))
				runtime.KeepAlive(d)
			}

			n := float64(b.N)
			b.ReportMetric(float64(totalRetained)/n, "retained_B/drain")
			b.ReportMetric(float64(totalRetained)/float64(max(totalNodes, 1)), "retained_B/node")
			b.ReportMetric(float64(totalNodes)/n, "nodes/drain")
		})
	}
}
