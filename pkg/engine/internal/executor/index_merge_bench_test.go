package executor

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/builder"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// The shape of the source indexes in BenchmarkIndexMerge.
const (
	benchIndexTenant          = "acme"
	benchIndexSources         = 8
	benchLogObjectsPerIndex   = 64
	benchSectionsPerLogObject = 8
	benchStreamsPerSection    = 10_000
)

// BenchmarkIndexMerge merges 8 source index objects that hold only postings.
// The merges that used the most memory in a dev cluster had 8 sources with two
// postings sections each. Here, every source has one bloom section and one
// label section, built by benchPostings.
//
// The sub-benchmarks share the sources:
//
//   - merge runs the whole index merge.
//   - read_postings only drains the K-way merge of the postings readers. It
//     shows the memory of the readers, which the section flushes of the
//     builder hide in merge.
//
// Both report peak-heap-B/op and peak-live-B/op, described in
// [measurePeakHeap] and [heapPeak]. B/op only counts the total of all
// allocations, so it does not show the peak.
func BenchmarkIndexMerge(b *testing.B) {
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
	for i := range benchIndexSources {
		path := fmt.Sprintf("indexes/source-%d", i)
		pb, size := benchPostings(b, rng, cfg)
		uploadSections(b, bucket, path, pb)
		run.Sections = append(run.Sections, &compactionv2pb.SectionRef{ObjectPath: path})
		inputBytes += size
	}
	node := &physical.IndexMerge{Tenant: benchIndexTenant, Runs: []*compactionv2pb.RunRef{run}}

	b.Run("merge", func(b *testing.B) {
		b.SetBytes(inputBytes)
		benchPeakHeap(b, func() {
			c := newTestExecutorContext(b, bucket)
			c.indexobjCfg = cfg
			_, err := c.doIndexMerge(b.Context(), node)
			require.NoError(b, err)
		})
	})

	b.Run("read_postings", func(b *testing.B) {
		b.SetBytes(inputBytes)
		benchPeakHeap(b, func() {
			c := newTestExecutorContext(b, bucket)
			objects, _, err := c.classifyRuns(b.Context(), node)
			require.NoError(b, err)
			readers := postingsSequences(b.Context(), objects)
			require.NoError(b, drainMerge(b.Context(), readers, comparePostingsRow, func(postings.Row) bool { return true }))
		})
	})
}

// benchPostings returns a postings builder that indexes benchLogObjectsPerIndex
// new log objects with benchSectionsPerLogObject logs sections each. It also
// returns the size of the bloom filters and bitmaps in the builder, in bytes.
//
// Each logs section gets one bloom row per key in benchMetadata and one label
// row per value of each label in benchIndexLabels. Bloom rows sort first, so
// the first batches that a reader loads hold only bloom rows.
func benchPostings(b *testing.B, rng *rand.Rand, cfg logsobj.BuilderBaseConfig) (*postings.MergeBuilder, int64) {
	pb := postings.NewMergeBuilder(nil, int(cfg.TargetPageSize), cfg.MaxPageRows, int(cfg.TargetSectionSize))
	pb.SetTenant(benchIndexTenant)

	var size int64
	for i := range benchLogObjectsPerIndex {
		obj := newBenchLogObject(rng, i)
		for section := range benchSectionsPerLogObject {
			for _, key := range benchMetadata {
				entry := obj.bloomEntry(rng, section, key.name, key.cardinality)
				size += int64(len(entry.BloomFilter) + len(entry.StreamIDBitmap))
				require.NoError(b, pb.AppendBloomEntry(entry))
			}
			for _, label := range benchIndexLabels {
				for value := range label.cardinality {
					entry := obj.labelEntry(rng, section, label.name, value, label.cardinality)
					size += int64(len(entry.StreamIDBitmap))
					require.NoError(b, pb.AppendLabelEntry(entry))
				}
			}
		}
	}
	return pb, size
}

// benchIndexLabels lists the stream labels that get label postings in
// benchPostings. Cardinality is the number of distinct values of the label in
// one logs section.
var benchIndexLabels = []struct {
	name        string
	cardinality int
}{
	{"app", 50},
	{"cluster", 2},
	{"namespace", 10},
}

// benchLogObject is a log object that a benchmark index refers to. The i-th
// log object of an index covers the i-th minute of the benchmark time range.
type benchLogObject struct {
	path         string
	minTimestamp int64
	maxTimestamp int64
}

func newBenchLogObject(rng *rand.Rand, i int) benchLogObject {
	start := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
	return benchLogObject{
		path:         fmt.Sprintf("objects/tenants/%s/%016x%016x", benchIndexTenant, rng.Uint64(), rng.Uint64()),
		minTimestamp: start.UnixNano(),
		maxTimestamp: start.Add(time.Minute).UnixNano(),
	}
}

// bloomEntry returns the bloom posting of a metadata key with about
// cardinality values in a logs section of o. Every stream of the section has
// the key. A cardinality of 0 uses benchHighCardinality.
func (o benchLogObject) bloomEntry(rng *rand.Rand, section int, key string, cardinality int) postings.BloomEntry {
	return postings.BloomEntry{
		ObjectPath:       o.path,
		ShardBuckets:     16,
		SectionIndex:     int64(section),
		ColumnName:       key,
		BloomFilter:      benchBloomFilter(rng, cardinality),
		StreamIDBitmap:   benchStreamBitmap(rng, benchStreamsPerSection),
		MinTimestamp:     o.minTimestamp,
		MaxTimestamp:     o.maxTimestamp,
		UncompressedSize: 1 << 20,
	}
}

// labelEntry returns the label posting of one value of a label with
// cardinality values in a logs section of o. The value covers an equal share
// of the streams of the section.
func (o benchLogObject) labelEntry(rng *rand.Rand, section int, label string, value, cardinality int) postings.LabelEntry {
	return postings.LabelEntry{
		ObjectPath:       o.path,
		ShardBuckets:     16,
		SectionIndex:     int64(section),
		ColumnName:       label,
		LabelValue:       fmt.Sprintf("%s-%d", label, value),
		StreamIDBitmap:   benchStreamBitmap(rng, benchStreamsPerSection/cardinality),
		MinTimestamp:     o.minTimestamp,
		MaxTimestamp:     o.maxTimestamp,
		UncompressedSize: 1 << 16,
		MaxShardBucket:   15,
	}
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

// benchStreamBitmap returns an LSB-encoded bitmap over the
// benchStreamsPerSection stream IDs of a logs section, with setBits random IDs
// set.
func benchStreamBitmap(rng *rand.Rand, setBits int) []byte {
	buf := make([]byte, (benchStreamsPerSection+7)/8)
	for range setBits {
		id := rng.IntN(benchStreamsPerSection)
		buf[id/8] |= 1 << (id % 8)
	}
	return buf
}
