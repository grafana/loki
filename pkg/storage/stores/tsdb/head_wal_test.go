package tsdb

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/encoding"
)

func TestEncodingRoundtrip(t *testing.T) {
	for _, tc := range []struct {
		desc string
		t    RecordType
	}{
		{
			t:    WalRecordSeriesAndChunks,
			desc: "wal record series and chunks",
		},
		{
			t:    WalRecordChunks,
			desc: "wal record chunks",
		},
		{
			t:    WalRecordSeriesWithFingerprint,
			desc: "wal record series with fingerprint",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			base := &WALRecord{
				UserID: "foo",
				Series: record.RefSeries{
					Ref:    chunks.HeadSeriesRef(1),
					Labels: mustParseLabels(`{foo="bar"}`),
				},
				Fingerprint: mustParseLabels(`{foo="bar"}`).Hash(),
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

			switch tc.t {
			case WalRecordChunks:
				base.Series = record.RefSeries{}
				base.Fingerprint = 0
			case WalRecordSeriesWithFingerprint:
				base.Chks = ChunkMetasRecord{}
			}

			encoded := base.encode()
			require.Equal(t, tc.t, RecordType(encoded[0]))
			rec := &WALRecord{}
			require.Nil(t, decodeWALRecord(encoded, rec))
			require.Equal(t, base, rec)
		})
	}
}

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

	var encb encoding.Encbuf
	// First need to add header data
	encb.PutByte(byte(WalRecordSeriesWithFingerprint))
	encb.PutUvarintStr(record.UserID)

	record.encodeSeriesWithFingerprint(&encb)
	decoded := &WALRecord{}

	err := decodeWALRecord(encb.Get(), decoded)
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

	var encb encoding.Encbuf
	// First need to add header data
	encb.PutByte(byte(WalRecordChunks))
	encb.PutUvarintStr(record.UserID)

	record.encodeChunks(&encb)
	decoded := &WALRecord{}

	err := decodeWALRecord(encb.Get(), decoded)
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
