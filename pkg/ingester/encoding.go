package ingester

import (
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/encoding"
	tsdb_record "github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/pkg/logproto"
)

// RecordType represents the type of the WAL/Checkpoint record.
type RecordType byte

const (
	_ = iota // ignore first value so the zero value doesn't look like a record type.
	// WALRecordSeries is the type for the WAL record for series.
	WALRecordSeries RecordType = iota
	// WALRecordSamples is the type for the WAL record for samples.
	WALRecordEntries
	// CheckpointRecord is the type for the Checkpoint record based on protos.
	CheckpointRecord
)

// WALRecord is a struct combining the series and samples record.
type WALRecord struct {
	UserID     string
	Series     []tsdb_record.RefSeries
	RefEntries RefEntries
}

func (r *WALRecord) Reset() {
	r.UserID = ""
	if len(r.Series) > 0 {
		r.Series = r.Series[:0]
	}
	r.RefEntries.Ref = 0
	if len(r.RefEntries.Entries) > 0 {
		r.RefEntries.Entries = r.RefEntries.Entries[:0]
	}

}

type RefEntries struct {
	Ref     uint64
	Entries []logproto.Entry
}

func (record *WALRecord) encodeSeries(b []byte) []byte {
	buf := EncWith(b)
	buf.PutByte(byte(WALRecordSeries))
	buf.PutUvarintStr(record.UserID)

	var enc tsdb_record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):])) to encode the series.
	encoded := buf.Get()
	encoded = append(encoded, enc.Series(record.Series, encoded[len(encoded):])...)

	return encoded
}

func (record *WALRecord) encodeEntries(b []byte) []byte {
	buf := EncWith(b)
	buf.PutByte(byte(WALRecordEntries))
	buf.PutUvarintStr(record.UserID)

	entries := record.RefEntries.Entries
	if len(entries) == 0 {
		return buf.Get()
	}

	// Only encode the series fingerprint if there are >0 entries.
	buf.PutBE64(record.RefEntries.Ref)

	// Store base timestamp and base reference number of first sample.
	// All samples encode their timestamp and ref as delta to those.
	first := entries[0].Timestamp.UnixNano()

	buf.PutBE64int64(first)

	for _, s := range entries {
		buf.PutVarint64(s.Timestamp.UnixNano() - first)
		// denote line length
		byteLine := []byte(s.Line)
		buf.PutUvarint(len(byteLine))
		buf.PutBytes(byteLine)
	}
	return buf.Get()

}

func decodeEntries(b []byte, entries *RefEntries) error {
	if len(b) == 0 {
		return nil
	}

	dec := DecWith(b)

	entries.Ref = dec.Be64()
	baseTime := dec.Be64int64()

	for len(dec.B) > 0 && dec.Err() == nil {
		dRef := dec.Varint64()
		ln := dec.Uvarint()
		line := dec.Bytes(ln)

		entries.Entries = append(entries.Entries, logproto.Entry{
			Timestamp: time.Unix(0, baseTime+dRef),
			Line:      string(line),
		})
	}

	if dec.Err() != nil {
		return errors.Wrapf(dec.Err(), "decode error after %d entries", len(entries.Entries))
	}
	if len(dec.B) > 0 {
		return errors.Errorf("unexpected %d bytes left in entry", len(dec.B))
	}
	return nil

}

func decodeWALRecord(b []byte, walRec *WALRecord) (err error) {
	var (
		userID  string
		dec     tsdb_record.Decoder
		rSeries []tsdb_record.RefSeries

		decbuf = DecWith(b)
		t      = RecordType(decbuf.Byte())
	)

	walRec.Series = walRec.Series[:0]
	walRec.RefEntries.Entries = walRec.RefEntries.Entries[:0]

	switch t {
	case WALRecordSeries:
		userID = decbuf.UvarintStr()
		rSeries, err = dec.Series(decbuf.B, walRec.Series)
	case WALRecordEntries:
		userID = decbuf.UvarintStr()
		err = decodeEntries(decbuf.B, &walRec.RefEntries)
	default:
		return errors.New("unknown record type")
	}

	// We reach here only if its a record with type header.
	if decbuf.Err() != nil {
		return decbuf.Err()
	}

	if err != nil {
		return err
	}

	walRec.UserID = userID
	walRec.Series = rSeries
	return nil
}

func EncWith(b []byte) (res Encbuf) {
	res.B = b
	return res

}

// Encbuf extends encoding.Encbuf with support for multi byte encoding
type Encbuf struct {
	encoding.Encbuf
}

func (e *Encbuf) PutBytes(c []byte) { e.B = append(e.B, c...) }

func DecWith(b []byte) (res Decbuf) {
	res.B = b
	return res
}

// Decbuf extends encoding.Decbuf with support for multi byte decoding
type Decbuf struct {
	encoding.Decbuf
}

func (d *Decbuf) Bytes(n int) []byte {
	if d.E != nil {
		return nil
	}
	if len(d.B) < n {
		d.E = encoding.ErrInvalidSize
		return nil
	}
	x := d.B[:n]
	d.B = d.B[n:]
	return x
}
