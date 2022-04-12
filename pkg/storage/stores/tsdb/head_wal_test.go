package tsdb

import (
	"testing"
	"time"

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
