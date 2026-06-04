package tsdb

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Helpers shared by the mmap-vs-pread benchmarks in reader_at_bench_test.go.

// countingReaderAt wraps an io.ReaderAt and tallies read calls + bytes, so we can
// measure the read amplification of driving the index reader off mmap.
type countingReaderAt struct {
	r     io.ReaderAt
	calls atomic.Int64
	bytes atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls.Add(1)
	c.bytes.Add(int64(len(p)))
	return c.r.ReadAt(p, off)
}
func (c *countingReaderAt) reset()       { c.calls.Store(0); c.bytes.Store(0) }
func (c *countingReaderAt) callN() int64 { return c.calls.Load() }
func (c *countingReaderAt) byteN() int64 { return c.bytes.Load() }

// genSeries builds nSeries realistic Loki streams with chunksPer chunks each.
// Label cardinality is modeled on a k8s logging deployment: a few low-card
// dimensions (cluster/namespace/level/stream) and one high-card dimension (pod).
func genSeries(nSeries, chunksPer int) []LoadableSeries {
	out := make([]LoadableSeries, 0, nSeries)
	base := model.Time(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	for i := range nSeries {
		lb := labels.NewScratchBuilder(8)
		lb.Add("cluster", fmt.Sprintf("cluster-%d", i%3))
		lb.Add("namespace", fmt.Sprintf("ns-%d", i%40))
		lb.Add("service_name", fmt.Sprintf("svc-%d", i%200))
		lb.Add("container", fmt.Sprintf("container-%d", i%4))
		lb.Add("level", []string{"info", "warn", "error", "debug"}[i%4])
		lb.Add("stream", []string{"stdout", "stderr"}[i%2])
		// high-cardinality dimension: pod, ~unique per series
		lb.Add("pod", fmt.Sprintf("svc-%d-%x-%05d", i%200, i*2654435761&0xffffff, i))
		lb.Sort()
		ls := lb.Labels()

		chks := make(index.ChunkMetas, 0, chunksPer)
		t := base
		for c := range chunksPer {
			mint := t
			maxt := t + model.Time(2*time.Hour.Milliseconds())
			chks = append(chks, index.ChunkMeta{
				Checksum: uint32(i*31 + c),
				MinTime:  int64(mint),
				MaxTime:  int64(maxt),
				KB:       uint32(1024 + c*7),
				Entries:  uint32(5000 + c*13),
			})
			t = maxt
		}
		out = append(out, LoadableSeries{Labels: ls, Chunks: chks})
	}
	return out
}
