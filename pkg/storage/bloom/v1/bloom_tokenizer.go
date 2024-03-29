package v1

import (
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/dskit/multierror"

	"github.com/grafana/loki/pkg/iter"

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
// If the buffer is nil or too small, a new one is created. The buffer is returned for reuse.
func prefixedToken(ngram int, chk ChunkRef, buf []byte) ([]byte, int) {
	enc := encoding.EncWith(buf)
	enc.Reset()
	enc.PutBE64(uint64(chk.From))
	enc.PutBE64(uint64(chk.Through))
	enc.PutBE32(chk.Checksum)
	prefixLn := enc.Len() // record the length of the prefix

	// If the buffer is too small, ensure enough capacity for the ngram
	if cap(enc.Get()) < prefixLn+ngram*MaxRuneLen {
		enc.PutBytes(make([]byte, ngram*MaxRuneLen))
	}

	// return the underlying byte slice and the length of the prefix
	return enc.Get(), prefixLn
}

// ChunkRefWithIter is a wrapper around a ChunkRef and an EntryIterator.
type ChunkRefWithIter struct {
	Ref ChunkRef
	Itr iter.EntryIterator
}

// Populate adds the tokens from the given chunks to the given seriesWithBloom.
func (bt *BloomTokenizer) Populate(swb *SeriesWithBloom, chks Iterator[ChunkRefWithIter]) (int, error) {
	startTime := time.Now().UnixMilli()

	clearCache(bt.cache)

	var (
		tokenBuf []byte
		prefixLn int
		// TODO(owen-d): slightly more efficient to expose the
		// UncompressedSize() method on the chunk interface and use that
		sourceBytes int // source bytes processed
	)
	// Iterate over chunks
	for chks.Next() && chks.Err() == nil {

		var (
			tokens                 int
			successfulInserts      int
			cachedInserts          int
			collisionInserts       int
			chunkSuccessfulInserts int
			chunkCachedInserts     int
			chunkCollisionInserts  int
			chk                    = chks.At()
			itr                    = chk.Itr
		)
		tokenBuf, prefixLn = prefixedToken(bt.lineTokenizer.N, chk.Ref, tokenBuf)

		// Iterate over lines in the chunk
		for itr.Next() && itr.Error() == nil {
			// TODO(owen-d): rather than iterate over the line twice, once for prefixed tokenizer & once for
			// raw tokenizer, we could iterate once and just return (prefix, token) pairs from the tokenizer.
			// Double points for them being different-ln references to the same data.
			line := itr.Entry().Line
			sourceBytes += len(line)
			chunkTokenizer := NewPrefixedTokenIter(tokenBuf, prefixLn, bt.lineTokenizer.Tokens(line))
			for chunkTokenizer.Next() {
				tok := chunkTokenizer.At()
				tokens++
				// TODO(owen-d): [n]byte this
				str := string(tok)
				_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
				if found {
					cachedInserts++
					continue
				}

				bt.cache[str] = nil
				collision := swb.Bloom.ScalableBloomFilter.TestAndAdd(tok)
				if collision {
					collisionInserts++
				} else {
					successfulInserts++
				}

				if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
					clearCache(bt.cache)
				}
			}

			lineTokenizer := bt.lineTokenizer.Tokens(line)
			for lineTokenizer.Next() {
				tok := lineTokenizer.At()
				tokens++
				str := string(tok)
				_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
				if found {
					chunkCachedInserts++
					continue
				}
				bt.cache[str] = nil

				collision := swb.Bloom.ScalableBloomFilter.TestAndAdd(tok)
				if collision {
					chunkCollisionInserts++
				} else {
					chunkSuccessfulInserts++
				}

				if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
					clearCache(bt.cache)
				}
			}

		}
		var es multierror.MultiError
		if err := itr.Close(); err != nil {
			es.Add(errors.Wrapf(err, "error closing chunk: %#v", chk.Ref))
		}
		if err := itr.Error(); err != nil {
			es.Add(errors.Wrapf(err, "error iterating chunk: %#v", chk.Ref))
		}
		if combined := es.Err(); combined != nil {
			return sourceBytes, combined
		}
		swb.Series.Chunks = append(swb.Series.Chunks, chk.Ref)

		// update metrics after each chunk added for more consistent reporting
		bt.metrics.tokensTotal.Add(float64(tokens))
		bt.metrics.insertsTotal.WithLabelValues(tokenTypeRaw, collisionTypeFalse).Add(float64(successfulInserts))
		bt.metrics.insertsTotal.WithLabelValues(tokenTypeRaw, collisionTypeCache).Add(float64(cachedInserts))
		bt.metrics.insertsTotal.WithLabelValues(tokenTypeRaw, collisionTypeTrue).Add(float64(collisionInserts))
		bt.metrics.insertsTotal.WithLabelValues(tokenTypeChunkPrefixed, collisionTypeFalse).Add(float64(chunkSuccessfulInserts))
		bt.metrics.insertsTotal.WithLabelValues(tokenTypeChunkPrefixed, collisionTypeCache).Add(float64(chunkCachedInserts))
		bt.metrics.insertsTotal.WithLabelValues(tokenTypeChunkPrefixed, collisionTypeTrue).Add(float64(chunkCollisionInserts))
	}

	if err := chks.Err(); err != nil {
		level.Error(util_log.Logger).Log("msg", "error downloading chunks batch", "err", err)
		return sourceBytes, fmt.Errorf("error downloading chunks batch: %w", err)
	}

	endTime := time.Now().UnixMilli()

	fillRatio := swb.Bloom.ScalableBloomFilter.FillRatio()
	bt.metrics.hammingWeightRatio.Observe(fillRatio)
	bt.metrics.estimatedCount.Observe(
		float64(estimatedCount(swb.Bloom.ScalableBloomFilter.Capacity(), fillRatio)),
	)
	bt.metrics.bloomSize.Observe(float64(swb.Bloom.ScalableBloomFilter.Capacity() / eightBits))
	bt.metrics.sbfCreationTime.Add(float64(endTime - startTime))
	return sourceBytes, nil
}

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}
