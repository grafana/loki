package tsdb

import (
	"testing"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
)

func Test_Encoding_Series(t *testing.T) {
	record := &walRecord{
		userID: "foo",
		series: record.RefSeries{
			Ref:    chunks.HeadSeriesRef(1),
			Labels: mustParseLabels(`{foo="bar"}`),
		},
	}
	buf := record.encodeSeries(nil)
	decoded := &walRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}

func Test_Encoding_Chunks(t *testing.T) {
	record := &walRecord{
		userID: "foo",
		chks: chunkMetasRecord{
			ref: 1,
			chks: index.ChunkMetas{
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
	decoded := &walRecord{}

	err := decodeWALRecord(buf, decoded)
	require.Nil(t, err)
	require.Equal(t, record, decoded)
}
