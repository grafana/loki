// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/encoding.go
// Provenance-includes-location: https://github.com/prometheus/prometheus/blob/main/tsdb/encoding/encoding.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Prometheus Authors.

package encoding

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/dennwc/varint"
	"github.com/pkg/errors"
)

// Varint64 reads a signed 64-bit integer encoded as ZigZag varint (same encoding
// used by encoding/binary.PutVarint and Prometheus's tsdb/encoding.Encbuf.PutVarint64).
func (d *Decbuf) Varint64() int64 {
	if d.E != nil {
		return 0
	}
	b, err := d.r.Peek(binary.MaxVarintLen64)
	if err != nil {
		d.E = err
		return 0
	}
	x, n := binary.Varint(b)
	if n < 1 {
		d.E = ErrInvalidSize
		return 0
	}
	if err = d.r.Skip(n); err != nil {
		d.E = err
		return 0
	}
	return x
}

var (
	ErrInvalidSize     = errors.New("invalid size")
	ErrInvalidChecksum = errors.New("invalid checksum")
)

// Decbuf provides safe methods to extract data from a big-endian binary data.
// It the Prometheus encoding.Decbuf type, but for a generic BufReader rather than []byte.
// Decbuf handles all necessary bounds checking and advancing of the underlying reader.
// Consumers may extract multiple datums without checking for errors,
// but the Err() must be checked before using the extracted data.
// New Decbuf instances must be created via a DecbufFactory.
type Decbuf struct {
	r BufReader
	E error
}

func (d *Decbuf) Uvarint() int { return int(d.Uvarint64()) }
func (d *Decbuf) Be32int() int { return int(d.Be32()) }

// CheckCrc32 checks the integrity of the contents of this Decbuf,
// comparing the contents with the CRC32 checksum stored in the last four bytes.
// CheckCrc32 consumes the contents of this Decbuf.
func (d *Decbuf) CheckCrc32(castagnoliTable *crc32.Table) {
	if d.r.Len() <= 4 {
		d.E = ErrInvalidSize
		return
	}

	hash := crc32.New(castagnoliTable)
	bytesToRead := d.r.Len() - 4
	maxChunkSize := 1024 * 1024
	rawBuf := make([]byte, maxChunkSize)

	for bytesToRead > 0 {
		chunkSize := min(bytesToRead, maxChunkSize)
		chunkBuf := rawBuf[0:chunkSize]

		err := d.r.ReadInto(chunkBuf)
		if err != nil {
			d.E = errors.Wrap(err, "read contents for CRC32 calculation")
			return
		}

		if n, err := hash.Write(chunkBuf); err != nil {
			d.E = errors.Wrap(err, "write bytes to CRC32 calculation")
			return
		} else if n != len(chunkBuf) {
			d.E = fmt.Errorf("CRC32 calculation only wrote %v bytes, expected to write %v bytes", n, len(chunkBuf))
			return
		}

		bytesToRead -= len(chunkBuf)
	}

	actual := hash.Sum32()
	expected := d.Be32()

	if actual != expected {
		d.E = ErrInvalidChecksum
	}
}

// Skip advances the pointer of the underlying BufReader by the given number
// of bytes. If E is non-nil, this method has no effect. Skip-ing beyond the
// end of the underlying BufReader will set E to an error and not advance the
// pointer of the BufReader.
func (d *Decbuf) Skip(l int) {
	if d.E != nil {
		return
	}

	d.E = d.r.Skip(l)
}

// SkipUvarintBytes advances the pointer of the underlying BufReader past the
// next varint-prefixed bytes. If E is non-nil, this method has no effect.
func (d *Decbuf) SkipUvarintBytes() {
	l := d.Uvarint64()
	d.Skip(int(l))
}

// ResetAt sets the pointer of the underlying BufReader to the absolute
// offset and discards any buffered data. If E is non-nil, this method has
// no effect. ResetAt-ing beyond the end of the underlying BufReader will set
// E to an error and not advance the pointer of BufReader.
func (d *Decbuf) ResetAt(off int) {
	if d.E != nil {
		return
	}

	// If we are trying to reset at an offset which is already buffered,
	// we can avoid resetting the BufReader and just discard some of the buffer instead.
	if dist := off - d.Offset(); dist >= 0 && dist < d.r.Buffered() {
		d.E = d.r.Skip(dist)
		return
	}

	d.E = d.r.ResetAt(off)
}

// UvarintStr reads varint prefixed bytes into a string and consumes them. The string
// returned allocates its own memory may be used after subsequent reads from the Decbuf.
// If E is non-nil, this method returns an empty string.
func (d *Decbuf) UvarintStr() string {
	return string(d.UnsafeUvarintBytes())
}

// UnsafeUvarintBytes reads varint prefixed bytes into a byte slice, consuming them.
// For in-memory readers the returned slice aliases the underlying buffer and requires no
// allocation.  For streaming (file-backed) readers the underlying bufio buffer may be
// refilled by the next read, so Read() is used to return an owned copy.  Either way the
// caller may safely hold the result across subsequent Decbuf reads.
// If E is non-nil, this method returns an empty byte slice.
func (d *Decbuf) UnsafeUvarintBytes() []byte {
	l := d.Uvarint64()
	if d.E != nil {
		return nil
	}

	b, err := d.r.Read(int(l))
	if err != nil {
		d.E = err
		return nil
	}

	return b
}

func (d *Decbuf) Uvarint64() uint64 {
	if d.E != nil {
		return 0
	}
	b, err := d.r.Peek(10)
	if err != nil {
		d.E = err
		return 0
	}

	x, n := varint.Uvarint(b)
	if n < 1 {
		d.E = ErrInvalidSize
		return 0
	}

	err = d.r.Skip(n)
	if err != nil {
		d.E = err
		return 0
	}

	return x
}

func (d *Decbuf) Be64() uint64 {
	if d.E != nil {
		return 0
	}

	b, err := d.r.Peek(8)
	if err != nil {
		d.E = err
		return 0
	}

	if len(b) != 8 {
		d.E = ErrInvalidSize
		return 0
	}

	v := binary.BigEndian.Uint64(b)
	err = d.r.Skip(8)
	if err != nil {
		d.E = err
		return 0
	}

	return v
}

func (d *Decbuf) Be32() uint32 {
	if d.E != nil {
		return 0
	}

	b, err := d.r.Peek(4)
	if err != nil {
		d.E = err
		return 0
	}

	if len(b) != 4 {
		d.E = ErrInvalidSize
		return 0
	}

	v := binary.BigEndian.Uint32(b)
	err = d.r.Skip(4)
	if err != nil {
		d.E = err
		return 0
	}

	return v
}

func (d *Decbuf) Byte() byte {
	if d.E != nil {
		return 0
	}

	b, err := d.r.Peek(1)
	if err != nil {
		d.E = err
		return 0
	}

	if len(b) != 1 {
		d.E = ErrInvalidSize
		return 0
	}

	v := b[0]
	err = d.r.Skip(1)
	if err != nil {
		d.E = err
		return 0
	}

	return v
}

func (d *Decbuf) Err() error { return d.E }

// Len returns the remaining number of bytes in the underlying BufReader.
func (d *Decbuf) Len() int { return d.r.Len() }

// Offset returns the current offset of the underlying BufReader.
// Calling d.ResetAt(d.Offset()) is effectively a no-op.
func (d *Decbuf) Offset() int { return d.r.Offset() }

// ReadBytes reads exactly n bytes from the underlying reader and advances the
// position. Unlike UnsafeUvarintBytes, the returned slice is safe to use after
// subsequent reads, but is heap-allocated.
func (d *Decbuf) ReadBytes(n int) ([]byte, error) {
	if d.E != nil {
		return nil, d.E
	}
	b, err := d.r.Read(n)
	if err != nil {
		d.E = err
		return nil, err
	}
	return b, nil
}

func (d *Decbuf) Close() error {
	if d.r != nil {
		return d.r.Close()
	}

	return nil
}
