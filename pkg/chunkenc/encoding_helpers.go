package chunkenc

import (
	"encoding/binary"
	"hash"
	"hash/crc32"
)

// encbuf is a helper type to populate a byte slice with various types.
type encbuf struct {
	b []byte
	c [binary.MaxVarintLen64]byte
}

func (e *encbuf) reset()      { e.b = e.b[:0] }
func (e *encbuf) get() []byte { return e.b }

func (e *encbuf) putByte(c byte) { e.b = append(e.b, c) }

func (e *encbuf) putBE64int(x int) { e.putBE64(uint64(x)) }
func (e *encbuf) putUvarint(x int) { e.putUvarint64(uint64(x)) }

func (e *encbuf) putBE32(x uint32) {
	binary.BigEndian.PutUint32(e.c[:], x)
	e.b = append(e.b, e.c[:4]...)
}

func (e *encbuf) putBE64(x uint64) {
	binary.BigEndian.PutUint64(e.c[:], x)
	e.b = append(e.b, e.c[:8]...)
}

func (e *encbuf) putUvarint64(x uint64) {
	n := binary.PutUvarint(e.c[:], x)
	e.b = append(e.b, e.c[:n]...)
}

func (e *encbuf) putVarint64(x int64) {
	n := binary.PutVarint(e.c[:], x)
	e.b = append(e.b, e.c[:n]...)
}

// putHash appends a hash over the buffers current contents to the buffer.
func (e *encbuf) putHash(h hash.Hash) {
	h.Reset()
	_, err := h.Write(e.b)
	if err != nil {
		panic(err) // The CRC32 implementation does not error
	}
	e.b = h.Sum(e.b)
}

// decbuf provides safe methods to extract data from a byte slice. It does all
// necessary bounds checking and advancing of the byte slice.
// Several datums can be extracted without checking for errors. However, before using
// any datum, the err() method must be checked.
type decbuf struct {
	b []byte
	e error
}

func (d *decbuf) uvarint() int { return int(d.uvarint64()) }

// crc32 returns a CRC32 checksum over the remaining bytes.
func (d *decbuf) crc32() uint32 {
	return crc32.Checksum(d.b, castagnoliTable)
}

func (d *decbuf) varint64() int64 {
	if d.e != nil {
		return 0
	}
	x, n := binary.Varint(d.b)
	if n < 1 {
		d.e = ErrInvalidSize
		return 0
	}
	d.b = d.b[n:]
	return x
}

func (d *decbuf) uvarint64() uint64 {
	if d.e != nil {
		return 0
	}
	x, n := binary.Uvarint(d.b)
	if n < 1 {
		d.e = ErrInvalidSize
		return 0
	}
	d.b = d.b[n:]
	return x
}

func (d *decbuf) be32() uint32 {
	if d.e != nil {
		return 0
	}
	if len(d.b) < 4 {
		d.e = ErrInvalidSize
		return 0
	}
	x := binary.BigEndian.Uint32(d.b)
	d.b = d.b[4:]
	return x
}

func (d *decbuf) byte() byte {
	if d.e != nil {
		return 0
	}
	if len(d.b) < 1 {
		d.e = ErrInvalidSize
		return 0
	}
	x := d.b[0]
	d.b = d.b[1:]
	return x
}

func (d *decbuf) bytes(n int) []byte {
	if d.e != nil {
		return nil
	}
	if len(d.b) < n {
		d.e = ErrInvalidSize
		return nil
	}
	x := d.b[:n]
	d.b = d.b[n:]
	return x
}

func (d *decbuf) err() error { return d.e }
