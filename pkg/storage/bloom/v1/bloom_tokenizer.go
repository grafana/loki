package v1

import (
	"math"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/iter"
	v2iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
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

func (bt *BloomTokenizer) newBloom() *Bloom {
	return &Bloom{
		// TODO parameterise SBF options. fp_rate
		ScalableBloomFilter: *filter.NewScalableBloomFilter(1024, 0.01, 0.8),
	}
}

// Populates a bloom filter(s) with the tokens from the given chunks.
// Called once per series
func (bt *BloomTokenizer) Populate(
	blooms v2iter.SizedIterator[*Bloom],
	chks v2iter.Iterator[ChunkRefWithIter],
	ch chan *BloomCreation,
) {
	clear(bt.cache) // MUST always clear the cache before starting a new series
	var next bool

	// All but the last bloom are considered full -- send back unaltered
	for next = blooms.Next(); next && blooms.Remaining() > 0; next = blooms.Next() {
		ch <- &BloomCreation{
			Bloom:            blooms.At(),
			SourceBytesAdded: 0,
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
			level.Warn(bt.logger).Log(
				"msg", "found existing empty bloom",
			)
		}
	} else {
		bloom = bt.newBloom()
	}

	var bytesAdded int

	for chks.Next() {
		chk := chks.At()
		itr := v2iter.NewPeekIter(chk.Itr)

		for {
			full, newBytes := bt.addChunkToBloom(
				bloom,
				chk.Ref,
				itr,
			)
			bytesAdded += newBytes

			// If a bloom is full, the chunk wasn't completely added
			// so we'll submit this bloom, start a new one, and continue indexing
			if full {
				bt.sendBloom(ch, bloom, bytesAdded)

				// start a new bloom + reset bytesAdded counter
				bytesAdded = 0
				bloom = bt.newBloom()

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
		level.Warn(bt.logger).Log(
			"msg", "resulting bloom is empty",
		)
	}

	// Send the last bloom
	bt.sendBloom(ch, bloom, bytesAdded)
	close(ch)
}

func (bt *BloomTokenizer) sendBloom(
	ch chan<- *BloomCreation,
	bloom *Bloom,
	bytesAdded int,
) {
	fillRatio := bloom.ScalableBloomFilter.FillRatio()
	bt.metrics.hammingWeightRatio.Observe(fillRatio)
	bt.metrics.estimatedCount.Observe(
		float64(estimatedCount(bloom.ScalableBloomFilter.Capacity(), fillRatio)),
	)
	bt.metrics.bloomSize.Observe(float64(bloom.ScalableBloomFilter.Capacity() / eightBits))
	bt.metrics.bloomsTotal.Inc()
	ch <- &BloomCreation{
		Bloom:            bloom,
		SourceBytesAdded: bytesAdded,
	}
}

// addChunkToBloom adds the tokens from the given chunk to the given bloom.
// It continues until the chunk is exhausted or the bloom is full.
// NB(owen-d): We ensure the invariant that each line is indexed entirely into at least one bloom.
// This includes both raw ngrams and chunk-prefixed ngrams and is why we use a peeking iterator --
// so we can advance the iterator only after we're sure the bloom has accepted the line.
// This is because the _line_ is the atom in Loki's data model and a query must either match (or not) an individual line.
// Therefore, we index entire lines into a bloom to ensure a lookups are accurate.
func (bt *BloomTokenizer) addChunkToBloom(bloom *Bloom, ref ChunkRef, entryIter v2iter.PeekIterator[push.Entry]) (full bool, bytesAdded int) {
	var (
		tokenBuf, prefixLn = prefixedToken(bt.lineTokenizer.N(), ref, nil)
		tokens             int
		successfulInserts  int
		cachedInserts      int
		collisionInserts   int
		chunkBytes         int
		linesAdded         int
	)

	// We use a peeking iterator to avoid advancing the iterator until we're sure the bloom has accepted the line.
outer:
	for entry, ok := entryIter.Peek(); ok; entry, ok = entryIter.Peek() {
		line := entry.Line
		chunkBytes += len(line)

		tokenItrs := []v2iter.Iterator[[]byte]{
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
				// To avoid allocations, an unsafe string can be used to check ownership in cache.
				str := unsafe.String(unsafe.SliceData(tok), len(tok))
				// A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
				if _, found := bt.cache[str]; found {
					cachedInserts++
					continue
				}

				// maxBloomSize is in bytes, but blooms operate at the bit level; adjust
				var collision bool
				collision, full = bloom.ScalableBloomFilter.TestAndAddWithMaxSize(tok, bt.maxBloomSize*eightBits)

				if full {
					// edge case: one line maxed out the bloom size -- retrying is futile
					// (and will loop endlessly), so we'll just skip indexing it
					if linesAdded == 0 {
						_ = entryIter.Next()
					}

					break outer
				}

				if collision {
					collisionInserts++
				} else {
					successfulInserts++
				}

				// only register the key in the cache if it was successfully added to the bloom
				// as can prevent us from trying subsequent copies
				str = string(tok)
				bt.cache[str] = nil
				if len(bt.cache) >= cacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
					clear(bt.cache)
				}
			}
		}

		// Only advance the iterator once we're sure the bloom has accepted the line
		linesAdded++
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
