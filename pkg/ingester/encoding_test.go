package ingester

import (
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
)

func Test_Encoding_Series(t *testing.T) {
	record := &WALRecord{
		entryIndexMap: make(map[uint64]int),
		UserID:        "123",
		Series: []record.RefSeries{
			{
				Ref: 456,
				Labels: labels.FromMap(map[string]string{
					"foo":  "bar",
					"bazz": "buzz",
				}),
			},
			{
				Ref: 789,
				Labels: labels.FromMap(map[string]string{
					"abc": "123",
					"def": "456",
				}),
			},
		},
	}

	buf := record.encodeSeries(nil)

	decoded := recordPool.GetRecord()

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_Encoding_Entries(t *testing.T) {
	record := &WALRecord{
		entryIndexMap: make(map[uint64]int),
		UserID:        "123",
		RefEntries: []RefEntries{
			{
				Ref: 456,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(1000, 0),
						Line:      "first",
					},
					{
						Timestamp: time.Unix(2000, 0),
						Line:      "second",
					},
				},
			},
			{
				Ref: 789,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(3000, 0),
						Line:      "third",
					},
					{
						Timestamp: time.Unix(4000, 0),
						Line:      "fourth",
					},
				},
			},
		},
	}

	buf := record.encodeEntries(nil)

	decoded := recordPool.GetRecord()

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func fillChunk(t *testing.T, c chunkenc.Chunk) int64 {
	t.Helper()
	var i, inserted int64
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      "entry for line 0",
	}

	for c.SpaceFor(entry) {
		require.NoError(t, c.Append(entry))
		i++
		inserted += int64(len(entry.Line))
		entry.Timestamp = time.Unix(0, i)
		entry.Line = fmt.Sprintf("entry for line %d", i)
	}
	return inserted
}

func dummyConf() *Config {
	var conf Config
	conf.BlockSize = 256 * 1024
	conf.TargetChunkSize = 1500 * 1024

	return &conf
}

func Test_EncodingChunks(t *testing.T) {

	conf := dummyConf()
	c := chunkenc.NewMemChunk(chunkenc.EncGZIP, conf.BlockSize, conf.TargetChunkSize)
	fillChunk(t, c)

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
	backAgain, err := fromWireChunks(conf, there)
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
}

func Test_EncodingCheckpoint(t *testing.T) {
	conf := dummyConf()
	c := chunkenc.NewMemChunk(chunkenc.EncGZIP, conf.BlockSize, conf.TargetChunkSize)
	require.Nil(t, c.Append(&logproto.Entry{
		Timestamp: time.Unix(1, 0),
		Line:      "hi there",
	}))
	data, err := c.Bytes()
	require.Nil(t, err)
	from, to := c.Bounds()

	ls := labels.FromMap(map[string]string{"foo": "bar"})
	s := &Series{
		UserID:      "fake",
		Fingerprint: 123,
		Labels:      client.FromLabelsToLabelAdapters(ls),
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

	b, err := encodeWithTypeHeader(s, CheckpointRecord)
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

	require.Equal(t, s, out)
	require.Equal(t, len(sChunks), len(outChunks))
	for i, exp := range sChunks {

		got := outChunks[i]
		zero := time.Unix(0, 0)
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
