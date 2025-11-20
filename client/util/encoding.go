package util

import (
	"encoding/binary"
	"hash/crc32"

	"github.com/prometheus/prometheus/tsdb/encoding"
)

// Encbuf extends encoding.Encbuf with support for multi byte encoding
type Encbuf struct {
	encoding.Encbuf
}

func EncWith(b []byte) (res Encbuf) {
	res.B = b
	return res
}

// EncWrap wraps a Prometheus encoding.Encbuf into our Encbuf
func EncWrap(inner encoding.Encbuf) Encbuf { return Encbuf{Encbuf: inner} }

// EncWrapEncbuf wraps an Encbuf (useful when re-exporting from other modules)
func EncWrapEncbuf(inner Encbuf) Encbuf { return inner }

func (e *Encbuf) PutString(s string) { e.B = append(e.B, s...) }

func (e *Encbuf) Skip(i int) {
	e.B = e.B[:len(e.B)+i]
}

// Decbuf extends encoding.Decbuf with support for multi byte decoding
type Decbuf struct {
	encoding.Decbuf
}

func DecWith(b []byte) (res Decbuf) {
	res.B = b
	return res
}

// DecWrap wraps a Prometheus encoding.Decbuf into our Decbuf
func DecWrap(inner encoding.Decbuf) Decbuf { return Decbuf{Decbuf: inner} }

// DecWrapDecbuf wraps a Decbuf (useful when re-exporting from other modules)
func DecWrapDecbuf(inner Decbuf) Decbuf { return inner }

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

func (d *Decbuf) CheckCrc(castagnoliTable *crc32.Table) error {
	if d.E != nil {
		return d.E
	}
	if len(d.B) < 4 {
		d.E = encoding.ErrInvalidSize
		return d.E
	}

	offset := len(d.B) - 4
	expCRC := binary.BigEndian.Uint32(d.B[offset:])
	d.B = d.B[:offset]

	if d.Crc32(castagnoliTable) != expCRC {
		d.E = encoding.ErrInvalidChecksum
		return d.E
	}
	return nil
}
