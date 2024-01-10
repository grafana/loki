package v1

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/pkg/storage/chunk"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	DefaultNGramLength = 4
	DefaultNGramSkip   = 0
)

var (
	four    = NewNGramTokenizer(4, 0)
	metrics = NewMetrics(prometheus.DefaultRegisterer)
)

func TestPrefixedKeyCreation(t *testing.T) {
	var ones uint64 = 0xffffffffffffffff

	ref := logproto.ChunkRef{
		From:     0,
		Through:  model.Time(int64(ones)),
		Checksum: 0xffffffff,
	}
	for _, tc := range []struct {
		desc          string
		ngram, expLen int
	}{
		{
			desc:   "0-gram",
			ngram:  0,
			expLen: 20,
		},
		{
			desc:   "4-gram",
			ngram:  4,
			expLen: 20 + 4*MaxRuneLen,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			token, prefixLn := prefixedToken(tc.ngram, ref)
			require.Equal(t, 20, prefixLn)
			require.Equal(t, tc.expLen, len(token))
			// first 8 bytes should be zeros from `from`
			for i := 0; i < 8; i++ {
				require.Equal(t, byte(0), token[i])
			}
			// next 8 bytes should be ones from `through`
			for i := 8; i < 16; i++ {
				require.Equal(t, byte(255), token[i])
			}
			// next 4 bytes should be ones from `checksum`
			for i := 16; i < 20; i++ {
				require.Equal(t, byte(255), token[i])
			}
		})
	}
}

func TestSetLineTokenizer(t *testing.T) {
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, metrics)

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
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, metrics)

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

	err := bt.PopulateSeriesWithBloom(&swb, chunks)
	require.NoError(t, err)
	tokenizer := NewNGramTokenizer(DefaultNGramLength, DefaultNGramSkip)
	itr := tokenizer.Tokens(testLine)
	for itr.Next() {
		token := itr.At()
		require.True(t, swb.Bloom.Test(token))
	}
}

func BenchmarkPopulateSeriesWithBloom(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var testLine = lorem + lorem + lorem
		bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, metrics)

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

		err := bt.PopulateSeriesWithBloom(&swb, chunks)
		require.NoError(b, err)
	}
}

func BenchmarkMapClear(b *testing.B) {
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, metrics)
	for i := 0; i < b.N; i++ {
		for k := 0; k < cacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		clearCache(bt.cache)
	}
}

func BenchmarkNewMap(b *testing.B) {
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, metrics)
	for i := 0; i < b.N; i++ {
		for k := 0; k < cacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		bt.cache = make(map[string]interface{}, cacheSize)
	}
}
