package v1

import (
	"math"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/iter"
	v2iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/encoding"

	"github.com/grafana/loki/pkg/push"
)

/*
BloomTokenizer is a utility that converts either Loki chunks or individual lines into tokens.
These tokens are n-grams, representing adjacent letters, that are used to populate a bloom filter.
https://en.wikipedia.org/wiki/Bloom_filter
Bloom filters are utilized for faster lookups of log lines.
*/
type BloomTokenizer struct {
	metrics *Metrics
	logger  log.Logger

	maxBloomSize  int // size in bytes
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
func NewBloomTokenizer(nGramLen, nGramSkip int, maxBloomSize int, metrics *Metrics, logger log.Logger) *BloomTokenizer {
	level.Info(logger).Log("msg", "create new bloom tokenizer", "ngram length", nGramLen, "ngram skip", nGramSkip)
	return &BloomTokenizer{
		metrics:       metrics,
		logger:        logger,
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

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}

// Populates a bloom filter(s) with the tokens from the given chunks.
// Called once per series
func (bt *BloomTokenizer) Populate(blooms v2iter.SizedIterator[*Bloom], chks v2iter.Iterator[ChunkRefWithIter], ch chan *BloomCreation) {
	clear(bt.cache) // MUST always clear the cache before starting a new series
	var next bool

	// All but the last bloom are considered full -- send back unaltered
	for next = blooms.Next(); next && blooms.Remaining() > 0; next = blooms.Next() {
		ch <- &BloomCreation{
			Bloom: blooms.At(),
			Info:  newIndexingInfo(),
		}
	}

	var bloom *Bloom
	if next {
		// The last bloom has been made available via the `Next()` call above
		bloom = blooms.At()

		// TODO(salvacorts): Delete this once we solve the correctness bug
		// We noticed some blooms are empty on the resulting blocks.
		// We have the feeling that the empty blooms may be reused from old blocks.
		// Here we log an error if we find an empty bloom.
		if bloom.Count() == 0 {
			level.Warn(bt.logger).Log("msg", "found existing empty bloom")
		}
	} else {
		bloom = NewBloom()
	}

	info := newIndexingInfo()

	for chks.Next() {
		chk := chks.At()
		itr := v2iter.NewPeekIter(chk.Itr)

		for {
			full, chunkStats := bt.addChunkToBloom(bloom, chk.Ref, itr)
			info = info.merge(chunkStats)

			// If a bloom is full, the chunk wasn't completely added
			// so we'll submit this bloom, start a new one, and continue indexing
			if full {
				bt.sendBloom(ch, bloom, info)

				// start a new bloom + reset stats
				info = newIndexingInfo()
				bloom = NewBloom()

				// cache _MUST_ be cleared when a new bloom is created to ensure that all tokens from
				// each line are indexed into at least one bloom
				clear(bt.cache)
				continue
			}

			break
		}
	}

	// TODO(salvacorts): Delete this once we solve the correctness bug
	if bloom.Count() == 0 {
		level.Warn(bt.logger).Log("msg", "resulting bloom is empty")
	}

	// Send the last bloom
	bt.sendBloom(ch, bloom, info)
	close(ch)
}

func (bt *BloomTokenizer) sendBloom(ch chan<- *BloomCreation, bloom *Bloom, info indexingInfo) {
	fillRatio := bloom.ScalableBloomFilter.FillRatio()
	bt.metrics.hammingWeightRatio.Observe(fillRatio)
	bt.metrics.estimatedCount.Observe(
		float64(estimatedCount(bloom.ScalableBloomFilter.Capacity(), fillRatio)),
	)
	bt.metrics.bloomSize.Observe(float64(bloom.ScalableBloomFilter.Capacity() / eightBits))
	bt.metrics.bloomsTotal.Inc()
	ch <- &BloomCreation{
		Bloom: bloom,
		Info:  info,
	}
}

func prefixForChunkRef(chk ChunkRef) []byte {
	enc := encoding.EncWith(make([]byte, 0, 20))
	enc.PutBE64(uint64(chk.From))    // 8 bytes
	enc.PutBE64(uint64(chk.Through)) // 8 bytes
	enc.PutBE32(chk.Checksum)        // 4 bytes
	return enc.Get()
}

// addChunkToBloom adds the values from structured metadata from the entries of the given chunk to the given bloom.
// addChunkToBloom returns true if the bloom has been completely filled, and may not have consumed the entire iterator.
// addChunkToBloom must be called multiple times until returning false with new blooms until the iterator has been fully consumed.
func (bt *BloomTokenizer) addChunkToBloom(bloom *Bloom, ref ChunkRef, entryIter v2iter.PeekIterator[push.Entry]) (bool, indexingInfo) {
	var (
		tokens            int
		successfulInserts int
		cachedInserts     int
		collisionInserts  int
		linesAdded        int

		collision bool
	)

	// return values
	full, info := false, newIndexingInfo()

	tokenizer := NewStructuredMetadataTokenizer(string(prefixForChunkRef(ref)))

	// We use a peeking iterator to avoid advancing the iterator until we're sure the bloom has accepted the line.
	for entry, ok := entryIter.Peek(); ok; entry, ok = entryIter.Peek() {
		for _, kv := range entry.StructuredMetadata {
			info.sourceBytes += len(kv.Name) + len(kv.Value)
			info.indexedFields.Add(Field(kv.Name))

			tokenItr := tokenizer.Tokens(kv)
			for tokenItr.Next() {
				tok := tokenItr.At()
				tokens++

				// A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
				if _, found := bt.cache[tok]; found {
					cachedInserts++
					continue
				}

				// maxBloomSize is in bytes, but blooms operate at the bit level; adjust
				collision, full = bloom.ScalableBloomFilter.TestAndAddWithMaxSize([]byte(tok), bt.maxBloomSize*eightBits)

				if collision {
					collisionInserts++
				} else {
					successfulInserts++
				}

				// only register the key in the cache if it was successfully added to the bloom
				// as can prevent us from trying subsequent copies
				bt.cache[tok] = nil
				if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
					clear(bt.cache)
				}
			}
		}

		// Only advance the iterator once we're sure the bloom has accepted the line
		linesAdded++
		_ = entryIter.Next()

		// Only break out of the loop if the bloom filter is full after indexing all structured metadata of an entry.
		if full {
			break
		}
	}

	// update metrics after each chunk added for more consistent reporting
	bt.metrics.tokensTotal.Add(float64(tokens))
	bt.metrics.insertsTotal.WithLabelValues(collisionTypeFalse).Add(float64(successfulInserts))
	bt.metrics.insertsTotal.WithLabelValues(collisionTypeCache).Add(float64(cachedInserts))
	bt.metrics.insertsTotal.WithLabelValues(collisionTypeTrue).Add(float64(collisionInserts))
	bt.metrics.sourceBytesAdded.Add(float64(info.sourceBytes))

	return full, info
}
