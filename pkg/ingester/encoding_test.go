package ingester

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func fillChunk(t testing.TB, c chunkenc.Chunk) {
	t.Helper()
	var i int64
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      "entry for line 0",
	}

	for c.SpaceFor(entry) {
		dup, err := c.Append(entry)
		require.False(t, dup)
		require.NoError(t, err)
		i++
		entry.Timestamp = time.Unix(0, i)
		entry.Line = fmt.Sprintf("entry for line %d", i)
	}
}

func dummyConf() *Config {
	var conf Config
	conf.BlockSize = 256 * 1024
	conf.TargetChunkSize = 1500 * 1024

	return &conf
}

func Test_EncodingChunks(t *testing.T) {
	for _, close := range []bool{true, false} {
		for _, tc := range []struct {
			desc string
			conf Config
		}{
			{
				// mostly for historical parity
				desc: "dummyConf",
				conf: *dummyConf(),
			},
			{
				desc: "default",
				conf: defaultIngesterTestConfig(t),
			},
		} {

			t.Run(fmt.Sprintf("%v-%s", close, tc.desc), func(t *testing.T) {
				conf := tc.conf
				c := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.GZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, conf.BlockSize, conf.TargetChunkSize)
				fillChunk(t, c)
				if close {
					require.Nil(t, c.Close())
				}

				from := []chunkDesc{
					{
						chunk: c,
					},
					// test non zero values
					{
						chunk:       c,
						closed:      true,
						synced:      true,
						flushed:     time.Unix(1, 0),
						lastUpdated: time.Unix(0, 1),
					},
				}
				there, err := toWireChunks(from, nil)
				require.Nil(t, err)
				chunks := make([]Chunk, 0, len(there))
				for _, c := range there {
					chunks = append(chunks, c.Chunk)

					// Ensure closed head chunks only contain the head metadata but no entries
					if close {
						const unorderedHeadSize = 2
						require.Equal(t, unorderedHeadSize, len(c.Head))
					} else {
						require.Greater(t, len(c.Head), 0)
					}
				}

				_, headfmt := defaultChunkFormat(t)

				backAgain, err := fromWireChunks(&conf, headfmt, chunks)
				require.Nil(t, err)

				for i, to := range backAgain {
					// test the encoding directly as the substructure may change.
					// for instance the uncompressed size for each block is not included in the encoded version.
					enc, err := to.chunk.Bytes()
					require.Nil(t, err)
					to.chunk = nil

					matched := from[i]
					exp, err := matched.chunk.Bytes()
					require.Nil(t, err)
					matched.chunk = nil

					require.Equal(t, exp, enc)
					require.Equal(t, matched, to)

				}

			})
		}
	}
}

func Test_EncodingCheckpoint(t *testing.T) {
	conf := dummyConf()
	c := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.GZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, conf.BlockSize, conf.TargetChunkSize)
	dup, err := c.Append(&logproto.Entry{
		Timestamp: time.Unix(1, 0),
		Line:      "hi there",
	})
	require.False(t, dup)
	require.Nil(t, err)
	data, err := c.Bytes()
	require.Nil(t, err)
	from, to := c.Bounds()

	ls := labels.FromMap(map[string]string{"foo": "bar"})
	s := &Series{
		UserID:      "fake",
		Fingerprint: 123,
		Labels:      logproto.FromLabelsToLabelAdapters(ls),
		To:          time.Unix(10, 0),
		LastLine:    "lastLine",
		Chunks: []Chunk{
			{
				From:        from,
				To:          to,
				Closed:      true,
				Synced:      true,
				FlushedAt:   time.Unix(1, 0),
				LastUpdated: time.Unix(0, 1),
				Data:        data,
			},
		},
	}

	b, err := encodeWithTypeHeader(s, wal.CheckpointRecord, nil)
	require.Nil(t, err)

	out := &Series{}
	err = decodeCheckpointRecord(b, out)
	require.Nil(t, err)

	// override the passed []byte to ensure that the resulting *Series doesn't
	// contain any trailing refs to it.
	for i := range b {
		b[i] = 0
	}

	// test chunk bytes separately
	sChunks := s.Chunks
	s.Chunks = nil
	outChunks := out.Chunks
	out.Chunks = nil

	zero := time.Unix(0, 0)

	require.Equal(t, true, s.To.Equal(out.To))
	s.To = zero
	out.To = zero

	require.Equal(t, s, out)
	require.Equal(t, len(sChunks), len(outChunks))
	for i, exp := range sChunks {

		got := outChunks[i]
		// Issues diffing zero-value time.Locations against nil ones.
		// Check/override them individually so that other fields get tested in an extensible manner.
		require.Equal(t, true, exp.From.Equal(got.From))
		exp.From = zero
		got.From = zero

		require.Equal(t, true, exp.To.Equal(got.To))
		exp.To = zero
		got.To = zero

		require.Equal(t, true, exp.FlushedAt.Equal(got.FlushedAt))
		exp.FlushedAt = zero
		got.FlushedAt = zero

		require.Equal(t, true, exp.LastUpdated.Equal(got.LastUpdated))
		exp.LastUpdated = zero
		got.LastUpdated = zero

		require.Equal(t, exp, got)
	}
}
