package executor

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"runtime"
	"runtime/metrics"
	"testing"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/builder"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// BenchmarkIndexMerge merges 8 source index objects that hold only postings.
// The merges that used the most memory in a dev cluster had 8 sources with two
// postings sections each. Here, every source has one bloom section and one
// label section.
//
// Every source indexes its own 64 log objects with 8 logs sections each. Each
// logs section gets one bloom row per key in benchMetadata and one label row
// per value of each key in benchIndexLabels. Bloom rows sort first, so the
// first batch that each source reader loads holds only bloom rows.
//
// The sub-benchmarks share the sources:
//
//   - merge runs the whole index merge.
//   - read_postings only drains the K-way merge of the postings readers. It
//     shows the memory of the readers, which the section flushes of the
//     builder hide in merge.
//
// The benchmark reports two heap metrics in addition to B/op, which only
// counts the total of all allocations. Both exclude the heap before the
// measured call starts, which holds the source objects in the in-memory
// bucket:
//
//   - peak-heap-B/op is the largest heap object size seen while one call
//     runs. It includes garbage that the GC has not freed yet, so it changes
//     with GC timing. It is the closest match to the memory use of a worker.
//   - peak-live-B/op is the largest heap that a GC cycle marked live during
//     one call. It is steadier between runs and matches the in-use heap
//     profile.
func BenchmarkIndexMerge(b *testing.B) {
	const (
		tenant            = "acme"
		sourceCount       = 8
		objectsPerSource  = 64
		sectionsPerObject = 8
		streamsPerSection = 10_000
	)
	rng := rand.New(rand.NewPCG(1, 2))

	// The benchmark cannot use the compactor config, because the compactor
	// package imports this one. The dataobj builder registers the same index
	// builder defaults as the compactor.
	var defaults builder.Config
	flagext.DefaultValues(&defaults)
	cfg := defaults.IndexobjBuilder

	bucket := objstore.NewInMemBucket()
	run := &compactionv2pb.RunRef{}
	var inputBytes int64
	for source := range sourceCount {
		pb := postings.NewMergeBuilder(nil, int(cfg.TargetPageSize), cfg.MaxPageRows, int(cfg.TargetSectionSize))
		pb.SetTenant(tenant)
		for object := range objectsPerSource {
			objectPath := fmt.Sprintf("objects/tenants/%s/%02x/%016x%016x", tenant, source, rng.Uint64(), rng.Uint64())
			minTs := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC).Add(time.Duration(object) * time.Minute).UnixNano()
			maxTs := minTs + time.Minute.Nanoseconds()
			for section := range sectionsPerObject {
				for _, key := range benchMetadata {
					entry := postings.BloomEntry{
						ObjectPath:       objectPath,
						ShardBuckets:     16,
						SectionIndex:     int64(section),
						ColumnName:       key.name,
						BloomFilter:      benchBloomFilter(rng, key.cardinality),
						StreamIDBitmap:   benchStreamBitmap(rng, streamsPerSection, streamsPerSection),
						MinTimestamp:     minTs,
						MaxTimestamp:     maxTs,
						UncompressedSize: 1 << 20,
					}
					inputBytes += int64(len(entry.BloomFilter) + len(entry.StreamIDBitmap))
					require.NoError(b, pb.AppendBloomEntry(entry))
				}
				for _, label := range benchIndexLabels {
					for value := range label.cardinality {
						entry := postings.LabelEntry{
							ObjectPath:       objectPath,
							ShardBuckets:     16,
							SectionIndex:     int64(section),
							ColumnName:       label.name,
							LabelValue:       fmt.Sprintf("%s-%d", label.name, value),
							StreamIDBitmap:   benchStreamBitmap(rng, streamsPerSection, streamsPerSection/label.cardinality),
							MinTimestamp:     minTs,
							MaxTimestamp:     maxTs,
							UncompressedSize: 1 << 16,
							MaxShardBucket:   15,
						}
						inputBytes += int64(len(entry.StreamIDBitmap))
						require.NoError(b, pb.AppendLabelEntry(entry))
					}
				}
			}
		}

		ob := dataobj.NewBuilder(nil)
		require.NoError(b, ob.Append(pb))
		obj, closer, err := ob.Flush()
		require.NoError(b, err)
		path := fmt.Sprintf("indexes/source-%d", source)
		require.NoError(b, uploadObjectToBucket(b.Context(), bucket, path, obj))
		require.NoError(b, closer.Close())
		run.Sections = append(run.Sections, &compactionv2pb.SectionRef{ObjectPath: path})
	}

	node := &physical.IndexMerge{
		Tenant: tenant,
		Runs:   []*compactionv2pb.RunRef{run},
	}

	b.Run("merge", func(b *testing.B) {
		b.SetBytes(inputBytes)
		benchPeakHeap(b, func() {
			c := newTestExecutorContext(b, bucket)
			c.indexobjCfg = cfg
			_, err := c.doIndexMerge(b.Context(), node)
			if err != nil {
				require.NoError(b, err)
			}
		})
	})

	b.Run("read_postings", func(b *testing.B) {
		b.SetBytes(inputBytes)
		benchPeakHeap(b, func() {
			c := newTestExecutorContext(b, bucket)
			objects, _, err := c.classifyRuns(b.Context(), node)
			require.NoError(b, err)

			readers := make([]sequence[postings.Row], 0, len(objects))
			for _, obj := range objects {
				readers = append(readers, &sectionConcatSeq[postings.Row]{
					ctx:        b.Context(),
					objectPath: obj.path,
					sections:   obj.postings,
					seqIdx:     len(readers),
					open:       openPostingsReader,
				})
			}
			err = drainMerge(b.Context(), readers, comparePostingsRow, func(postings.Row) bool { return true })
			if err != nil {
				require.NoError(b, err)
			}
		})
	})
}

// benchPeakHeap runs fn once per benchmark iteration and reports the largest
// heap sizes seen while fn runs, as peak-heap-B/op and peak-live-B/op.
func benchPeakHeap(b *testing.B, fn func()) {
	b.ReportAllocs()
	var peak heapPeak
	for b.Loop() {
		runtime.GC()
		sampler := startHeapSampler()
		fn()
		peak = peak.max(sampler.stop())
	}
	b.ReportMetric(float64(peak.heap), "peak-heap-B/op")
	b.ReportMetric(float64(peak.live), "peak-live-B/op")
}

// benchIndexLabels lists the stream labels that get label postings in
// BenchmarkIndexMerge. Cardinality is the number of distinct values of the
// label in one logs section.
var benchIndexLabels = []struct {
	name        string
	cardinality int
}{
	{"app", 50},
	{"cluster", 2},
	{"namespace", 10},
}

// benchHighCardinality is the mean number of distinct values that a key with a
// cardinality of 0 in benchMetadata has in one logs section.
const benchHighCardinality = 50_000

// benchBloomFilter returns random bytes with the length of a marshaled bloom
// filter for about cardinality values at the false positive rate of the
// postings builder. A cardinality of 0 uses benchHighCardinality.
//
// The value count varies by up to 50% either way. With equal filter sizes,
// every batch has the same size, and the buffer growth waste of the readers
// depends on where that one size falls between two powers of two.
//
// The bytes are random because the bloom column is not compressed, so only
// the length affects the merge.
func benchBloomFilter(rng *rand.Rand, cardinality int) []byte {
	if cardinality == 0 {
		cardinality = benchHighCardinality
	}
	cardinality = max(1, cardinality/2+rng.IntN(cardinality+1))
	bits, _ := bloom.EstimateParameters(uint(cardinality), 1.0/128.0)
	buf := make([]byte, (bits+63)/64*8+24)
	for i := 0; i+8 <= len(buf); i += 8 {
		binary.LittleEndian.PutUint64(buf[i:], rng.Uint64())
	}
	return buf
}

// benchStreamBitmap returns an LSB-encoded stream ID bitmap for streams IDs
// with setBits random IDs set.
func benchStreamBitmap(rng *rand.Rand, streams, setBits int) []byte {
	buf := make([]byte, (streams+7)/8)
	for range setBits {
		id := rng.IntN(streams)
		buf[id/8] |= 1 << (id % 8)
	}
	return buf
}

// heapPeak holds the largest heap sizes seen above a baseline, in bytes.
type heapPeak struct {
	// heap is the size of all heap objects, live or not yet freed.
	heap uint64
	// live is the heap size that the last GC cycle marked live.
	live uint64
}

func (p heapPeak) max(o heapPeak) heapPeak {
	return heapPeak{heap: max(p.heap, o.heap), live: max(p.live, o.live)}
}

// heapSampler polls the heap sizes in a goroutine and records the largest
// values above the sizes at start. The Go heap profile only shows the heap at
// the last GC, so it misses the peak inside one merge.
//
// The baseline excludes memory that lives across the measured call, such as
// the source objects in an in-memory bucket.
type heapSampler struct {
	done chan struct{}
	peak chan heapPeak
}

func startHeapSampler() *heapSampler {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/heap/live:bytes"},
	}
	read := func() heapPeak {
		metrics.Read(samples)
		return heapPeak{heap: samples[0].Value.Uint64(), live: samples[1].Value.Uint64()}
	}
	base := read()

	s := &heapSampler{done: make(chan struct{}), peak: make(chan heapPeak)}
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var peak heapPeak
		for {
			peak = peak.max(read())
			select {
			case <-s.done:
				s.peak <- heapPeak{
					heap: peak.heap - min(peak.heap, base.heap),
					live: peak.live - min(peak.live, base.live),
				}
				return
			case <-ticker.C:
			}
		}
	}()
	return s
}

// stop ends sampling and returns the largest heap sizes seen above the sizes
// at start.
func (s *heapSampler) stop() heapPeak {
	close(s.done)
	return <-s.peak
}
