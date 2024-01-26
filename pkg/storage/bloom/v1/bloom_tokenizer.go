package v1

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util/encoding"
	util_log "github.com/grafana/loki/pkg/util/log"
)

/*
BloomTokenizer is a utility that converts either Loki chunks or individual lines into tokens.
These tokens are n-grams, representing adjacent letters, that are used to populate a bloom filter.
https://en.wikipedia.org/wiki/Bloom_filter
Bloom filters are utilized for faster lookups of log lines.
*/
type BloomTokenizer struct {
	metrics *Metrics

	lineTokenizer *NGramTokenizer
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
func NewBloomTokenizer(nGramLen, nGramSkip int, metrics *Metrics) *BloomTokenizer {
	// TODO(chaudum): Replace logger
	level.Info(util_log.Logger).Log("msg", "create new bloom tokenizer", "ngram length", nGramLen, "ngram skip", nGramSkip)
	return &BloomTokenizer{
		metrics:       metrics,
		cache:         make(map[string]interface{}, cacheSize),
		lineTokenizer: NewNGramTokenizer(nGramLen, nGramSkip),
	}
}

func (bt *BloomTokenizer) GetNGramLength() uint64 {
	return uint64(bt.lineTokenizer.N)
}

func (bt *BloomTokenizer) GetNGramSkip() uint64 {
	return uint64(bt.lineTokenizer.Skip)
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

	enc.PutBytes(make([]byte, ngram*MaxRuneLen)) // ensure enough capacity for the ngram

	// return the underlying byte slice and the length of the prefix
	return enc.Get(), prefixLn
}

// PopulateSeriesWithBloom is intended to be called on the write path, and is used to populate the bloom filter for a given series.
func (bt *BloomTokenizer) PopulateSeriesWithBloom(seriesWithBloom *SeriesWithBloom, chunks Iterator[[]chunk.Chunk]) error {
	startTime := time.Now().UnixMilli()
	level.Debug(util_log.Logger).Log("msg", "PopulateSeriesWithBloom")

	clearCache(bt.cache)
	chunkTotalUncompressedSize := 0

	for chunks.Next() {
		chunksBatch := chunks.At()
		for idx := range chunksBatch {
			lc := chunksBatch[idx].Data.(*chunkenc.Facade).LokiChunk()
			tokenBuf, prefixLn := prefixedToken(bt.lineTokenizer.N, chunksBatch[idx].ChunkRef)
			chunkTotalUncompressedSize += lc.UncompressedSize()

			itr, err := lc.Iterator(
				context.Background(),
				time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
				time.Unix(0, math.MaxInt64),
				logproto.FORWARD,
				log.NewNoopPipeline().ForStream(chunksBatch[idx].Metric),
			)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "chunk iterator cannot be created", "err", err)
				return err
			}

			defer itr.Close()

			for itr.Next() && itr.Error() == nil {
				chunkTokenizer := NewPrefixedTokenIter(tokenBuf, prefixLn, bt.lineTokenizer.Tokens(itr.Entry().Line))
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
			seriesWithBloom.Series.Chunks = append(seriesWithBloom.Series.Chunks, ChunkRef{
				Start:    chunksBatch[idx].From,
				End:      chunksBatch[idx].Through,
				Checksum: chunksBatch[idx].Checksum,
			})
		} // for each chunk
	}
	if err := chunks.Err(); err != nil {
		level.Error(util_log.Logger).Log("msg", "error downloading chunks batch", "err", err)
		return fmt.Errorf("error downloading chunks batch: %w", err)
	}

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

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}
