package v1

import (
	"math"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"

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

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}

func (bt *BloomTokenizer) Populate(
	series *Series,
	blooms SizedIterator[*Bloom],
	chks Iterator[ChunkRefWithIter],
	ch chan<- *BloomCreation,
) {
	// All but the last bloom are considered full -- send back unaltered
	for blooms.Next() && blooms.Remaining() > 0 {
		ch <- &BloomCreation{
			Bloom:            blooms.At(),
			sourceBytesAdded: 0,
		}
	}

	// The last bloom has been made available via the `Next()` call above
	bloom := blooms.At()

	var bytesAdded int

	for chks.Next() {
		chk := chks.At()

		for {
			full, newBytes := bt.addChunkToBloom(bloom, series, chk)
			bytesAdded += newBytes

			// If a bloom is full, the chunk wasn't completely added
			// so we'll submit this bloom, start a new one, and continue indexing
			if full {
				ch <- &BloomCreation{
					Bloom:            bloom,
					sourceBytesAdded: bytesAdded,
				}

				// start a new bloom + reset bytesAdded counter
				bytesAdded = 0
				bloom = &Bloom{
					// TODO parameterise SBF options. fp_rate
					ScalableBloomFilter: *filter.NewScalableBloomFilter(1024, 0.01, 0.8),
				}
				continue
			}

			break
		}

	}

	// Send the last bloom
	ch <- &BloomCreation{
		Bloom:            bloom,
		sourceBytesAdded: bytesAdded,
	}
	return

}

// addChunkToBloom adds the tokens from the given chunk to the given bloom.
// It continues until the chunk is exhausted or the bloom is full.
func (bt *BloomTokenizer) addChunkToBloom(bloom *Bloom, series *Series, chk ChunkRefWithIter) (full bool, bytesAdded int) {
	var (
		tokenBuf, prefixLn = prefixedToken(bt.lineTokenizer.N(), chk.Ref, nil)
		tokens             int
		successfulInserts  int
		cachedInserts      int
		collisionInserts   int
		chunkBytes         int

		// We use a peeking iterator to avoid advancing the iterator until we're sure
		// the bloom has accepted the line
		entryIter = newPeekingEntryIterAdapter(chk.Itr)
	)

	// Iterate over lines in the chunk
	// for line, ok := entryIter.Peek
outer:
	for entry, ok := entryIter.Peek(); ok; entry, ok = entryIter.Peek() {
		line := entry.Line
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
				// TODO[owen-d]: [n]byte this
				str := string(tok)
				_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
				if found {
					cachedInserts++
					continue
				}

				// maxBloomSize is in bytes, but blooms operate at the bit level; adjust
				var collision bool
				collision, full = bloom.ScalableBloomFilter.TestAndAddWithMaxSize(tok, bt.maxBloomSize*eightBits)

				if full {
					break outer
				}

				if collision {
					collisionInserts++
				} else {
					successfulInserts++
				}

				// only register the key in the cache if it was successfully added to the bloom
				// as can prevent us from trying subsequent copies
				bt.cache[str] = nil
				if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
					clearCache(bt.cache)
				}
			}
		}

		// Only advance the iterator once we're sure the bloom has accepted the line
		_ = entryIter.Next()
	}

	// update metrics after each chunk added for more consistent reporting
	bt.metrics.tokensTotal.Add(float64(tokens))
	bt.metrics.insertsTotal.WithLabelValues(collisionTypeFalse).Add(float64(successfulInserts))
	bt.metrics.insertsTotal.WithLabelValues(collisionTypeCache).Add(float64(cachedInserts))
	bt.metrics.insertsTotal.WithLabelValues(collisionTypeTrue).Add(float64(collisionInserts))
	bt.metrics.sourceBytesAdded.Add(float64(chunkBytes))

	return full, chunkBytes
}

type entryIterAdapter struct {
	iter.EntryIterator
}

func (a entryIterAdapter) At() logproto.Entry {
	return a.EntryIterator.Entry()
}

func (a entryIterAdapter) Err() error {
	return a.EntryIterator.Error()
}

func newPeekingEntryIterAdapter(itr iter.EntryIterator) *PeekIter[logproto.Entry] {
	return NewPeekingIter[logproto.Entry](entryIterAdapter{itr})
}
