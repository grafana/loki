package tsdb

import (
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
)

func Test_Encoding_Series(t *testing.T) {
	record := &WalRecord{
		UserID: "foo",
		Series: record.RefSeries{
			Ref:    chunks.HeadSeriesRef(1),
			Labels: mustParseLabels(`{foo="bar"}`),
		},
	}
	buf := record.encodeSeries(nil)
	decoded := &WalRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_Encoding_Chunks(t *testing.T) {
	record := &WalRecord{
		UserID: "foo",
		Chks: ChunkMetasRecord{
			Ref: 1,
			Chks: index.ChunkMetas{
				{
					Checksum: 1,
					MinTime:  1,
					MaxTime:  4,
					KB:       5,
					Entries:  6,
				},
				{
					Checksum: 2,
					MinTime:  5,
					MaxTime:  10,
					KB:       7,
					Entries:  8,
				},
			},
		},
	}
	buf := record.encodeChunks(nil)
	decoded := &WalRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_Encoding_StartTime(t *testing.T) {
	record := &WalRecord{
		StartTime: time.Now().UnixNano(),
	}
	buf := record.encodeStartTime(nil)
	decoded := &WalRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_HeadWALLog(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	w, err := newHeadWAL(log.NewNopLogger(), dir)
	require.Nil(t, err)
	require.Nil(t, w.Start(start))

	newSeries := &WalRecord{
		UserID:    "foo",
		StartTime: 0,
		Series:    record.RefSeries{Ref: 1, Labels: mustParseLabels(`{foo="bar"}`)},
		Chks: ChunkMetasRecord{
			Chks: []index.ChunkMeta{
				{
					Checksum: 1,
					MinTime:  1,
					MaxTime:  10,
					KB:       5,
					Entries:  50,
				},
			},
			Ref: 1,
		},
	}
	require.Nil(t, w.Log(newSeries))

	chunksOnly := &WalRecord{
		UserID:    "foo",
		StartTime: 0,
		Chks: ChunkMetasRecord{
			Chks: []index.ChunkMeta{
				{
					Checksum: 2,
					MinTime:  5,
					MaxTime:  100,
					KB:       3,
					Entries:  25,
				},
			},
			Ref: 1,
		},
	}
	require.Nil(t, w.Log(chunksOnly))
	require.Nil(t, w.Stop())
}
