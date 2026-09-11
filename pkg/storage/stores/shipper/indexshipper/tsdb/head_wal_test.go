package tsdb

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
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
		Fingerprint: labels.StableHash(mustParseLabels(`{foo="bar"}`)),
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
	chks := index.ChunkMetas{
		{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6, IngestedAt: 1234},
		{Checksum: 2, MinTime: 5, MaxTime: 10, KB: 7, Entries: 8},
	}

	for _, tc := range []struct {
		name    string
		version RecordType
		want    index.ChunkMetas
	}{
		{
			name:    "current version round-trips the ingestion timestamps",
			version: CurrentChunksRec,
			want:    chks,
		},
		{
			// WALs written by binaries predating WalRecordChunksV2 must keep
			// decoding; their chunks have no ingestion timestamp.
			name:    "WalRecordChunks drops the ingestion timestamps",
			version: WalRecordChunks,
			want: index.ChunkMetas{
				{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6},
				{Checksum: 2, MinTime: 5, MaxTime: 10, KB: 7, Entries: 8},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &WALRecord{
				UserID: "foo",
				Chks: ChunkMetasRecord{
					Ref:  1,
					Chks: chks,
				},
			}
			buf := record.encodeChunks(tc.version, nil)
			require.Equal(t, tc.version, RecordType(buf[0]))

			decoded := &WALRecord{}
			require.NoError(t, decodeWALRecord(buf, decoded))
			require.Equal(t, record.UserID, decoded.UserID)
			require.Equal(t, record.Chks.Ref, decoded.Chks.Ref)
			require.Equal(t, tc.want, decoded.Chks.Chks)
		})
	}
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
