package ingester

import (
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/prometheus/prometheus/tsdb/record"

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
	UserID string
	Series []record.RefSeries

	// entryIndexMap coordinates the RefEntries index associated with a particular fingerprint.
	// This is helpful for constant time lookups during ingestion and is ignored when restoring
	// from the WAL.
	entryIndexMap map[uint64]int
	RefEntries    []RefEntries
}

func (r *WALRecord) IsEmpty() bool {
	return len(r.Series) == 0 && len(r.RefEntries) == 0
}

func (r *WALRecord) Reset() {
	r.UserID = ""
	if len(r.Series) > 0 {
		r.Series = r.Series[:0]
	}

	for _, ref := range r.RefEntries {
		recordPool.PutEntries(ref.Entries)
	}
	r.RefEntries = r.RefEntries[:0]
	r.entryIndexMap = make(map[uint64]int)
}

func (r *WALRecord) AddEntries(fp uint64, entries ...logproto.Entry) {
	if idx, ok := r.entryIndexMap[fp]; ok {
		r.RefEntries[idx].Entries = append(r.RefEntries[idx].Entries, entries...)
		return
	}

	r.entryIndexMap[fp] = len(r.RefEntries)
	r.RefEntries = append(r.RefEntries, RefEntries{
		Ref:     fp,
		Entries: entries,
	})
}

type RefEntries struct {
	Ref     uint64
	Entries []logproto.Entry
}

func (r *WALRecord) encodeSeries(b []byte) []byte {
	buf := EncWith(b)
	buf.PutByte(byte(WALRecordSeries))
	buf.PutUvarintStr(r.UserID)

	var enc record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):])) to encode the series.
	encoded := buf.Get()
	encoded = append(encoded, enc.Series(r.Series, encoded[len(encoded):])...)

	return encoded
}

func (r *WALRecord) encodeEntries(b []byte) []byte {
	buf := EncWith(b)
	buf.PutByte(byte(WALRecordEntries))
	buf.PutUvarintStr(r.UserID)

	// Placeholder for the first timestamp of any sample encountered.
	// All others in this record will store their timestamps as diffs relative to this
	// as a space optimization.
	var first int64

outer:
	for _, ref := range r.RefEntries {
		for _, entry := range ref.Entries {
			first = entry.Timestamp.UnixNano()
			buf.PutBE64int64(first)
			break outer
		}
	}

	for _, ref := range r.RefEntries {
		// ignore refs with 0 entries
		if len(ref.Entries) < 1 {
			continue
		}
		buf.PutBE64(ref.Ref)             // write fingerprint
		buf.PutUvarint(len(ref.Entries)) // write number of entries

		for _, s := range ref.Entries {
			buf.PutVarint64(s.Timestamp.UnixNano() - first)
			// denote line length
			byteLine := []byte(s.Line)
			buf.PutUvarint(len(byteLine))
			buf.PutBytes(byteLine)
		}
	}
	return buf.Get()

}

func decodeEntries(b []byte, rec *WALRecord) error {
	if len(b) == 0 {
		return nil
	}

	dec := DecWith(b)
	baseTime := dec.Be64int64()

	for len(dec.B) > 0 && dec.Err() == nil {
		refEntries := RefEntries{
			Ref: dec.Be64(),
		}

		nEntries := dec.Uvarint()
		rem := nEntries
		for ; dec.Err() == nil && rem > 0; rem-- {
			timeOffset := dec.Varint64()
			lineLength := dec.Uvarint()
			line := dec.Bytes(lineLength)

			refEntries.Entries = append(refEntries.Entries, logproto.Entry{
				Timestamp: time.Unix(0, baseTime+timeOffset),
				Line:      string(line),
			})
		}

		if dec.Err() != nil {
			return errors.Wrapf(dec.Err(), "entry decode error after %d RefEntries", nEntries-rem)
		}

		rec.RefEntries = append(rec.RefEntries, refEntries)
	}

	if dec.Err() != nil {
		return errors.Wrap(dec.Err(), "refEntry decode error")
	}

	if len(dec.B) > 0 {
		return errors.Errorf("unexpected %d bytes left in entry", len(dec.B))
	}
	return nil

}

func decodeWALRecord(b []byte, walRec *WALRecord) (err error) {
	var (
		userID  string
		dec     record.Decoder
		rSeries []record.RefSeries

		decbuf = DecWith(b)
		t      = RecordType(decbuf.Byte())
	)

	switch t {
	case WALRecordSeries:
		userID = decbuf.UvarintStr()
		rSeries, err = dec.Series(decbuf.B, walRec.Series)
	case WALRecordEntries:
		userID = decbuf.UvarintStr()
		err = decodeEntries(decbuf.B, walRec)
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
