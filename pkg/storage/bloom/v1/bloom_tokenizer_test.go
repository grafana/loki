package v1

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/pkg/storage/chunk"

	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	four = NewNGramTokenizer(4, 0)
)

func TestSetLineTokenizer(t *testing.T) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)

	// Validate defaults
	require.Equal(t, bt.lineTokenizer.N, DefaultNGramLength)
	require.Equal(t, bt.lineTokenizer.Skip, DefaultNGramSkip)

	// Set new tokenizer, and validate against that
	bt.SetLineTokenizer(NewNGramTokenizer(6, 7))
	require.Equal(t, bt.lineTokenizer.N, 6)
	require.Equal(t, bt.lineTokenizer.Skip, 7)
}

func TestPopulateSeriesWithBloom(t *testing.T) {
	var testLine = "this is a log line"
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)

	sbf := filter.NewScalableBloomFilter(1024, 0.01, 0.8)
	var lbsList []labels.Labels
	lbsList = append(lbsList, labels.FromStrings("foo", "bar"))

	var fpList []model.Fingerprint
	for i := range lbsList {
		fpList = append(fpList, model.Fingerprint(lbsList[i].Hash()))
	}

	var memChunks = make([]*chunkenc.MemChunk, 0)
	memChunk0 := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), 256000, 1500000)
	_ = memChunk0.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      testLine,
	})
	memChunks = append(memChunks, memChunk0)

	var chunks = make([]chunk.Chunk, 0)
	for i := range memChunks {
		chunks = append(chunks, chunk.NewChunk("user", fpList[i], lbsList[i], chunkenc.NewFacade(memChunks[i], 256000, 1500000), model.TimeFromUnixNano(0), model.TimeFromUnixNano(1)))
	}

	bloom := Bloom{
		ScalableBloomFilter: *sbf,
	}
	series := Series{
		Fingerprint: model.Fingerprint(lbsList[0].Hash()),
	}
	swb := SeriesWithBloom{
		Bloom:  &bloom,
		Series: &series,
	}

	bt.PopulateSeriesWithBloom(&swb, chunks)
	tokenizer := NewNGramTokenizer(DefaultNGramLength, DefaultNGramSkip)
	itr := tokenizer.Tokens(testLine)
	for itr.Next() {
		token := itr.At()
		require.True(t, swb.Bloom.Test(token))
	}
}

func BenchmarkMapClear(b *testing.B) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)
	for i := 0; i < b.N; i++ {
		for k := 0; k < CacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		clearCache(bt.cache)
	}
}

func BenchmarkNewMap(b *testing.B) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)
	for i := 0; i < b.N; i++ {
		for k := 0; k < CacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		bt.cache = make(map[string]interface{}, CacheSize)
	}
}
