package tsdb

import (
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

type WAL interface {
	Start(time.Time) error
	Log(*WALRecord) error
	Stop() error
}

// TODO(owen-d): There are probably some performance gains to be had by utilizing
// pools here, but in the interest of implementation time and given chunks aren't
// flushed often (generally ~5/s), this seems fine.
// This may also be applicable to varint encoding.

// 128KB
// The segment sizes are kept small for the TSDB Head here because
// we only store chunk references
const walSegmentSize = 128 << 10

type RecordType byte

// By prefixing records with versions, we can easily update our wal schema
const (
	// FirstWrite is a special record type written once
	// at the beginning of every WAL. It records the system time
	// when the WAL was created. This is used to determine when to rotate
	// WALs and persists across restarts.
	WalRecordSeries RecordType = iota
	WalRecordChunks
	WalRecordSeriesWithFingerprint
)

type WALRecord struct {
	UserID      string
	Series      record.RefSeries
	Fingerprint uint64
	Chks        ChunkMetasRecord
}

type ChunkMetasRecord struct {
	Chks index.ChunkMetas
	Ref  uint64
}

// NB(owen-d): unused since we started recording the fingerprint
// with the series, but left here for future understanding
func (r *WALRecord) encodeSeries(b []byte) []byte {
	buf := encoding.EncWith(b)
	buf.PutByte(byte(WalRecordSeries))
	buf.PutUvarintStr(r.UserID)

	var enc record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):])) to encode the series.
	encoded := buf.Get()
	encoded = append(encoded, enc.Series([]record.RefSeries{r.Series}, encoded[len(encoded):])...)

	return encoded
}

func (r *WALRecord) encodeSeriesWithFingerprint(b []byte) []byte {
	buf := encoding.EncWith(b)
	buf.PutByte(byte(WalRecordSeriesWithFingerprint))
	buf.PutUvarintStr(r.UserID)
	buf.PutBE64(r.Fingerprint)

	var enc record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):])) to encode the series.
	encoded := buf.Get()
	encoded = append(encoded, enc.Series([]record.RefSeries{r.Series}, encoded[len(encoded):])...)

	return encoded
}

func (r *WALRecord) encodeChunks(b []byte) []byte {
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

func decodeChunks(b []byte, rec *WALRecord) error {
	if len(b) == 0 {
		return nil
	}

	dec := encoding.DecWith(b)

	rec.Chks.Ref = dec.Be64()
	if err := dec.Err(); err != nil {
		return errors.Wrap(err, "decoding series ref")
	}

	ln := dec.Uvarint()
	if err := dec.Err(); err != nil {
		return errors.Wrap(err, "decoding number of chunks")
	}
	// allocate space for the required number of chunks
	rec.Chks.Chks = make(index.ChunkMetas, 0, ln)

	for len(dec.B) > 0 && dec.Err() == nil {
		rec.Chks.Chks = append(rec.Chks.Chks, index.ChunkMeta{
			MinTime:  dec.Be64int64(),
			MaxTime:  dec.Be64int64(),
			Checksum: dec.Be32(),
			KB:       dec.Be32(),
			Entries:  dec.Be32(),
		})
	}

	if err := dec.Err(); err != nil {
		return errors.Wrap(err, "decoding chunk metas")
	}

	return nil
}

func decodeWALRecord(b []byte, walRec *WALRecord) error {
	var (
		userID string
		dec    record.Decoder

		decbuf = encoding.DecWith(b)
		t      = RecordType(decbuf.Byte())
	)

	switch t {
	case WalRecordSeries:
		userID = decbuf.UvarintStr()
		rSeries, err := dec.Series(decbuf.B, nil)
		if err != nil {
			return errors.Wrap(err, "decoding head series")
		}
		// unlike tsdb, we only add one series per record.
		if len(rSeries) > 1 {
			return errors.New("more than one series detected in tsdb head wal record")
		}
		if len(rSeries) == 1 {
			walRec.Series = rSeries[0]
		}
	case WalRecordSeriesWithFingerprint:
		userID = decbuf.UvarintStr()
		walRec.Fingerprint = decbuf.Be64()
		rSeries, err := dec.Series(decbuf.B, nil)
		if err != nil {
			return errors.Wrap(err, "decoding head series")
		}
		// unlike tsdb, we only add one series per record.
		if len(rSeries) > 1 {
			return errors.New("more than one series detected in tsdb head wal record")
		}
		if len(rSeries) == 1 {
			walRec.Series = rSeries[0]
		}
	case WalRecordChunks:
		userID = decbuf.UvarintStr()
		if err := decodeChunks(decbuf.B, walRec); err != nil {
			return err
		}
	default:
		return errors.New("unknown record type")
	}

	if decbuf.Err() != nil {
		return decbuf.Err()
	}

	walRec.UserID = userID
	return nil
}

// the headWAL, unlike Head, is multi-tenant. This is just to avoid the need to maintain
// an open segment per tenant (potentially thousands of them)
type headWAL struct {
	initialized time.Time
	log         log.Logger
	wal         *wlog.WL
}

func newHeadWAL(log log.Logger, dir string, t time.Time) (*headWAL, error) {
	// NB: if we use a non-nil Prometheus Registerer, ensure
	// that the underlying metrics won't conflict with existing WAL metrics in the ingester.
	// Likely, this can be done by adding extra label(s)
	wal, err := wlog.NewSize(log, nil, dir, walSegmentSize, wlog.CompressionNone)
	if err != nil {
		return nil, err
	}

	return &headWAL{
		initialized: t,
		log:         log,
		wal:         wal,
	}, nil
}

func (w *headWAL) Stop() error {
	return w.wal.Close()
}

func (w *headWAL) Log(record *WALRecord) error {
	if record == nil {
		return nil
	}

	var buf []byte

	// Always write series before chunks
	if len(record.Series.Labels) > 0 {
		buf = record.encodeSeriesWithFingerprint(buf[:0])
		if err := w.wal.Log(buf); err != nil {
			return err
		}
	}

	if len(record.Chks.Chks) > 0 {
		buf = record.encodeChunks(buf[:0])
		if err := w.wal.Log(buf); err != nil {
			return err
		}
	}

	return nil
}
