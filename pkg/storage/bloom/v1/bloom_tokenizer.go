package v1

import (
	"context"
	"math"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
	//"github.com/grafana/loki/tools/tsdb/helpers"
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

	lineTokenizer    Tokenizer
	chunkIDTokenizer *WrappedTokenizer
	cache            map[string]interface{}
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
	t.lineTokenizer = NewNGramTokenizer(DefaultNGramLength, DefaultNGramLength+1, DefaultNGramSkip) // default to 4-grams, no skip
	t.chunkIDTokenizer = ChunkIDTokenizer(t.lineTokenizer)

	level.Info(util_log.Logger).Log("bloom tokenizer created")

	return t, nil
}

func (bt *BloomTokenizer) SetLineTokenizer(t Tokenizer) {
	bt.lineTokenizer = t
	bt.chunkIDTokenizer = ChunkIDTokenizer(bt.lineTokenizer)
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

func (bt *BloomTokenizer) PopulateSeriesWithBloom(seriesWithBloom *SeriesWithBloom, chunks []chunk.Chunk) {
	clearCache(bt.cache)
	for idx := range chunks {
		lc := chunks[idx].Data.(*chunkenc.Facade).LokiChunk()
		bt.chunkIDTokenizer.Reinit(chunks[idx].ChunkRef)

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
			toks := bt.chunkIDTokenizer.Tokens(itr.Entry().Line)

			for _, tok := range toks {
				if tok.Key != nil {
					str := string(tok.Key)
					_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
					if !found {
						bt.cache[str] = nil

						seriesWithBloom.Bloom.ScalableBloomFilter.TestAndAdd(tok.Key)

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

// TokenizeLine returns a slice of tokens for the given line, based on the current value of the tokenizer
// If the tokenizer has a skip value, then the line will be tokenized multiple times,
// starting at the beginning of the line, with "skip" number of iterations, offset by one each time
// Each offset is kept as a separate slice of tokens, and all are returned in a slice of slices
func (bt *BloomTokenizer) TokenizeLine(line string) [][]Token {
	allTokens := make([][]Token, 0, 10)
	if len(line) >= bt.lineTokenizer.GetMin() && len(line) >= bt.lineTokenizer.GetSkip() {
		for i := 0; i <= bt.lineTokenizer.GetSkip(); i++ {
			tmpTokens := make([]Token, 0, 100)
			tokens := bt.lineTokenizer.Tokens(line[i:])
			for _, token := range tokens {
				tmpToken := Token{}
				tmpToken.Key = make([]byte, len(token.Key))
				copy(tmpToken.Key, token.Key)
				tmpTokens = append(tmpTokens, tmpToken)
			}
			if len(tokens) > 0 {
				allTokens = append(allTokens, tmpTokens)
			}
		}
	}
	return allTokens
}
