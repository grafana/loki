package v1

import (
	"context"
	"encoding/binary"
	"math"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"

	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type metrics struct{}

/*
BloomTokenizer is a utility that converts either Loki chunks or individual lines into tokens.
These tokens are n-grams, representing adjacent letters, that are used to populate a bloom filter.
https://en.wikipedia.org/wiki/Bloom_filter
Bloom filters are utilized for faster lookups of log lines.
*/
type BloomTokenizer struct {
	metrics *metrics

	lineTokenizer *NGramTokenizer
	cache         map[string]interface{}
}

const CacheSize = 150000
const DefaultNGramLength = 4
const DefaultNGramSkip = 0

// NewBloomTokenizer returns a new instance of the Bloom Tokenizer.
// Warning: the tokens returned use the same byte slice to reduce allocations. This has two consequences:
// 1) The token slices generated must not be mutated externally
// 2) The token slice must not be used after the next call to `Tokens()` as it will repopulate the slice.
// 2) This is not thread safe.
func NewBloomTokenizer(reg prometheus.Registerer) (*BloomTokenizer, error) {
	t := &BloomTokenizer{
		metrics: newMetrics(reg),
	}
	t.cache = make(map[string]interface{}, CacheSize)
	t.lineTokenizer = NewNGramTokenizer(DefaultNGramLength, DefaultNGramSkip) // default to 4-grams, no skip

	level.Info(util_log.Logger).Log("bloom tokenizer created")

	return t, nil
}

func (bt *BloomTokenizer) SetLineTokenizer(t *NGramTokenizer) {
	bt.lineTokenizer = t
}

// TODO: Something real here with metrics
func newMetrics(_ prometheus.Registerer) *metrics {
	return &metrics{}
}

func clearCache(cache map[string]interface{}) {
	for k := range cache {
		delete(cache, k)
	}
}

func calculatePrefix(chk logproto.ChunkRef) []byte {
	i64buf := make([]byte, binary.MaxVarintLen64)
	i32buf := make([]byte, 4)
	prefix := make([]byte, 32)

	binary.PutVarint(i64buf, int64(chk.From))
	prefix = append(prefix, i64buf...)
	binary.PutVarint(i64buf, int64(chk.Through))
	prefix = append(prefix, i64buf...)
	binary.LittleEndian.PutUint32(i32buf, chk.Checksum)
	prefix = append(prefix, i32buf...)

	return prefix
}

// PopulateSeriesWithBloom is intended to be called on the write path, and is used to populate the bloom filter for a given series.
func (bt *BloomTokenizer) PopulateSeriesWithBloom(seriesWithBloom *SeriesWithBloom, chunks []chunk.Chunk) {
	clearCache(bt.cache)
	for idx := range chunks {
		lc := chunks[idx].Data.(*chunkenc.Facade).LokiChunk()
		prefix := calculatePrefix(chunks[idx].ChunkRef)

		// TODO: error handling
		itr, err := lc.Iterator(
			context.Background(),
			time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
			time.Unix(0, math.MaxInt64),
			logproto.FORWARD,
			log.NewNoopPipeline().ForStream(chunks[idx].Metric),
		)
		if err != nil {
			level.Info(util_log.Logger).Log("chunk iterator cannot be created")
			return
		}

		defer itr.Close()

		for itr.Next() && itr.Error() == nil {
			chunkTokenizer := NewPrefixedTokenIter(prefix, bt.lineTokenizer.Tokens(itr.Entry().Line))
			for chunkTokenizer.Next() {
				tok := chunkTokenizer.At()
				if tok != nil {
					str := string(tok)
					_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
					if !found {
						bt.cache[str] = nil

						seriesWithBloom.Bloom.ScalableBloomFilter.TestAndAdd(tok)

						if len(bt.cache) >= CacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
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

						if len(bt.cache) >= CacheSize { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
							clearCache(bt.cache)
						}
					}
				}
			}

		}
		seriesWithBloom.Series.Chunks = append(seriesWithBloom.Series.Chunks, ChunkRef{
			Start:    chunks[idx].From,
			End:      chunks[idx].Through,
			Checksum: chunks[idx].Checksum,
		})
	} // for each chunk
}
