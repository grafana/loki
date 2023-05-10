package tsdb

import (
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/encoding"
)

const (
	// Disable this to begin writing chunks and series in the same record.
	WALWriteChunksAndSeriesSeparately = false
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
	// In an attempt to mitigate replay failures missing series
	// (seen a few times, unsure of cause), we record the series
	// and chunks in the same record. This is more expensive, but
	// should be more resilient to failures and still not a performance
	// bottleneck as we flush chunks orders of magnitude less than individual
	// log lines.
	WalRecordSeriesAndChunks
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
// and testware
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

func (r *WALRecord) encodeSeriesWithFingerprint(buf *encoding.Encbuf) {
	buf.PutBE64(r.Fingerprint)
	var enc record.Encoder
	series := enc.Series([]record.RefSeries{r.Series}, []byte{})
	buf.PutBytes(series)
}

func (r *WALRecord) encodeChunks(buf *encoding.Encbuf) {
	buf.PutBE64(r.Chks.Ref)
	buf.PutUvarint(len(r.Chks.Chks))

	for _, chk := range r.Chks.Chks {
		buf.PutBE64(uint64(chk.MinTime))
		buf.PutBE64(uint64(chk.MaxTime))
		buf.PutBE32(chk.Checksum)
		buf.PutBE32(chk.KB)
		buf.PutBE32(chk.Entries)
	}
}

func (r *WALRecord) encodeSeriesAndChunks(buf *encoding.Encbuf) {
	r.encodeSeriesWithFingerprint(buf)
	r.encodeChunks(buf)
}

func (r *WALRecord) decodeSeries(dec *encoding.Decbuf, fp bool) error {
	var d record.Decoder

	// decode fingerprint first if required
	if fp {
		r.Fingerprint = dec.Be64()
	}

	// copied from prometheus tsdb/record
	if record.Type(dec.Byte()) != record.Series {
		return errors.New("invalid record type")
	}

	// we only ever write one series at a time
	ref := storage.SeriesRef(dec.Be64())
	lset := d.DecodeLabels(&dec.Decbuf)
	r.Series = record.RefSeries{
		Ref:    chunks.HeadSeriesRef(ref),
		Labels: lset,
	}

	if err := dec.Err(); err != nil {
		return err
	}

	return nil
}

func (r *WALRecord) decodeChunks(dec *encoding.Decbuf) error {
	r.Chks.Ref = dec.Be64()
	if err := dec.Err(); err != nil {
		return errors.Wrap(err, "decoding series ref")
	}

	ln := dec.Uvarint()
	if err := dec.Err(); err != nil {
		return errors.Wrap(err, "decoding number of chunks")
	}
	// allocate space for the required number of chunks
	r.Chks.Chks = make(index.ChunkMetas, 0, ln)

	for len(dec.B) > 0 && dec.Err() == nil {
		r.Chks.Chks = append(r.Chks.Chks, index.ChunkMeta{
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
		decbuf = encoding.DecWith(b)
		// read the record type and userID first
		t = RecordType(decbuf.Byte())
	)
	walRec.UserID = decbuf.UvarintStr()

	switch t {
	case WalRecordSeriesAndChunks:
		if err := walRec.decodeSeries(&decbuf, true); err != nil {
			return err
		}
		if err := walRec.decodeChunks(&decbuf); err != nil {
			return err
		}
	case WalRecordSeries:
		if err := walRec.decodeSeries(&decbuf, false); err != nil {
			return err
		}
	case WalRecordSeriesWithFingerprint:
		if err := walRec.decodeSeries(&decbuf, true); err != nil {
			return err
		}
	case WalRecordChunks:
		if err := walRec.decodeChunks(&decbuf); err != nil {
			return err
		}
	default:
		return errors.New("unknown record type")
	}

	if decbuf.Err() != nil {
		return decbuf.Err()
	}

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
	wal, err := wlog.NewSize(log, nil, dir, walSegmentSize, false)
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

func (r *WALRecord) encode() []byte {
	buf := encoding.EncWith([]byte{})
	// We always write the record type and userID first
	switch {
	// Prefer writing entire records with both labels & chunks since it simplifies replay logic
	// and the space amplification is not significant
	case len(r.Series.Labels) > 0 && len(r.Chks.Chks) > 0:
		buf.PutByte(byte(WalRecordSeriesAndChunks))
		buf.PutUvarintStr(r.UserID)
		r.encodeSeriesAndChunks(&buf)
	case len(r.Series.Labels) > 0:
		buf.PutByte(byte(WalRecordSeriesWithFingerprint))
		buf.PutUvarintStr(r.UserID)
		r.encodeSeriesWithFingerprint(&buf)
	case len(r.Chks.Chks) > 0:
		buf.PutByte(byte(WalRecordChunks))
		buf.PutUvarintStr(r.UserID)
		r.encodeChunks(&buf)
	}
	return buf.Get()
}

func (w *headWAL) Log(record *WALRecord) error {
	if record == nil {
		return nil
	}

	if WALWriteChunksAndSeriesSeparately {
		// Always write series before chunks
		// hack to be reverted later which allows us to write the series
		// and chunks separately
		if len(record.Series.Labels) > 0 {
			r := &WALRecord{
				UserID:      record.UserID,
				Series:      record.Series,
				Fingerprint: record.Fingerprint,
			}
			if err := w.wal.Log(r.encode()); err != nil {
				return err
			}
		}

		if len(record.Chks.Chks) > 0 {
			r := &WALRecord{
				UserID: record.UserID,
				Chks:   record.Chks,
			}
			if err := w.wal.Log(r.encode()); err != nil {
				return err
			}
		}

	} else {
		if err := w.wal.Log(record.encode()); err != nil {
			return err
		}
	}

	return nil
}
