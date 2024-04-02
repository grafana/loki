package tsdb

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func Test_Encoding_Series(t *testing.T) {
	record := &WALRecord{
		UserID: "foo",
		Series: record.RefSeries{
			Ref:    chunks.HeadSeriesRef(1),
			Labels: mustParseLabels(`{foo="bar"}`),
		},
	}
	buf := record.encodeSeries(nil)
	decoded := &WALRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_Encoding_SeriesWithFingerprint(t *testing.T) {
	record := &WALRecord{
		UserID:      "foo",
		Fingerprint: mustParseLabels(`{foo="bar"}`).Hash(),
		Series: record.RefSeries{
			Ref:    chunks.HeadSeriesRef(1),
			Labels: mustParseLabels(`{foo="bar"}`),
		},
	}
	buf := record.encodeSeriesWithFingerprint(nil)
	decoded := &WALRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_Encoding_Chunks(t *testing.T) {
	record := &WALRecord{
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
	decoded := &WALRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_HeadWALLog(t *testing.T) {
	dir := t.TempDir()
	w, err := newHeadWAL(log.NewNopLogger(), dir, time.Now())
	require.Nil(t, err)

	newSeries := &WALRecord{
		UserID: "foo",
		Series: record.RefSeries{Ref: 1, Labels: mustParseLabels(`{foo="bar"}`)},
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

	chunksOnly := &WALRecord{
		UserID: "foo",
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
