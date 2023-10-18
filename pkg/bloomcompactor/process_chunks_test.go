package bloomcompactor

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/push"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

var (
	userID            = "userID"
	from   model.Time = model.TimeFromUnixNano(0)
	to     model.Time = model.TimeFromUnixNano(0)

	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
)

func Test_CompactChunks(t *testing.T) {
	lbs := labels.FromStrings("foo", "bar")
	fp := model.Fingerprint(lbs.Hash())
	// todo initialize a bloom for a series.
	bloomForChunks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: fp,
		},
		Bloom: &v1.Bloom{
			Sbf: *filter.NewDefaultScalableBloomFilter(0.01),
		},
	}
	// 1. create real chunks
	// 2. create SeriesWithBloom
	var (
		chunks []chunk.Chunk = make([]chunk.Chunk, 1)
		refs   []v1.ChunkRef = make([]v1.ChunkRef, 1)
	)

	memChk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), testBlockSize, testTargetSize)
	memChk.Append(&push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      "foo",
	})

	for i := range chunks {
		chunks[i] = chunk.NewChunk(userID, fp, lbs, chunkenc.NewFacade(memChk, testBlockSize, testTargetSize), from, to)
		require.NoError(t, chunks[i].Encode())
		refs[i] = v1.ChunkRef{
			Start:    chunks[i].From,
			End:      chunks[i].Through,
			Checksum: chunks[i].Checksum,
		}
	}
	// that's what we test
	for _, c := range chunks {
		fillBloom(bloomForChunks, c, SpaceTokenizer)
	}

	blockDir := t.TempDir()
	// Write the block to disk
	writer := v1.NewDirectoryBlockWriter(blockDir)
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), writer)
	require.NoError(t, err)
	err = builder.BuildFrom(v1.NewSliceIter([]v1.SeriesWithBloom{bloomForChunks}))
	require.NoError(t, err)

	// readn and verify the data.
	querier := v1.NewBlockQuerier(v1.NewBlock(v1.NewDirectoryBlockReader(blockDir)))

	matches, err := querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("foo")})
	require.NoError(t, err)
	require.Equal(t, 1, len(matches))

	matches, err = querier.CheckChunksForSeries(fp, refs, [][]byte{[]byte("bar")})
	require.NoError(t, err)
	require.Equal(t, 0, len(matches))
}

type TokenizerFunc func(e logproto.Entry) [][]byte

var SpaceTokenizer TokenizerFunc = func(e logproto.Entry) [][]byte {
	// todo verify []byte(e.Line) doesn't do an allocation.
	return bytes.Split([]byte(e.Line), []byte(` `))
}

func fillBloom(b v1.SeriesWithBloom, c chunk.Chunk, tokenizer TokenizerFunc) error {
	itr, err := c.Data.(*chunkenc.Facade).LokiChunk().Iterator(
		context.Background(),
		time.Unix(0, 0),
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		log.NewNoopPipeline().ForStream(c.Metric),
	)
	if err != nil {
		return err
	}
	// todo log error of close
	defer itr.Close()
	for itr.Next() {
		for _, t := range tokenizer(itr.Entry()) {
			b.Bloom.Sbf.Add(t)
		}
	}
	if err := itr.Error(); err != nil {
		return err
	}
	b.Series.Chunks = append(b.Series.Chunks, v1.ChunkRef{
		Start:    c.From,
		End:      c.Through,
		Checksum: c.Checksum,
	})
	return nil
}
