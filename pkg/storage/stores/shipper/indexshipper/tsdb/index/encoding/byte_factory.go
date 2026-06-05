// SPDX-License-Identifier: AGPL-3.0-only

package encoding

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/pkg/errors"
)

// ByteSliceDecbufFactory implements DecbufFactory over an in-memory byte slice.
// It is used to adapt ByteSlice-backed index data to the streaming DecbufFactory
// API during the migration away from mmap-based reading.
type ByteSliceDecbufFactory struct {
	data []byte
}

func NewByteSliceDecbufFactory(data []byte) *ByteSliceDecbufFactory {
	return &ByteSliceDecbufFactory{data: data}
}

func (f *ByteSliceDecbufFactory) newReader(offset, length int) (*ByteSliceReader, error) {
	end := offset + length
	if end > len(f.data) || offset < 0 {
		return nil, errors.Wrapf(ErrInvalidSize, "slice bounds [%d:%d] out of range [:%d]", offset, end, len(f.data))
	}
	return &ByteSliceReader{b: f.data[offset:end]}, nil
}

func (f *ByteSliceDecbufFactory) NewDecbufAtChecked(offset int, table *crc32.Table) Decbuf {
	if offset+numLenBytes > len(f.data) {
		return Decbuf{E: ErrInvalidSize}
	}
	contentLength := int(binary.BigEndian.Uint32(f.data[offset:]))
	totalLength := numLenBytes + contentLength + crc32.Size
	r, err := f.newReader(offset, totalLength)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "create byte slice reader")}
	}
	d := Decbuf{r: r}
	if d.ResetAt(numLenBytes); d.E != nil {
		return d
	}
	if table != nil {
		if d.CheckCrc32(table); d.E != nil {
			return d
		}
		d.ResetAt(numLenBytes)
	}
	return d
}

func (f *ByteSliceDecbufFactory) NewDecbufAtUnchecked(offset int) Decbuf {
	return f.NewDecbufAtChecked(offset, nil)
}

func (f *ByteSliceDecbufFactory) NewRawDecbuf() Decbuf {
	return Decbuf{r: &ByteSliceReader{b: f.data}}
}

func (f *ByteSliceDecbufFactory) NewDecbufInSection(tableOffset, sectionStartOffset, sectionEndOffset int) Decbuf {
	base := tableOffset + sectionStartOffset
	requestedLength := sectionEndOffset - sectionStartOffset
	if requestedLength < 0 {
		return Decbuf{E: fmt.Errorf("end offset %d before start offset %d", sectionEndOffset, sectionStartOffset)}
	}
	available := len(f.data) - base
	if available < 0 {
		available = 0
	}
	length := min(requestedLength, available)
	r, err := f.newReader(base, length)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "create byte slice reader")}
	}
	return Decbuf{r: r}
}

func (f *ByteSliceDecbufFactory) NewDecbufUvarintAt(offset int, table *crc32.Table) Decbuf {
	if offset >= len(f.data) {
		return Decbuf{E: ErrInvalidSize}
	}
	var lenBuf [binary.MaxVarintLen64]byte
	n := copy(lenBuf[:], f.data[offset:])
	l, varLen := binary.Uvarint(lenBuf[:n])
	if varLen <= 0 {
		return Decbuf{E: fmt.Errorf("invalid uvarint length prefix at offset %d", offset)}
	}
	if table != nil {
		end := offset + varLen + int(l) + crc32.Size
		if end > len(f.data) {
			return Decbuf{E: ErrInvalidSize}
		}
		actual := crc32.Checksum(f.data[offset+varLen:offset+varLen+int(l)], table)
		expected := binary.BigEndian.Uint32(f.data[offset+varLen+int(l):])
		if actual != expected {
			return Decbuf{E: ErrInvalidChecksum}
		}
	}
	r, err := f.newReader(offset+varLen, int(l))
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "create byte slice reader")}
	}
	return Decbuf{r: r}
}

func (f *ByteSliceDecbufFactory) Close() error { return nil }
