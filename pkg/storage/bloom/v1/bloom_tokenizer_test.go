package v1

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	logger "github.com/go-kit/log"
	"github.com/grafana/dskit/multierror"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/iter"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"

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
	t.Parallel()
	var ones uint64 = 0xffffffffffffffff

	ref := ChunkRef{
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
			token, prefixLn := prefixedToken(tc.ngram, ref, nil)
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
	t.Parallel()
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, metrics, logger.NewNopLogger())

	// Validate defaults
	require.Equal(t, bt.lineTokenizer.N(), DefaultNGramLength)
	require.Equal(t, bt.lineTokenizer.SkipFactor(), DefaultNGramSkip)

	// Set new tokenizer, and validate against that
	bt.lineTokenizer = NewNGramTokenizer(6, 7)
	require.Equal(t, bt.lineTokenizer.N(), 6)
	require.Equal(t, bt.lineTokenizer.SkipFactor(), 7)
}

func TestTokenizerPopulate(t *testing.T) {
	t.Parallel()
	testLine := "this is a log line"
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, metrics, logger.NewNopLogger())

	sbf := filter.NewScalableBloomFilter(1024, 0.01, 0.8)

	memChunk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), 256000, 1500000)
	_, _ = memChunk.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      testLine,
	})
	itr, err := memChunk.Iterator(
		context.Background(),
		time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		log.NewNoopPipeline().ForStream(nil),
	)
	require.Nil(t, err)

	bloom := Bloom{
		ScalableBloomFilter: *sbf,
	}

	blooms, err := populateAndConsumeBloom(
		bt,
		v2.NewSliceIter([]*Bloom{&bloom}),
		v2.NewSliceIter([]ChunkRefWithIter{{
			Ref: ChunkRef{},
			Itr: itr,
		}}),
	)
	require.NoError(t, err)
	require.Equal(t, 1, len(blooms))

	tokenizer := NewNGramTokenizer(DefaultNGramLength, DefaultNGramSkip)
	toks := tokenizer.Tokens(testLine)
	for toks.Next() {
		token := toks.At()
		require.True(t, blooms[0].Test(token))
	}
}

func TestBloomTokenizerPopulateWithoutPreexistingBloom(t *testing.T) {
	testLine := "this is a log line"
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, metrics, logger.NewNopLogger())

	memChunk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), 256000, 1500000)
	_, _ = memChunk.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      testLine,
	})
	itr, err := memChunk.Iterator(
		context.Background(),
		time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		log.NewNoopPipeline().ForStream(nil),
	)
	require.Nil(t, err)

	blooms, err := populateAndConsumeBloom(
		bt,
		v2.NewEmptyIter[*Bloom](),
		v2.NewSliceIter([]ChunkRefWithIter{{
			Ref: ChunkRef{},
			Itr: itr,
		}}),
	)
	require.NoError(t, err)
	require.Equal(t, 1, len(blooms))

	tokenizer := NewNGramTokenizer(DefaultNGramLength, DefaultNGramSkip)
	toks := tokenizer.Tokens(testLine)
	for toks.Next() {
		token := toks.At()
		require.True(t, blooms[0].Test(token))
	}
}

func chunkRefItrFromLines(lines ...string) (iter.EntryIterator, error) {
	memChunk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), 256000, 1500000)
	for i, line := range lines {
		if _, err := memChunk.Append(&push.Entry{
			Timestamp: time.Unix(0, int64(i)),
			Line:      line,
		}); err != nil {
			return nil, err
		}
	}

	itr, err := memChunk.Iterator(
		context.Background(),
		time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		log.NewNoopPipeline().ForStream(nil),
	)
	return itr, err
}

func randomStr(ln int) string {
	rng := rand.New(rand.NewSource(0))
	charset := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_!@#$%^&*() ")

	res := make([]rune, ln)
	for i := 0; i < ln; i++ {
		res[i] = charset[rng.Intn(len(charset))]
	}
	return string(res)
}

func TestTokenizerPopulateWontExceedMaxSize(t *testing.T) {
	maxSize := 2048
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, maxSize, NewMetrics(nil), logger.NewNopLogger())
	ch := make(chan *BloomCreation)
	line := randomStr(10e3)
	itr, err := chunkRefItrFromLines(line)
	require.NoError(t, err)
	go bt.Populate(
		v2.NewSliceIter([]*Bloom{
			{
				*filter.NewScalableBloomFilter(1024, 0.01, 0.8),
			},
		}),
		v2.NewSliceIter([]ChunkRefWithIter{
			{
				Ref: ChunkRef{},
				Itr: itr,
			},
		}),
		ch,
	)

	var ct int
	for created := range ch {
		ct++
		capacity := created.Bloom.ScalableBloomFilter.Capacity() / 8
		require.Less(t, int(capacity), maxSize)
	}
	// ensure we created two bloom filters from this dataset
	require.Equal(t, 2, ct)
}

func populateAndConsumeBloom(
	bt *BloomTokenizer,
	blooms v2.SizedIterator[*Bloom],
	chks v2.Iterator[ChunkRefWithIter],
) (res []*Bloom, err error) {
	var e multierror.MultiError
	ch := make(chan *BloomCreation)
	go bt.Populate(blooms, chks, ch)
	for x := range ch {
		if x.Err != nil {
			e = append(e, x.Err)
		} else {
			res = append(res, x.Bloom)
		}
	}
	return res, e.Err()
}

func BenchmarkPopulateSeriesWithBloom(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testLine := lorem + lorem + lorem
		bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, metrics, logger.NewNopLogger())

		sbf := filter.NewScalableBloomFilter(1024, 0.01, 0.8)

		memChunk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), 256000, 1500000)
		_, _ = memChunk.Append(&push.Entry{
			Timestamp: time.Unix(0, 1),
			Line:      testLine,
		})
		itr, err := memChunk.Iterator(
			context.Background(),
			time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
			time.Unix(0, math.MaxInt64),
			logproto.FORWARD,
			log.NewNoopPipeline().ForStream(nil),
		)
		require.Nil(b, err)

		bloom := Bloom{
			ScalableBloomFilter: *sbf,
		}

		_, err = populateAndConsumeBloom(
			bt,
			v2.NewSliceIter([]*Bloom{&bloom}),
			v2.NewSliceIter([]ChunkRefWithIter{{
				Ref: ChunkRef{},
				Itr: itr,
			}}),
		)
		require.NoError(b, err)
	}
}

func TestTokenizerClearsCacheBetweenPopulateCalls(t *testing.T) {
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, NewMetrics(nil), logger.NewNopLogger())
	line := "foobarbazz"
	var blooms []*Bloom

	for i := 0; i < 2; i++ {
		ch := make(chan *BloomCreation)
		itr, err := chunkRefItrFromLines(line)
		require.NoError(t, err)
		go bt.Populate(
			v2.NewEmptyIter[*Bloom](),
			v2.NewSliceIter([]ChunkRefWithIter{
				{
					Ref: ChunkRef{},
					Itr: itr,
				},
			}),
			ch,
		)
		var ct int
		for created := range ch {
			blooms = append(blooms, created.Bloom)
			ct++
		}
		// ensure we created one bloom for each call
		require.Equal(t, 1, ct)

	}

	for _, bloom := range blooms {
		toks := bt.lineTokenizer.Tokens(line)
		for toks.Next() {
			token := toks.At()
			require.True(t, bloom.Test(token))
		}
		require.NoError(t, toks.Err())
	}
}

func BenchmarkMapClear(b *testing.B) {
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, metrics, logger.NewNopLogger())
	for i := 0; i < b.N; i++ {
		for k := 0; k < cacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		clear(bt.cache)
	}
}

func BenchmarkNewMap(b *testing.B) {
	bt := NewBloomTokenizer(DefaultNGramLength, DefaultNGramSkip, 0, metrics, logger.NewNopLogger())
	for i := 0; i < b.N; i++ {
		for k := 0; k < cacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		bt.cache = make(map[string]interface{}, cacheSize)
	}
}
