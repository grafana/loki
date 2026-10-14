package builder

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"runtime"
	runtimemetrics "runtime/metrics"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// BenchmarkFlushCommitter_Concurrency flushes 50 real builders through the
// real sorter, indexer and ToC writer. The bucket adds a latency per request,
// so uploads and ToC writes take time without using CPU.
//
// Run it with -cpu=2 to match the CPU limit of a builder on loki-dev-002.
//
// Each case reports peak-heap-MiB: the peak heap during the flush above the
// heap before it. This includes garbage, so it depends on when the GC runs.
//
// The cases under "live/" force a GC every 50 ms during the flush and report
// peak-live-MiB: the peak live heap above the live heap before the flush. The
// forced GCs slow the flush down, so ignore the time of these cases. They also
// report base-live-MiB: the live heap before the flush, which holds the data
// of every builder.
func BenchmarkFlushCommitter_Concurrency(b *testing.B) {
	// The dev002 profiles scale the request times so that each I/O stage has
	// the share of the flush time that it has on loki-dev-002. The flush there
	// sorts much more data than this benchmark, so real request times would
	// make I/O most of the flush.
	var (
		none        = benchBucketLatency{}
		dev002Slow  = benchBucketLatency{dataPut: 10 * time.Millisecond, put: 4 * time.Millisecond, get: time.Millisecond}
		dev002Fast  = benchBucketLatency{dataPut: 4 * time.Millisecond, put: 4 * time.Millisecond, get: time.Millisecond}
		realRequest = benchBucketLatency{dataPut: 50 * time.Millisecond, put: 50 * time.Millisecond, get: 20 * time.Millisecond, bytesPerSecond: 100 << 20}
	)
	profiles := []struct {
		name    string
		latency benchBucketLatency
		live    bool
	}{
		// Requests take no time, so the flush is CPU work only.
		{name: "upload=none", latency: none},
		// Data object uploads are slow, as before the upload size fix.
		{name: "upload=dev002-slow", latency: dev002Slow, live: true},
		// Data object uploads take as long as other uploads.
		{name: "upload=dev002-fast", latency: dev002Fast},
		// Requests take as long as typical object storage requests.
		{name: "upload=real", latency: realRequest},
	}
	orders := []struct {
		name  string
		order func([]builder)
	}{
		{name: "order=tenant", order: func([]builder) {}},
		{name: "order=largest-first", order: func(builders []builder) {
			slices.SortStableFunc(builders, func(a, b builder) int {
				return cmp.Compare(b.GetEstimatedSize(), a.GetEstimatedSize())
			})
		}},
	}

	tenants := newBenchTenants()
	for _, live := range []bool{false, true} {
		for _, profile := range profiles {
			if live && !profile.live {
				continue
			}
			for _, order := range orders {
				for _, concurrency := range []int{1, 2, 4} {
					name := fmt.Sprintf("%s/%s/concurrency=%d", profile.name, order.name, concurrency)
					if live {
						name = "live/" + name
					}
					b.Run(name, func(b *testing.B) {
						benchmarkFlush(b, tenants, profile.latency, order.order, concurrency, live)
					})
				}
			}
		}
	}
}

func benchmarkFlush(b *testing.B, tenants []benchTenant, latency benchBucketLatency, order func([]builder), concurrency int, live bool) {
	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("bench", flag.PanicOnError))
	cfg.LogsobjBuilder.DropDuplicates = true

	var (
		logger       = log.NewNopLogger()
		reg          = prometheus.NewRegistry()
		scratchStore = scratch.NewMemory()
	)
	factory, err := logsobj.NewBuilderFactory(cfg.LogsobjBuilder, scratchStore, logsobj.NewBuilderMetrics(), logger, nil)
	if err != nil {
		b.Fatal(err)
	}
	var (
		indexerMetrics    = index.NewIndexerMetrics(reg)
		indexobjMetrics   = indexobj.NewBuilderMetrics(reg)
		calculatorMetrics = index.NewCalculatorMetrics(reg)
		tocWriterMetrics  = metastore.NewTocWriterMetrics(reg)
		flusher           = newFlusher(logsobj.NewSorter(factory, reg), nil, logger, reg)
	)

	var peak, base uint64
	for b.Loop() {
		b.StopTimer()
		// A new bucket per flush keeps the ToCs from growing across flushes.
		bucket := &benchBucket{Bucket: objstore.NewInMemBucket(), latency: latency}
		idxBucket := objstore.NewPrefixedBucket(bucket, "index/")
		flusher.uploader = dataobj_uploader.New(dataobj_uploader.Config{SHAPrefixSize: 2}, bucket, logger)
		indexer, err := index.NewSimpleIndexer(cfg.IndexobjBuilder, scratchStore, logger, idxBucket, indexerMetrics, indexobjMetrics, calculatorMetrics)
		if err != nil {
			b.Fatal(err)
		}
		tocWriter := metastore.NewTableOfContentsWriter(idxBucket, metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, logger, tocWriterMetrics, indexobjMetrics)
		committer := newFlushCommitter(flusher, &mockCommitter{}, indexer, tocWriter, 0, concurrency, logger, prometheus.NewRegistry())

		builders := make([]builder, len(tenants))
		for i, tenant := range tenants {
			builders[i] = tenant.fill(b, factory)
		}
		order(builders)

		runtime.GC()
		sampler := startHeapSampler(live)
		b.StartTimer()

		if err := committer.Flush(context.Background(), builders, flushReasonBuilderFull, 1); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		peak = max(peak, sampler.stop())
		base = max(base, sampler.base)
		b.StartTimer()
	}
	if live {
		b.ReportMetric(float64(base)/(1<<20), "base-live-MiB")
		b.ReportMetric(float64(peak)/(1<<20), "peak-live-MiB")
	} else {
		b.ReportMetric(float64(peak)/(1<<20), "peak-heap-MiB")
	}
}

// A benchTenant describes the log lines of one builder.
type benchTenant struct {
	name  string
	lines int
	seed  uint64
}

// fill returns a new builder that holds the log lines of t. The lines are the
// same on every call. fill generates them on each call rather than once, so
// that they do not add to the heap that the benchmark measures.
//
// The lines are in logfmt, with random IDs to keep compression realistic.
// Their timestamps are out of order inside one ToC window, so the sort has
// work to do.
func (t benchTenant) fill(b *testing.B, factory *logsobj.BuilderFactory) builder {
	const (
		linesPerStream = 2_000
		linesPerAppend = 100
	)
	var (
		windowStart = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
		levels      = []string{"debug", "info", "info", "info", "warn", "error"}
		paths       = []string{"push", "query_range", "labels", "series", "index/stats", "tail"}
		messages    = []string{"request completed", "cache miss", "retrying request", "flushed chunk", "query executed"}
		rnd         = rand.New(rand.NewPCG(t.seed, t.seed))
		recTime     = time.Now()
	)
	lb, err := factory.NewBuilder()
	if err != nil {
		b.Fatal(err)
	}
	for n, streamID := t.lines, 0; n > 0; streamID++ {
		labels := fmt.Sprintf(`{service="svc-%d", env="prod", pod="pod-%d"}`, streamID%7, streamID)
		for i := 0; i < linesPerStream && n > 0; i += linesPerAppend {
			stream := logproto.Stream{Labels: labels}
			for j := 0; j < linesPerAppend && n > 0; j++ {
				ts := windowStart.Add(time.Duration(rnd.Int64N(int64(time.Hour))))
				line := fmt.Sprintf(
					"ts=%s level=%s msg=%q path=/loki/api/v1/%s user_id=%d duration=%dms status=%d bytes=%d trace_id=%016x",
					ts.Format(time.RFC3339Nano), levels[rnd.IntN(len(levels))], messages[rnd.IntN(len(messages))],
					paths[rnd.IntN(len(paths))], rnd.IntN(10_000), rnd.IntN(5_000), 200+rnd.IntN(4)*100, rnd.IntN(1<<20), rnd.Uint64(),
				)
				stream.Entries = append(stream.Entries, logproto.Entry{Timestamp: ts, Line: line})
				n--
			}
			if err := lb.Append(t.name, stream, recTime); err != nil {
				b.Fatal(err)
			}
		}
	}
	return lb
}

// newBenchTenants returns the builders of one flush. The sizes follow
// loki-dev-002, where 80% of the objects in a flush are below 100 KB:
//   - 40 small builders of about 400 KB of log lines each,
//   - 8 medium builders of about 8 MB each,
//   - 2 large builders of about 60 MB each.
//
// The sizes are spread across the tenant names, so the large builders do not
// come first or last in tenant order.
func newBenchTenants() []benchTenant {
	const (
		small  = 2_000
		medium = 40_000
		large  = 300_000
	)
	lines := make([]int, 0, 50)
	for range 40 {
		lines = append(lines, small)
	}
	for range 8 {
		lines = append(lines, medium)
	}
	lines = append(lines, large, large)

	rnd := rand.New(rand.NewPCG(1, 2))
	rnd.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })

	tenants := make([]benchTenant, len(lines))
	for i, n := range lines {
		tenants[i] = benchTenant{name: fmt.Sprintf("tenant-%02d", i), lines: n, seed: uint64(i)}
	}
	return tenants
}

// benchBucketLatency is the time that a [benchBucket] request takes.
type benchBucketLatency struct {
	// dataPut is the base time of a data object upload.
	dataPut time.Duration
	// put is the base time of any other upload.
	put time.Duration
	// get is the base time of a download.
	get time.Duration
	// bytesPerSecond is the upload bandwidth of one request. It adds to the
	// base time of an upload.
	bytesPerSecond float64
}

// benchBucket adds latency to the requests of an in-memory bucket. It keeps
// only ToC objects and discards every other upload, so that uploaded data does
// not add to the heap that the benchmark measures.
type benchBucket struct {
	objstore.Bucket
	latency benchBucketLatency
}

func (b *benchBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	if isBenchToc(name) {
		time.Sleep(b.latency.put)
		return b.Bucket.Upload(ctx, name, r)
	}
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		return err
	}
	base := b.latency.put
	if strings.HasPrefix(name, "objects/") {
		base = b.latency.dataPut
	}
	if b.latency.bytesPerSecond > 0 {
		base += time.Duration(float64(n) / b.latency.bytesPerSecond * float64(time.Second))
	}
	time.Sleep(base)
	return nil
}

func (b *benchBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	time.Sleep(b.latency.get)
	return b.Bucket.Get(ctx, name)
}

// GetAndReplace sleeps before it calls the in-memory bucket, because that
// bucket holds a single lock for every replace.
func (b *benchBucket) GetAndReplace(ctx context.Context, name string, f func(io.ReadCloser) (io.ReadCloser, error)) error {
	time.Sleep(b.latency.get + b.latency.put)
	return b.Bucket.GetAndReplace(ctx, name, f)
}

func isBenchToc(name string) bool {
	return strings.Contains(name, metastore.TocPrefix)
}

// heapSampler records the peak heap while it runs.
type heapSampler struct {
	done chan struct{}
	wg   sync.WaitGroup
	base uint64
	peak uint64
}

const (
	heapObjectsMetric = "/memory/classes/heap/objects:bytes"
	heapLiveMetric    = "/gc/heap/live:bytes"
)

// startHeapSampler samples the heap until stop. The caller must run a GC
// first, so that the heap at the start holds no garbage.
//
// If live is true, the sampler forces a GC every 50 ms and samples the live
// heap that each GC marks. Otherwise it samples the heap, garbage included,
// every millisecond.
func startHeapSampler(live bool) *heapSampler {
	name, period := heapObjectsMetric, time.Millisecond
	if live {
		name, period = heapLiveMetric, 50*time.Millisecond
	}
	sample := []runtimemetrics.Sample{{Name: name}}
	runtimemetrics.Read(sample)

	s := &heapSampler{done: make(chan struct{}), base: sample[0].Value.Uint64()}
	s.wg.Go(func() {
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
			}
			if live {
				runtime.GC()
			}
			runtimemetrics.Read(sample)
			s.peak = max(s.peak, sample[0].Value.Uint64())
		}
	})
	return s
}

// stop returns the peak heap above the heap at the start.
func (s *heapSampler) stop() uint64 {
	close(s.done)
	s.wg.Wait()
	if s.peak < s.base {
		return 0
	}
	return s.peak - s.base
}
