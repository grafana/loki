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
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
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
	t.lineTokenizer = NewNGramTokenizer(4, 5, 0) // default to 4-grams, no skip
	t.chunkIDTokenizer = ChunkIDTokenizer(t.lineTokenizer)

	level.Info(util_log.Logger).Log("bloom tokenizer created")

	return t, nil
}

func (bt *BloomTokenizer) SetLineTokenizer(t Tokenizer) {
	bt.lineTokenizer = t
	bt.chunkIDTokenizer = ChunkIDTokenizer(bt.lineTokenizer)
}

// TODO: Something real here with metrics
func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{}
}

func clearCache(cache map[string]interface{}) {
	for k := range cache {
		delete(cache, k)
	}
}

func (bt *BloomTokenizer) PopulateSBF(sbf *filter.ScalableBloomFilter, chunks []chunk.Chunk) {
	clearCache(bt.cache)
	for idx := range chunks {
		lc := chunks[idx].Data.(*chunkenc.Facade).LokiChunk()
		bt.chunkIDTokenizer.Reinit(chunks[idx].ChunkRef)

		// TODO: error handling
		itr, _ := lc.Iterator(
			context.Background(),
			time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
			time.Unix(0, math.MaxInt64),
			logproto.FORWARD,
			log.NewNoopPipeline().ForStream(chunks[idx].Metric),
		)

		for itr.Next() && itr.Error() == nil {
			toks := bt.chunkIDTokenizer.Tokens(itr.Entry().Line)

			for _, tok := range toks {
				if tok.Key != nil {
					str := string(tok.Key)
					_, found := bt.cache[str] // A cache is used ahead of the SBF, as it cuts out the costly operations of scaling bloom filters
					if !found {
						bt.cache[str] = nil

						sbf.TestAndAdd(tok.Key)

						if len(bt.cache) > 150000 { // While crude, this has proven efficient in performance testing.  This speaks to the similarity in log lines near each other
							clearCache(bt.cache)
						}
					}
				}
			}
		}
	} // for each chunk
}

func (bt *BloomTokenizer) TokenizeLine(line string) []Token {
	return bt.lineTokenizer.Tokens(line)
}
