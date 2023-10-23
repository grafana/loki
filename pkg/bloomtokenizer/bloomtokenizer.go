package bloomtokenizer

import (
	"context"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/tools/tsdb/helpers"
	"github.com/prometheus/client_golang/prometheus"
	"math"
	"time"
)

type metrics struct{}

type BloomTokenizer struct {
	metrics *metrics

	lineTokenizer    Tokenizer
	chunkIdTokenizer *WrappedTokenizer
	cache            map[string]interface{}
}

const CacheSize = 150000

// New returns a new instance of the Bloom Tokenizer.
func NewBloomTokenizer(reg prometheus.Registerer) (*BloomTokenizer, error) {
	t := &BloomTokenizer{
		metrics: newMetrics(reg),
	}
	t.cache = make(map[string]interface{}, CacheSize)
	// TODO: make these configurable
	t.lineTokenizer = newNGramTokenizer(4, 5, 0)
	t.chunkIdTokenizer = ChunkIDTokenizer(t.lineTokenizer)

	level.Info(util_log.Logger).Log("bloom tokenizer created")

	return t, nil
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
		bt.chunkIdTokenizer.reinit(chunks[idx].ChunkRef)

		itr, err := lc.Iterator(
			context.Background(),
			time.Unix(0, 0),
			time.Unix(0, math.MaxInt64),
			logproto.FORWARD,
			log.NewNoopPipeline().ForStream(chunks[idx].Metric),
		)
		helpers.ExitErr("getting iterator", err)

		for itr.Next() && itr.Error() == nil {
			toks := bt.chunkIdTokenizer.Tokens(itr.Entry().Line)

			for _, tok := range toks {
				if tok.Key != nil {

					_, found := bt.cache[tok.Value]
					if !found {
						bt.cache[tok.Value] = nil

						sbf.TestAndAdd(tok.Key)

						if len(bt.cache) > 150000 {
							clearCache(bt.cache)
						}
					}

				}
			}
		}
		helpers.ExitErr("iterating chunks", itr.Error())
	} // for each chunk

}

func (bt *BloomTokenizer) TokenizeLine(line string) []Token {
	return bt.lineTokenizer.Tokens(line)
}
