package main

import (
	"context"
	"math"
	"time"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"

	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type metrics struct {
	sbfCreationTime    prometheus.Counter   // time spent creating sbfs
	chunkSize          prometheus.Histogram // uncompressed size of all chunks summed per series
	bloomSize          prometheus.Histogram // size of the bloom filter in bytes
	hammingWeightRatio prometheus.Histogram // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount     prometheus.Histogram // estimated number of elements in the bloom filter
}

/*
BloomTokenizer is a utility that converts either Loki chunks or individual lines into tokens.
These tokens are n-grams, representing adjacent letters, that are used to populate a bloom filter.
https://en.wikipedia.org/wiki/Bloom_filter
Bloom filters are utilized for faster lookups of log lines.
*/
type BloomTokenizer struct {
	metrics *metrics

	lineTokenizer *v1.NGramTokenizer
	cache         map[string]interface{}
}

const cacheSize = 150000
const bloomTokenizerMetricsSubsystem = "bloom_tokenizer"
const eightBits = 8

// NewBloomTokenizer returns a new instance of the Bloom Tokenizer.
// Warning: the tokens returned use the same byte slice to reduce allocations. This has two consequences:
// 1) The token slices generated must not be mutated externally
// 2) The token slice must not be used after the next call to `Tokens()` as it will repopulate the slice.
// 2) This is not thread safe.
func NewBloomTokenizer(reg prometheus.Registerer, NGramLength, NGramSkip int) (*BloomTokenizer, error) {
	t := &BloomTokenizer{
		metrics: newMetrics(reg, constants.Loki, bloomTokenizerMetricsSubsystem),
	}
	t.cache = make(map[string]interface{}, cacheSize)
	t.lineTokenizer = v1.NewNGramTokenizer(NGramLength, NGramSkip)

	level.Info(util_log.Logger).Log("bloom tokenizer created")

	return t, nil
}

func (bt *BloomTokenizer) SetLineTokenizer(t *v1.NGramTokenizer) {
	bt.lineTokenizer = t
}

func (bt *BloomTokenizer) GetNGramLength() uint64 {
	return uint64(bt.lineTokenizer.N)
}

func (bt *BloomTokenizer) GetNGramSkip() uint64 {
	return uint64(bt.lineTokenizer.Skip)
}

func newMetrics(r prometheus.Registerer, namespace, subsystem string) *metrics {
	return &metrics{
		sbfCreationTime: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name:      "bloom_creation_time",
			Help:      "Time spent creating scalable bloom filters",
			Namespace: namespace,
			Subsystem: subsystem,
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:      "bloom_chunk_series_size",
			Help:      "Uncompressed size of chunks in a series",
			Buckets:   prometheus.ExponentialBucketsRange(1024, 1073741824, 10),
			Namespace: namespace,
			Subsystem: subsystem,
		}),
		bloomSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:      "bloom_size",
			Help:      "Size of the bloom filter in bytes",
			Buckets:   prometheus.ExponentialBucketsRange(128, 16777216, 8),
			Namespace: namespace,
			Subsystem: subsystem,
		}),
		hammingWeightRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:      "bloom_hamming_weight_ratio",
			Help:      "Ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter",
			Buckets:   prometheus.ExponentialBucketsRange(0.001, 1, 12),
			Namespace: namespace,
			Subsystem: subsystem,
		}),
		estimatedCount: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:      "bloom_estimated_count",
			Help:      "Estimated number of elements in the bloom filter",
			Buckets:   prometheus.ExponentialBucketsRange(1, 33554432, 10),
			Namespace: namespace,
			Subsystem: subsystem,
		}),
	}
}

func clearCache(cache map[string]interface{}) {
	clear(cache)
}

// prefixedToken returns a byte slice with sufficient capacity for a chunk-ref prefixed token
// of specific ngram length, along with the length of the prefix.
// It ensures enough capacity for the prefix and the token so additional tokens can be created
// without allocations by appending them to the prefix length
func prefixedToken(ngram int, chk logproto.ChunkRef) ([]byte, int) {
	var enc encoding.Encbuf
	enc.PutBE64(uint64(chk.From))
	enc.PutBE64(uint64(chk.Through))
	enc.PutBE32(chk.Checksum)
	prefixLn := enc.Len() // record the length of the prefix

	enc.PutBytes(make([]byte, ngram*v1.MaxRuneLen)) // ensure enough capacity for the ngram

	// return the underlying byte slice and the length of the prefix
	return enc.Get(), prefixLn
}

// PopulateSeriesWithBloom is intended to be called on the write path, and is used to populate the bloom filter for a given series.
func (bt *BloomTokenizer) PopulateSeriesWithBloom(seriesWithBloom *v1.SeriesWithBloom, chunks []chunk.Chunk) error {
	startTime := time.Now().UnixMilli()

	clearCache(bt.cache)
	chunkTotalUncompressedSize := 0

	for idx := range chunks {
		lc := chunks[idx].Data.(*chunkenc.Facade).LokiChunk()
		tokenBuf, prefixLn := prefixedToken(bt.lineTokenizer.N, chunks[idx].ChunkRef)
		chunkTotalUncompressedSize += lc.UncompressedSize()

		itr, err := lc.Iterator(
			context.Background(),
			time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
			time.Unix(0, math.MaxInt64),
			logproto.FORWARD,
			log.NewNoopPipeline().ForStream(chunks[idx].Metric),
		)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "chunk iterator cannot be created", "err", err)
			return err
		}

		defer itr.Close()

		for itr.Next() && itr.Error() == nil {
			chunkTokenizer := v1.NewPrefixedTokenIter(tokenBuf, prefixLn, bt.lineTokenizer.Tokens(itr.Entry().Line))
			for chunkTokenizer.Next() {
				tok := chunkTokenizer.At()
				if tok != nil {
					str := string(tok)
					_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
					if !found {
						bt.cache[str] = nil

						seriesWithBloom.Bloom.ScalableBloomFilter.TestAndAdd(tok)

						if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
							clearCache(bt.cache)
						}
					}
				}
			}
			lineTokenizer := bt.lineTokenizer.Tokens(itr.Entry().Line)
			for lineTokenizer.Next() {
				tok := lineTokenizer.At()
				if tok != nil {
					str := string(tok)
					_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
					if !found {
						bt.cache[str] = nil

						seriesWithBloom.Bloom.ScalableBloomFilter.TestAndAdd(tok)

						if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
							clearCache(bt.cache)
						}
					}
				}
			}

		}
		seriesWithBloom.Series.Chunks = append(seriesWithBloom.Series.Chunks, v1.ChunkRef{
			From:     chunks[idx].From,
			Through:  chunks[idx].Through,
			Checksum: chunks[idx].Checksum,
		})
	} // for each chunk

	endTime := time.Now().UnixMilli()

	fillRatio := seriesWithBloom.Bloom.ScalableBloomFilter.FillRatio()
	bt.metrics.hammingWeightRatio.Observe(fillRatio)
	bt.metrics.estimatedCount.Observe(
		float64(estimatedCount(seriesWithBloom.Bloom.ScalableBloomFilter.Capacity(), fillRatio)),
	)
	bt.metrics.bloomSize.Observe(float64(seriesWithBloom.Bloom.ScalableBloomFilter.Capacity() / eightBits))
	bt.metrics.sbfCreationTime.Add(float64(endTime - startTime))
	bt.metrics.chunkSize.Observe(float64(chunkTotalUncompressedSize))
	return nil
}
