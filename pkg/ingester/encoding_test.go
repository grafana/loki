package ingester

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
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

	var decoded WALRecord

	err := decodeWALRecord(buf, &decoded)
	require.Nil(t, err)
	require.Equal(t, record, &decoded)
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

	var decoded WALRecord

	err := decodeWALRecord(buf, &decoded)
	require.Nil(t, err)
	require.Equal(t, record, &decoded)
}

func fillChunk(c chunkenc.Chunk) int64 {
	var i, inserted int64
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      "entry for line 0",
	}

	for c.SpaceFor(entry) {
		err := c.Append(entry)
		if err != nil {
			panic(err)
		}
		i++
		inserted += int64(len(entry.Line))
		entry.Timestamp = time.Unix(0, i)
		entry.Line = fmt.Sprintf("entry for line %d", i)
	}
	return inserted
}

func Test_EncodingChunks(t *testing.T) {

	var conf Config
	conf.BlockSize = 256 * 1024
	conf.TargetChunkSize = 1500 * 1024

	c := chunkenc.NewMemChunk(chunkenc.EncGZIP, conf.BlockSize, conf.TargetChunkSize)
	fillChunk(c)

	from := []chunkDesc{
		{
			chunk: c,
		},
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
	backAgain, err := fromWireChunks(&conf, there)
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
