// Package decoder provides low-level decoding utilities for MaxMind DB data.
package decoder

import (
	"fmt"
	"iter"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// Decoder allows decoding of a single value stored at a specific offset
// in the database.
type Decoder struct {
	d             DataDecoder
	offset        uint
	nextOffset    uint
	hasNextOffset bool
}

type decoderOptions struct {
	// Intentionally empty for now. DecoderOption callbacks are still invoked so
	// adding options in a future release is non-breaking.
}

// DecoderOption configures a Decoder.
//
//nolint:revive // name follows existing library pattern (ReaderOption, NetworksOption)
type DecoderOption func(*decoderOptions)

// NewDecoder creates a new Decoder with the given DataDecoder, offset, and options.
func NewDecoder(d DataDecoder, offset uint, options ...DecoderOption) *Decoder {
	opts := decoderOptions{}
	for _, option := range options {
		option(&opts)
	}

	return &Decoder{
		d:      d,
		offset: offset,
	}
}

// ReadBool reads the value pointed by the decoder as a bool.
//
// Returns an error if the database is malformed or if the pointed value is not a bool.
func (d *Decoder) ReadBool() (bool, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindBool)
	if err != nil {
		return false, d.wrapError(err)
	}

	value, newOffset, err := d.d.decodeBool(size, offset)
	if err != nil {
		return false, d.wrapError(err)
	}
	d.setNextOffset(newOffset)
	return value, nil
}

// ReadString reads the value pointed by the decoder as a string.
//
// Returns an error if the database is malformed or if the pointed value is not a string.
func (d *Decoder) ReadString() (string, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindString)
	if err != nil {
		return "", d.wrapError(err)
	}

	value, newOffset, err := d.d.decodeString(size, offset)
	if err != nil {
		return "", d.wrapError(err)
	}
	d.setNextOffset(newOffset)
	return value, nil
}

// ReadBytes reads the value pointed by the decoder as bytes.
//
// Returns an error if the database is malformed or if the pointed value is not bytes.
func (d *Decoder) ReadBytes() ([]byte, error) {
	val, err := d.readBytes(KindBytes)
	if err != nil {
		return nil, d.wrapError(err)
	}
	return val, nil
}

// ReadFloat32 reads the value pointed by the decoder as a float32.
//
// Returns an error if the database is malformed or if the pointed value is not a float.
func (d *Decoder) ReadFloat32() (float32, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindFloat32)
	if err != nil {
		return 0, d.wrapError(err)
	}

	value, nextOffset, err := d.d.decodeFloat32(size, offset)
	if err != nil {
		return 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return value, nil
}

// ReadFloat64 reads the value pointed by the decoder as a float64.
//
// Returns an error if the database is malformed or if the pointed value is not a double.
func (d *Decoder) ReadFloat64() (float64, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindFloat64)
	if err != nil {
		return 0, d.wrapError(err)
	}

	value, nextOffset, err := d.d.decodeFloat64(size, offset)
	if err != nil {
		return 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return value, nil
}

// ReadInt32 reads the value pointed by the decoder as an int32.
//
// Returns an error if the database is malformed or if the pointed value is not an int32.
func (d *Decoder) ReadInt32() (int32, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindInt32)
	if err != nil {
		return 0, d.wrapError(err)
	}

	value, nextOffset, err := d.d.decodeInt32(size, offset)
	if err != nil {
		return 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return value, nil
}

// ReadUint16 reads the value pointed by the decoder as a uint16.
//
// Returns an error if the database is malformed or if the pointed value is not a uint16.
func (d *Decoder) ReadUint16() (uint16, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindUint16)
	if err != nil {
		return 0, d.wrapError(err)
	}

	value, nextOffset, err := d.d.decodeUint16(size, offset)
	if err != nil {
		return 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return value, nil
}

// ReadUint32 reads the value pointed by the decoder as a uint32.
//
// Returns an error if the database is malformed or if the pointed value is not a uint32.
func (d *Decoder) ReadUint32() (uint32, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindUint32)
	if err != nil {
		return 0, d.wrapError(err)
	}

	value, nextOffset, err := d.d.decodeUint32(size, offset)
	if err != nil {
		return 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return value, nil
}

// ReadUint64 reads the value pointed by the decoder as a uint64.
//
// Returns an error if the database is malformed or if the pointed value is not a uint64.
func (d *Decoder) ReadUint64() (uint64, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindUint64)
	if err != nil {
		return 0, d.wrapError(err)
	}

	value, nextOffset, err := d.d.decodeUint64(size, offset)
	if err != nil {
		return 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return value, nil
}

// ReadUint128 reads the value pointed by the decoder as a uint128.
//
// Returns an error if the database is malformed or if the pointed value is not a uint128.
func (d *Decoder) ReadUint128() (hi, lo uint64, err error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindUint128)
	if err != nil {
		return 0, 0, d.wrapError(err)
	}

	hi, lo, nextOffset, err := d.d.decodeUint128(size, offset)
	if err != nil {
		return 0, 0, d.wrapError(err)
	}

	d.setNextOffset(nextOffset)
	return hi, lo, nil
}

// ReadMap returns an iterator to read the map along with the map size. The
// size can be used to pre-allocate a map with the correct capacity for better
// performance. The first value from the iterator is the key. Please note that
// this byte slice is only valid during the iteration. This is done to avoid
// an unnecessary allocation. You must make a copy of it if you are storing it
// for later use. The second value is an error indicating that the database is
// malformed or that the pointed value is not a map.
func (d *Decoder) ReadMap() (iter.Seq2[[]byte, error], uint, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindMap)
	if err != nil {
		return nil, 0, d.wrapError(err)
	}

	iterator := func(yield func([]byte, error) bool) {
		currentOffset := offset

		for range size {
			key, keyEndOffset, err := d.d.decodeKey(currentOffset)
			if err != nil {
				yield(nil, d.wrapErrorAtOffset(err, currentOffset))
				return
			}

			// Position decoder to read value after yielding key
			d.reset(keyEndOffset)

			ok := yield(key, nil)
			if !ok {
				return
			}

			// Skip the value to get to next key-value pair
			valueEndOffset, err := d.d.nextValueOffset(keyEndOffset, 1)
			if err != nil {
				yield(nil, d.wrapError(err))
				return
			}
			currentOffset = valueEndOffset
		}

		// Set the final offset after map iteration
		d.reset(currentOffset)
	}

	return iterator, size, nil
}

// ReadSlice returns an iterator over the values of the slice along with the
// slice size. The size can be used to pre-allocate a slice with the correct
// capacity for better performance. The iterator returns an error if the
// database is malformed or if the pointed value is not a slice.
func (d *Decoder) ReadSlice() (iter.Seq[error], uint, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(KindSlice)
	if err != nil {
		return nil, 0, d.wrapError(err)
	}

	iterator := func(yield func(error) bool) {
		currentOffset := offset

		for i := range size {
			// Position decoder to read current element
			d.reset(currentOffset)

			ok := yield(nil)
			if !ok {
				// Skip the unvisited elements
				remaining := size - i - 1
				if remaining > 0 {
					endOffset, err := d.d.nextValueOffset(currentOffset, remaining)
					if err == nil {
						d.reset(endOffset)
					}
				}
				return
			}

			// Advance to next element
			nextOffset, err := d.d.nextValueOffset(currentOffset, 1)
			if err != nil {
				yield(d.wrapError(err))
				return
			}
			currentOffset = nextOffset
		}

		// Set final offset after slice iteration
		d.reset(currentOffset)
	}

	return iterator, size, nil
}

// SkipValue skips over the current value without decoding it.
// This is useful in custom decoders when encountering unknown fields.
// The decoder will be positioned after the skipped value.
func (d *Decoder) SkipValue() error {
	// We can reuse the existing nextValueOffset logic by jumping to the next value
	nextOffset, err := d.d.nextValueOffset(d.offset, 1)
	if err != nil {
		return d.wrapError(err)
	}
	d.reset(nextOffset)
	return nil
}

// PeekKind returns the kind of the current value without consuming it.
// This allows for look-ahead parsing similar to jsontext.Decoder.PeekKind().
func (d *Decoder) PeekKind() (Kind, error) {
	//nolint:dogsled // only the resolved kind matters here
	kindNum, _, _, _, err := d.resolveCtrlData(
		d.offset,
	)
	if err != nil {
		return 0, d.wrapError(err)
	}
	return kindNum, nil
}

// Offset returns the current offset position in the database.
// If the current position points to a pointer, this method resolves one
// pointer and returns the offset of the actual data. This ensures
// that multiple pointers to the same data return the same offset, which
// is important for caching purposes.
func (d *Decoder) Offset() uint {
	// This intentionally does not use resolveCtrlData: Offset must return the
	// resolved value's control-byte offset, not the post-control-byte payload
	// offset used by read methods.
	kindNum, size, ctrlEndOffset, err := d.d.decodeCtrlData(d.offset)
	if err != nil || kindNum != KindPointer {
		return d.offset
	}

	pointer, _, err := d.d.decodePointer(size, ctrlEndOffset)
	if err != nil {
		// Return original offset to avoid breaking the public API.
		// The caller will encounter the same error when they try to read.
		return d.offset
	}
	kindNum, _, _, err = d.d.decodeCtrlData(pointer)
	if err != nil || kindNum == KindPointer {
		return d.offset
	}
	return pointer
}

func (d *Decoder) reset(offset uint) {
	d.offset = offset
	d.hasNextOffset = false
	d.nextOffset = 0
}

func (d *Decoder) setNextOffset(offset uint) {
	if !d.hasNextOffset {
		d.hasNextOffset = true
		d.nextOffset = offset
	}
}

func unexpectedKindErr(expectedKind, actualKind Kind) error {
	return fmt.Errorf("unexpected kind %d, expected %d", actualKind, expectedKind)
}

func (d *Decoder) decodeCtrlDataAndFollow(expectedKind Kind) (uint, uint, error) {
	kindNum, size, dataOffset, nextOffset, err := d.resolveCtrlData(d.offset)
	if err != nil {
		return 0, 0, err // Don't wrap here, let caller wrap
	}
	if nextOffset != 0 {
		d.setNextOffset(nextOffset)
	}
	if kindNum != expectedKind {
		return 0, 0, unexpectedKindErr(expectedKind, kindNum)
	}
	return size, dataOffset, nil
}

// resolveCtrlData resolves at most one pointer starting at offset and returns
// the control data for the non-pointer value. Unlike decodeCtrlData, which
// reads exactly one control record, this helper also reports the pointer's end
// offset so callers can preserve the decoder's sequential "next value" position
// after resolving indirection. Pointer-to-pointer data is invalid.
//
// nextOffset is 0 when offset already pointed at a non-pointer value (no
// indirection was followed). Callers must check this before calling
// setNextOffset; passing 0 would clobber the sequential position. A genuine
// pointer-end offset is always >= 2 because a pointer occupies a control byte
// plus at least one payload byte.
func (d *Decoder) resolveCtrlData(
	offset uint,
) (kind Kind, size, dataOffset, nextOffset uint, err error) {
	kind, size, dataOffset, err = d.d.decodeCtrlData(offset)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if kind != KindPointer {
		return kind, size, dataOffset, 0, nil
	}

	var pointerEndOffset uint
	dataOffset, pointerEndOffset, err = d.d.decodePointer(size, dataOffset)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	nextOffset = pointerEndOffset

	kind, size, dataOffset, err = d.d.decodeCtrlData(dataOffset)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if kind == KindPointer {
		return 0, 0, 0, 0, mmdberrors.NewInvalidDatabaseError("pointer-to-pointer chain detected")
	}

	return kind, size, dataOffset, nextOffset, nil
}

func (d *Decoder) readBytes(kind Kind) ([]byte, error) {
	size, offset, err := d.decodeCtrlDataAndFollow(kind)
	if err != nil {
		return nil, err // Return unwrapped - caller will wrap
	}

	if offset+size > uint(len(d.d.getBuffer())) {
		return nil, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (offset+size %d exceeds buffer length %d)",
			offset+size,
			len(d.d.getBuffer()),
		)
	}
	d.setNextOffset(offset + size)
	return d.d.getBuffer()[offset : offset+size], nil
}
