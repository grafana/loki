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
	"github.com/grafana/loki/v3/pkg/util/encoding"
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
	for _, tc := range []struct {
		name       string
		chks       index.ChunkMetas
		wantRecord RecordType
	}{
		{
			name:       "no ingestion timestamps",
			wantRecord: WalRecordChunks,
			chks: index.ChunkMetas{
				{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6},
				{Checksum: 2, MinTime: 5, MaxTime: 10, KB: 7, Entries: 8},
			},
		},
		{
			name:       "all chunks carry an ingestion timestamp",
			wantRecord: WalRecordChunksWithIngestedAt,
			chks: index.ChunkMetas{
				{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6, IngestedAt: 1234},
				{Checksum: 2, MinTime: 5, MaxTime: 10, KB: 7, Entries: 8, IngestedAt: 5678},
			},
		},
		{
			name:       "a single ingestion timestamp promotes the whole record",
			wantRecord: WalRecordChunksWithIngestedAt,
			chks: index.ChunkMetas{
				{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6},
				{Checksum: 2, MinTime: 5, MaxTime: 10, KB: 7, Entries: 8, IngestedAt: 5678},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &WALRecord{
				UserID: "foo",
				Chks: ChunkMetasRecord{
					Ref:  1,
					Chks: tc.chks,
				},
			}
			buf := record.encodeChunks(nil)
			require.Equal(t, tc.wantRecord, RecordType(buf[0]))

			decoded := &WALRecord{}
			require.NoError(t, decodeWALRecord(buf, decoded))
			require.Equal(t, record, decoded)
		})
	}
}

// Test_Encoding_Chunks_LegacyRecord covers the mixed-version case of a rolling
// restart: WALs written before WalRecordChunksWithIngestedAt existed must still
// decode, with a zero IngestedAt, and records without ingestion timestamps must
// still be written in that older layout so a rollback can read them.
func Test_Encoding_Chunks_LegacyRecord(t *testing.T) {
	chks := index.ChunkMetas{
		{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6},
		{Checksum: 2, MinTime: 5, MaxTime: 10, KB: 7, Entries: 8},
	}
	record := &WALRecord{
		UserID: "foo",
		Chks:   ChunkMetasRecord{Ref: 1, Chks: chks},
	}

	require.Equal(t, encodeChunksLegacy(record, nil), record.encodeChunks(nil))

	// A legacy record holds no ingestion timestamps, whatever the in-memory metas say.
	record.Chks.Chks = index.ChunkMetas{
		{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6, IngestedAt: 1234},
	}
	decoded := &WALRecord{}
	require.NoError(t, decodeWALRecord(encodeChunksLegacy(record, nil), decoded))
	require.Equal(t, index.ChunkMetas{
		{Checksum: 1, MinTime: 1, MaxTime: 4, KB: 5, Entries: 6},
	}, decoded.Chks.Chks)
}

// encodeChunksLegacy is the WalRecordChunks encoder as it existed before
// WalRecordChunksWithIngestedAt was added. It pins the layout that older
// binaries wrote, and that both they and current ones must keep reading.
func encodeChunksLegacy(r *WALRecord, b []byte) []byte {
	buf := encoding.EncWith(b)
	buf.PutByte(byte(WalRecordChunks))
	buf.PutUvarintStr(r.UserID)
	buf.PutBE64(r.Chks.Ref)
	buf.PutUvarint(len(r.Chks.Chks))

	for _, chk := range r.Chks.Chks {
		buf.PutBE64(uint64(chk.MinTime))
		buf.PutBE64(uint64(chk.MaxTime))
		buf.PutBE32(chk.Checksum)
		buf.PutBE32(chk.KB)
		buf.PutBE32(chk.Entries)
	}

	return buf.Get()
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
