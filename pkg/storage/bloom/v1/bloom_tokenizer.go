package v1

import (
	"fmt"
	"math"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/dskit/multierror"

	"github.com/grafana/loki/v3/pkg/iter"

	"github.com/grafana/loki/v3/pkg/util/encoding"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

/*
BloomTokenizer is a utility that converts either Loki chunks or individual lines into tokens.
These tokens are n-grams, representing adjacent letters, that are used to populate a bloom filter.
https://en.wikipedia.org/wiki/Bloom_filter
Bloom filters are utilized for faster lookups of log lines.
*/
type BloomTokenizer struct {
	metrics *Metrics

	maxBloomSize  int
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
func NewBloomTokenizer(nGramLen, nGramSkip int, maxBloomSize int, metrics *Metrics) *BloomTokenizer {
	// TODO(chaudum): Replace logger
	level.Info(util_log.Logger).Log("msg", "create new bloom tokenizer", "ngram length", nGramLen, "ngram skip", nGramSkip)
	return &BloomTokenizer{
		metrics:       metrics,
		cache:         make(map[string]interface{}, cacheSize),
		lineTokenizer: NewNGramTokenizer(nGramLen, nGramSkip),
		maxBloomSize:  maxBloomSize,
	}
}

func (bt *BloomTokenizer) N() uint64 {
	return uint64(bt.lineTokenizer.N())
}

func (bt *BloomTokenizer) SkipFactor() uint64 {
	return uint64(bt.lineTokenizer.SkipFactor())
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
// The `skip` return value indicates whether this series should be discarded and is used to short-circuit
// bloom generation for series that are too large. We will undoubtedly improve this in the future.
func (bt *BloomTokenizer) Populate(swb *SeriesWithBlooms, chks Iterator[ChunkRefWithIter]) (bytesAdded int, skip bool, err error) {
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
			chunkBytes             int
			chk                    = chks.At()
			itr                    = chk.Itr
		)
		tokenBuf, prefixLn = prefixedToken(bt.lineTokenizer.N(), chk.Ref, tokenBuf)

		// Iterate over lines in the chunk
	entries:
		for itr.Next() && itr.Error() == nil {
			// TODO(owen-d): rather than iterate over the line twice, once for prefixed tokenizer & once for
			// raw tokenizer, we could iterate once and just return (prefix, token) pairs from the tokenizer.
			// Double points for them being different-ln references to the same data.
			line := itr.Entry().Line
			chunkBytes += len(line)

			tokenItrs := []Iterator[[]byte]{
				// two iterators, one for the raw tokens and one for the chunk prefixed tokens.
				// Warning: the underlying line tokenizer (used in both iterators) uses the same buffer for tokens.
				// They are NOT SAFE for concurrent use.
				NewPrefixedTokenIter(tokenBuf, prefixLn, bt.lineTokenizer.Tokens(line)),
				bt.lineTokenizer.Tokens(line),
			}

			for _, itr := range tokenItrs {
				for itr.Next() {
					tok := itr.At()
					tokens++
					// TODO(owen-d): [n]byte this
					str := string(tok)
					_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
					if found {
						cachedInserts++
						continue
					}

					bt.cache[str] = nil
					collision, sz := swb.Blooms.ScalableBloomFilter.HeavyAdd(tok)
					if collision {
						collisionInserts++
					} else {
						successfulInserts++
					}

					if bt.maxBloomSize > 0 && sz > bt.maxBloomSize {
						skip = true
						break entries
					}

					if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
						clearCache(bt.cache)
					}
				}

			}
		}

		// add the recorded chunkbytes to the sourcebytes counter in case we return early via error
		sourceBytes += chunkBytes

		var es multierror.MultiError
		if err := itr.Close(); err != nil {
			es.Add(errors.Wrapf(err, "error closing chunk: %#v", chk.Ref))
		}
		if err := itr.Error(); err != nil {
			es.Add(errors.Wrapf(err, "error iterating chunk: %#v", chk.Ref))
		}
		if combined := es.Err(); combined != nil {
			return sourceBytes, skip, combined
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
		bt.metrics.sourceBytesAdded.Add(float64(chunkBytes))

		// Exit early if the series is too large
		if skip {
			break
		}
	}

	if err := chks.Err(); err != nil {
		level.Error(util_log.Logger).Log("msg", "error downloading chunks batch", "err", err)
		return sourceBytes, skip, fmt.Errorf("error downloading chunks batch: %w", err)
	}

	level.Debug(util_log.Logger).Log(
		"msg", "bloom filter populated",
		"chunks", len(swb.Series.Chunks),
		"fp", swb.Series.Fingerprint,
		"sourceBytes", datasize.ByteSize(sourceBytes).HumanReadable(),
		"bloomSize", datasize.ByteSize(swb.Blooms.Capacity()/8).HumanReadable(),
		"skipped", skip,
	)

	endTime := time.Now().UnixMilli()

	fillRatio := swb.Blooms.ScalableBloomFilter.FillRatio()
	bt.metrics.hammingWeightRatio.Observe(fillRatio)
	bt.metrics.estimatedCount.Observe(
		float64(estimatedCount(swb.Blooms.ScalableBloomFilter.Capacity(), fillRatio)),
	)
	bt.metrics.bloomSize.Observe(float64(swb.Blooms.ScalableBloomFilter.Capacity() / eightBits))

	ty := bloomCreationTypeIndexed
	if skip {
		ty = bloomCreationTypeSkipped
	}
	bt.metrics.sbfCreationTime.WithLabelValues(ty).Add(float64(endTime - startTime))
	bt.metrics.bloomsTotal.WithLabelValues(ty).Inc()

	return sourceBytes, skip, nil
}

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}
