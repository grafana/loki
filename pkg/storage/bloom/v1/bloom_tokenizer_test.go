package v1

import (
	"fmt"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/prometheus/prometheus/model/labels"
	"time"

	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSetLineTokenizer(t *testing.T) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)

	// Validate defaults
	require.Equal(t, bt.lineTokenizer.GetMin(), DefaultNGramLength)
	require.Equal(t, bt.lineTokenizer.GetMax(), DefaultNGramLength+1)
	require.Equal(t, bt.lineTokenizer.GetSkip(), DefaultNGramSkip)

	require.Equal(t, bt.chunkIDTokenizer.GetMin(), DefaultNGramLength)
	require.Equal(t, bt.chunkIDTokenizer.GetMax(), DefaultNGramLength+1)
	require.Equal(t, bt.chunkIDTokenizer.GetSkip(), DefaultNGramSkip)

	// Set new tokenizer, and validate against that
	bt.SetLineTokenizer(NewNGramTokenizer(6, 7, 2))
	require.Equal(t, bt.lineTokenizer.GetMin(), 6)
	require.Equal(t, bt.lineTokenizer.GetMax(), 7)
	require.Equal(t, bt.lineTokenizer.GetSkip(), 2)

	require.Equal(t, bt.chunkIDTokenizer.GetMin(), 6)
	require.Equal(t, bt.chunkIDTokenizer.GetMax(), 7)
	require.Equal(t, bt.chunkIDTokenizer.GetSkip(), 2)
}

func TestTokenizeLine(t *testing.T) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)

	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp: []Token{
				{Key: []byte("abcd")}},
		},
		{
			desc:  "uuid partial",
			input: "2b1a5e46-36a2-4",
			exp: []Token{
				{Key: []byte("2b1a")},
				{Key: []byte("b1a5")},
				{Key: []byte("1a5e")},
				{Key: []byte("a5e4")},
				{Key: []byte("5e46")},
				{Key: []byte("e46-")},
				{Key: []byte("46-3")},
				{Key: []byte("6-36")},
				{Key: []byte("-36a")},
				{Key: []byte("36a2")},
				{Key: []byte("6a2-")},
				{Key: []byte("a2-4")},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, bt.TokenizeLine(tc.input))
		})
	}
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
	memChunk0.Append(&push.Entry{
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
	tokens := bt.TokenizeLine(testLine)
	for _, token := range tokens {
		require.True(t, swb.Bloom.Test(token.Key))
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
