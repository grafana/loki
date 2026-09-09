package decoder

import (
	"errors"
	"reflect"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

var (
	errInvalidZeroMapCursor   = errors.New("invalid zero map cursor")
	errInvalidZeroMapReader   = errors.New("invalid zero map reader")
	errInvalidZeroSliceCursor = errors.New("invalid zero slice cursor")
)

// Cursor identifies one value in the decoder's input. Cursor values are
// immutable. Read methods return a successor cursor positioned immediately
// after the value in its containing stream. The zero value is not readable.
type Cursor struct {
	decoder     *DataDecoder
	offset      uint
	originToken uint
}

// MapCursor incrementally reads a map without rescanning values that have
// already been completely consumed. Its zero value is invalid; obtain one by
// calling Cursor.Map.
type MapCursor struct {
	decoder     *DataDecoder
	err         error
	valueOrigin uint
	offset      uint
	outerEnd    uint
	size        uint
	remaining   uint
	pending     bool
}

// MapReader provides the counted map traversal used by generated struct
// decoders. It deliberately avoids mutable iterator state on the hot path.
type MapReader struct {
	decoder     *DataDecoder
	valueOrigin uint
	dataOffset  uint
	outerEnd    uint
	size        uint
}

// SliceCursor incrementally reads a slice without rescanning values that have
// already been completely consumed. Its zero value is invalid; obtain one by
// calling Cursor.Slice.
type SliceCursor struct {
	decoder     *DataDecoder
	err         error
	valueOrigin uint
	dataOffset  uint
	offset      uint
	outerEnd    uint
	size        uint
	remaining   uint
	index       uint
	valueOffset uint
	pending     bool
}

// Cursor returns an immutable cursor for the decoder's current value.
func (d *Decoder) Cursor() Cursor {
	return Cursor{decoder: d.cursorDecoder, offset: d.offset}
}

// Advance moves the decoder to a successor cursor returned by a Cursor read.
// It rejects cursors from another decoder or from an unrelated value.
func (d *Decoder) Advance(next Cursor) error {
	if next.decoder != d.cursorDecoder {
		return errors.New("cannot advance with a cursor from another decoder")
	}
	if next.originToken != d.offset+1 {
		return errors.New("cursor is not the successor of the current value")
	}
	d.reset(next.offset)
	return nil
}

// Kind returns the resolved kind at the cursor without consuming it.
func (c Cursor) Kind() (Kind, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}
	//nolint:dogsled // Only the resolved kind is needed.
	kind, _, _, _, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return 0, c.wrapError(err)
	}
	return kind, nil
}

// Skip returns a cursor immediately after the current value.
func (c Cursor) Skip() (Cursor, error) {
	if err := c.validate(); err != nil {
		return Cursor{}, err
	}
	next, err := c.decoder.nextValueOffset(c.offset, 1)
	if err != nil {
		return Cursor{}, c.wrapError(err)
	}
	return c.successor(next), nil
}

// ReadBool reads a bool and returns its successor cursor.
func (c Cursor) ReadBool() (bool, Cursor, error) {
	size, dataOffset, next, err := c.scalar(KindBool)
	if err != nil {
		return false, Cursor{}, err
	}
	value, _, err := c.decoder.decodeBool(size, dataOffset)
	if err != nil {
		return false, Cursor{}, c.wrapError(err)
	}
	return value, c.successor(next), nil
}

// ReadString reads a string and returns its successor cursor.
func (c Cursor) ReadString() (string, Cursor, error) {
	if err := c.validate(); err != nil {
		return "", Cursor{}, err
	}
	value, next, err := c.decoder.decodeStringValue(c.offset)
	if err != nil {
		var mismatch UnexpectedKindError
		if errors.As(err, &mismatch) {
			return "", Cursor{}, c.unexpectedKinds(mismatch.Expected, mismatch.Actual)
		}
		return "", Cursor{}, c.wrapError(err)
	}
	return value, c.successor(next), nil
}

// ReadBytes reads bytes and returns its successor cursor. The returned bytes
// alias the decoder input and must not be modified. Copy them before retaining
// them.
func (c Cursor) ReadBytes() ([]byte, Cursor, error) {
	size, dataOffset, next, err := c.scalar(KindBytes)
	if err != nil {
		return nil, Cursor{}, err
	}
	buffer := c.decoder.getBuffer()
	if dataOffset > uint(len(buffer)) || size > uint(len(buffer))-dataOffset {
		return nil, Cursor{}, c.wrapError(malformedRangeError(dataOffset, size, len(buffer)))
	}
	return buffer[dataOffset : dataOffset+size], c.successor(next), nil
}

// ReadFloat32 reads a float32 and returns its successor cursor.
func (c Cursor) ReadFloat32() (float32, Cursor, error) {
	size, dataOffset, next, err := c.scalar(KindFloat32)
	if err != nil {
		return 0, Cursor{}, err
	}
	value, _, err := c.decoder.decodeFloat32(size, dataOffset)
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}
	return value, c.successor(next), nil
}

// ReadFloat64 reads a float64 and returns its successor cursor.
func (c Cursor) ReadFloat64() (float64, Cursor, error) {
	size, dataOffset, next, err := c.scalar(KindFloat64)
	if err != nil {
		return 0, Cursor{}, err
	}
	value, _, err := c.decoder.decodeFloat64(size, dataOffset)
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}
	return value, c.successor(next), nil
}

// ReadFloat reads either MMDB floating-point kind as a float64 and returns its
// successor cursor.
//
//nolint:nestif // Keep common direct encodings inline on this hot path.
func (c Cursor) ReadFloat() (float64, Cursor, error) {
	if err := c.validate(); err != nil {
		return 0, Cursor{}, err
	}
	buffer := c.decoder.buffer
	if c.offset < uint(len(buffer)) {
		ctrlByte := buffer[c.offset]
		size := uint(ctrlByte & 0x1f)
		if size < 29 {
			switch Kind(ctrlByte >> 5) {
			case KindFloat64:
				value, end, err := c.decoder.decodeFloat64(size, c.offset+1)
				if err != nil {
					return 0, Cursor{}, c.wrapError(err)
				}
				return value, c.successor(end), nil
			case KindExtended:
				if c.offset+1 < uint(len(buffer)) && Kind(buffer[c.offset+1]+7) == KindFloat32 {
					value, end, err := c.decoder.decodeFloat32(size, c.offset+2)
					if err != nil {
						return 0, Cursor{}, c.wrapError(err)
					}
					return float64(value), c.successor(end), nil
				}
			default:
			}
		}
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}
	var value float64
	var end uint
	switch kind {
	case KindFloat32:
		var decoded float32
		decoded, end, err = c.decoder.decodeFloat32(size, dataOffset)
		value = float64(decoded)
	case KindFloat64:
		value, end, err = c.decoder.decodeFloat64(size, dataOffset)
	default:
		return 0, Cursor{}, c.unexpectedKinds(
			NewKindSet(KindFloat64, KindFloat32),
			kind,
		)
	}
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}
	if pointerEnd != 0 {
		end = pointerEnd
	}
	return value, c.successor(end), nil
}

// ReadInt32 reads an int32 and returns its successor cursor.
func (c Cursor) ReadInt32() (int32, Cursor, error) {
	size, dataOffset, next, err := c.scalar(KindInt32)
	if err != nil {
		return 0, Cursor{}, err
	}
	value, _, err := c.decoder.decodeInt32(size, dataOffset)
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}
	return value, c.successor(next), nil
}

// ReadUint reads any MMDB unsigned integer kind up to 64 bits and returns its
// successor cursor.
func (c Cursor) ReadUint() (uint64, Cursor, error) {
	if err := c.validate(); err != nil {
		return 0, Cursor{}, err
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}

	var value uint64
	var end uint
	switch kind {
	case KindUint16:
		var v uint16
		v, end, err = c.decoder.decodeUint16(size, dataOffset)
		value = uint64(v)
	case KindUint32:
		var v uint32
		v, end, err = c.decoder.decodeUint32(size, dataOffset)
		value = uint64(v)
	case KindUint64:
		value, end, err = c.decoder.decodeUint64(size, dataOffset)
	default:
		return 0, Cursor{}, c.unexpectedKinds(
			NewKindSet(KindUint16, KindUint32, KindUint64),
			kind,
		)
	}
	if err != nil {
		return 0, Cursor{}, c.wrapError(err)
	}
	if pointerEnd != 0 {
		end = pointerEnd
	}
	return value, c.successor(end), nil
}

// ReadInteger reads any MMDB integer kind up to 64 bits in one pass. Signed
// values are returned as the uint64 bit pattern of their int64 representation.
func (c Cursor) ReadInteger() (value uint64, signed bool, next Cursor, err error) {
	if err := c.validate(); err != nil {
		return 0, false, Cursor{}, err
	}
	buffer := c.decoder.buffer
	if c.offset < uint(len(buffer)) {
		ctrlByte := buffer[c.offset]
		size := uint(ctrlByte & 0x1f)
		dataOffset := c.offset + 1
		switch Kind(ctrlByte >> 5) {
		case KindUint16:
			if size >= 29 {
				break
			}
			value, end, err := c.decoder.decodeUint16(size, dataOffset)
			if err != nil {
				return 0, false, Cursor{}, c.wrapError(err)
			}
			return uint64(value), false, c.successor(end), nil
		case KindUint32:
			if size >= 29 {
				break
			}
			value, end, err := c.decoder.decodeUint32(size, dataOffset)
			if err != nil {
				return 0, false, Cursor{}, c.wrapError(err)
			}
			return uint64(value), false, c.successor(end), nil
		}
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return 0, false, Cursor{}, c.wrapError(err)
	}
	var end uint
	switch kind {
	case KindInt32:
		var decoded int32
		decoded, end, err = c.decoder.decodeInt32(size, dataOffset)
		value = uint64(int64(decoded))
		signed = true
	case KindUint16:
		var decoded uint16
		decoded, end, err = c.decoder.decodeUint16(size, dataOffset)
		value = uint64(decoded)
	case KindUint32:
		var decoded uint32
		decoded, end, err = c.decoder.decodeUint32(size, dataOffset)
		value = uint64(decoded)
	case KindUint64:
		value, end, err = c.decoder.decodeUint64(size, dataOffset)
	default:
		return 0, false, Cursor{}, c.unexpectedKinds(
			NewKindSet(KindUint16, KindUint32, KindInt32, KindUint64),
			kind,
		)
	}
	if err != nil {
		return 0, false, Cursor{}, c.wrapError(err)
	}
	if pointerEnd != 0 {
		end = pointerEnd
	}
	return value, signed, c.successor(end), nil
}

// ReadUint128 reads a uint128 as high and low 64-bit words and returns its
// successor cursor.
func (c Cursor) ReadUint128() (uint64, uint64, Cursor, error) {
	size, dataOffset, next, err := c.scalar(KindUint128)
	if err != nil {
		return 0, 0, Cursor{}, err
	}
	hi, lo, _, err := c.decoder.decodeUint128(size, dataOffset)
	if err != nil {
		return 0, 0, Cursor{}, c.wrapError(err)
	}
	return hi, lo, c.successor(next), nil
}

// Map opens the current value as a map.
func (c Cursor) Map() (MapCursor, error) {
	if err := c.validate(); err != nil {
		return MapCursor{}, err
	}
	buffer := c.decoder.buffer
	if c.offset < uint(len(buffer)) {
		ctrlByte := buffer[c.offset]
		size := uint(ctrlByte & 0x1f)
		if Kind(ctrlByte>>5) == KindMap && size < 29 {
			dataOffset := c.offset + 1
			if err := validateCursorContainerSize(
				c.decoder,
				KindMap,
				size,
				dataOffset,
			); err != nil {
				return MapCursor{}, c.wrapError(err)
			}
			return MapCursor{
				decoder:     c.decoder,
				valueOrigin: c.offset,
				offset:      dataOffset,
				size:        size,
				remaining:   size,
			}, nil
		}
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return MapCursor{}, c.wrapError(err)
	}
	if kind != KindMap {
		return MapCursor{}, c.unexpectedKind(KindMap, kind)
	}
	bufferLen := uint(len(buffer))
	if dataOffset > bufferLen ||
		(size >= containerPreflightValueCount && size > (bufferLen-dataOffset)/2) {
		return MapCursor{}, c.wrapError(mmdberrors.NewOffsetError())
	}
	if err := validateCursorContainerSize(c.decoder, KindMap, size, dataOffset); err != nil {
		return MapCursor{}, c.wrapError(err)
	}
	return MapCursor{
		decoder:     c.decoder,
		valueOrigin: c.offset,
		offset:      dataOffset,
		outerEnd:    pointerEnd,
		size:        size,
		remaining:   size,
	}, nil
}

// MapReader opens the current value for generated counted map traversal.
func (c Cursor) MapReader() (MapReader, error) {
	if err := c.validate(); err != nil {
		return MapReader{}, err
	}
	buffer := c.decoder.buffer
	if c.offset < uint(len(buffer)) {
		ctrlByte := buffer[c.offset]
		size := uint(ctrlByte & 0x1f)
		if Kind(ctrlByte>>5) == KindMap && size < 29 {
			dataOffset := c.offset + 1
			return MapReader{
				decoder:     c.decoder,
				valueOrigin: c.offset,
				dataOffset:  dataOffset,
				size:        size,
			}, nil
		}
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return MapReader{}, c.wrapError(err)
	}
	if kind != KindMap {
		return MapReader{}, c.unexpectedKind(KindMap, kind)
	}
	bufferLen := uint(len(buffer))
	if dataOffset > bufferLen ||
		(size >= containerPreflightValueCount && size > (bufferLen-dataOffset)/2) {
		return MapReader{}, c.wrapError(mmdberrors.NewOffsetError())
	}
	return MapReader{
		decoder:     c.decoder,
		valueOrigin: c.offset,
		dataOffset:  dataOffset,
		outerEnd:    pointerEnd,
		size:        size,
	}, nil
}

// Len returns the declared number of map entries.
func (m MapReader) Len() uint { return m.size }

// Size validates the declared entry count for use as an allocation hint. Large
// maps receive a complete structural preflight before Size returns successfully.
func (m MapReader) Size() (uint, error) {
	if size, ok := m.SizeForAllocation(); ok {
		return size, nil
	}
	if m.decoder == nil {
		return 0, errInvalidZeroMapReader
	}
	if err := validateCursorContainerSize(
		m.decoder,
		KindMap,
		m.size,
		m.dataOffset,
	); err != nil {
		return 0, wrapErrorAtOffset(err, m.valueOrigin)
	}
	return m.size, nil
}

// SizeForAllocation returns the declared size and true when a cheap bounds
// check is sufficient to use it as an allocation hint. Call Size when ok is
// false to perform the complete allocation preflight or retrieve a reader error.
func (m MapReader) SizeForAllocation() (size uint, ok bool) {
	if m.decoder == nil || !cursorContainerSizeIsCheap(
		KindMap,
		m.size,
		m.dataOffset,
		uint(len(m.decoder.buffer)),
	) {
		return 0, false
	}
	return m.size, true
}

// First returns a cursor positioned at the first map key.
func (m MapReader) First() Cursor {
	return Cursor{decoder: m.decoder, offset: m.dataOffset}
}

// End returns the map successor after the caller has iterated exactly Len
// entries from First. The caller is responsible for supplying the cursor after
// those entries; End cannot verify map membership or the iteration count.
func (m MapReader) End(next Cursor) (Cursor, error) {
	if m.decoder == nil {
		return Cursor{}, errInvalidZeroMapReader
	}
	if next.decoder != m.decoder {
		return Cursor{}, errors.New("map successor belongs to another decoder")
	}
	if next.originToken == 0 && (m.size != 0 || next.offset != m.dataOffset) {
		return Cursor{}, errors.New("map successor is not proven to follow a decoded value")
	}
	end := next.offset
	if m.outerEnd != 0 {
		end = m.outerEnd
	}
	return Cursor{
		decoder:     m.decoder,
		offset:      end,
		originToken: m.valueOrigin + 1,
	}, nil
}

// Slice opens the current value as a slice.
func (c Cursor) Slice() (SliceCursor, error) {
	if err := c.validate(); err != nil {
		return SliceCursor{}, err
	}
	buffer := c.decoder.buffer
	bufferLen := uint(len(buffer))
	if c.offset < bufferLen && c.offset+1 < bufferLen {
		ctrlByte := buffer[c.offset]
		size := uint(ctrlByte & 0x1f)
		if Kind(ctrlByte>>5) == KindExtended &&
			Kind(buffer[c.offset+1])+7 == KindSlice && size < 29 {
			dataOffset := c.offset + 2
			if size > bufferLen-dataOffset {
				return SliceCursor{}, c.wrapError(mmdberrors.NewOffsetError())
			}
			return SliceCursor{
				decoder:     c.decoder,
				valueOrigin: c.offset,
				dataOffset:  dataOffset,
				offset:      dataOffset,
				size:        size,
				remaining:   size,
			}, nil
		}
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return SliceCursor{}, c.wrapError(err)
	}
	if kind != KindSlice {
		return SliceCursor{}, c.unexpectedKind(KindSlice, kind)
	}
	if dataOffset > bufferLen || size > bufferLen-dataOffset {
		return SliceCursor{}, c.wrapError(mmdberrors.NewOffsetError())
	}
	return SliceCursor{
		decoder:     c.decoder,
		valueOrigin: c.offset,
		dataOffset:  dataOffset,
		offset:      dataOffset,
		outerEnd:    pointerEnd,
		size:        size,
		remaining:   size,
	}, nil
}

// Unmarshal invokes an existing custom unmarshaler at the cursor and returns
// a validated successor cursor. It is intended for generated decoders with
// nested fields that already implement Unmarshaler. A nil interface or an
// interface containing a typed nil is rejected.
func (c Cursor) Unmarshal(value Unmarshaler) (Cursor, error) {
	if err := c.validate(); err != nil {
		return Cursor{}, err
	}
	if value == nil {
		return Cursor{}, errors.New("cannot unmarshal into nil")
	}
	reflected := reflect.ValueOf(value)
	if isNilableDynamicKind(reflected.Kind()) && reflected.IsNil() {
		return Cursor{}, errors.New("cannot unmarshal into nil")
	}
	child := acquireDecoder(c.decoder, c.offset)
	if err := value.UnmarshalMaxMindDB(child); err != nil {
		releaseDecoder(child)
		return Cursor{}, err
	}
	releaseDecoder(child)
	next, err := c.decoder.nextValueOffset(c.offset, 1)
	if err != nil {
		return Cursor{}, c.wrapError(err)
	}
	return c.successor(next), nil
}

// UnmarshalCursor invokes an existing cursor unmarshaler and validates that it
// returned the proven successor of this cursor. A nil interface or an interface
// containing a typed nil is rejected.
func (c Cursor) UnmarshalCursor(value CursorUnmarshaler) (Cursor, error) {
	if err := c.validate(); err != nil {
		return Cursor{}, err
	}
	if value == nil {
		return Cursor{}, errors.New("cannot unmarshal into nil")
	}
	reflected := reflect.ValueOf(value)
	if isNilableDynamicKind(reflected.Kind()) && reflected.IsNil() {
		return Cursor{}, errors.New("cannot unmarshal into nil")
	}
	next, err := value.UnmarshalMaxMindDBCursor(c)
	if err != nil {
		return Cursor{}, err
	}
	if next.decoder != c.decoder {
		return Cursor{}, errors.New("cursor unmarshaler returned a cursor from another decoder")
	}
	if next.originToken != c.offset+1 {
		return Cursor{}, errors.New("cursor unmarshaler did not return the successor of its input")
	}
	return next, nil
}

func isNilableDynamicKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}

// Size returns the entry count validated when Map opened the container. It
// returns zero for an uninitialized cursor, whose Err method reports an error.
func (m *MapCursor) Size() uint { return m.size }

// Next consumes the proven successor of the previous map value and returns the
// next key and value cursor. Pass a zero Cursor on the first call. The returned
// key aliases the decoder input and must not be modified. Copy it before
// retaining it.
func (m *MapCursor) Next(successor Cursor) ([]byte, Cursor, bool) {
	if m.err != nil {
		return nil, Cursor{}, false
	}
	if m.pending {
		if successor.decoder != m.decoder || successor.originToken != m.offset+1 {
			m.err = errors.New("cursor is not the successor of the current map value")
			return nil, Cursor{}, false
		}
		m.offset = successor.offset
		m.remaining--
		m.pending = false
	} else if successor.decoder != nil {
		m.err = errors.New("map has no current value")
		return nil, Cursor{}, false
	}
	if m.remaining == 0 {
		return nil, Cursor{}, false
	}
	key, valueOffset, err := m.decoder.decodeKey(m.offset)
	if err != nil {
		m.err = wrapErrorAtOffset(err, m.offset)
		return nil, Cursor{}, false
	}
	m.offset = valueOffset
	m.pending = true
	return key, Cursor{decoder: m.decoder, offset: m.offset}, true
}

// Err returns the first map iteration error.
func (m *MapCursor) Err() error {
	if m.err != nil {
		return m.err
	}
	if m.decoder == nil {
		return errInvalidZeroMapCursor
	}
	return nil
}

// End returns the proven successor of the complete map.
func (m *MapCursor) End() (Cursor, error) {
	if err := m.Err(); err != nil {
		return Cursor{}, err
	}
	if m.pending || m.remaining != 0 {
		return Cursor{}, errors.New("map was not completely consumed")
	}
	end := m.offset
	if m.outerEnd != 0 {
		end = m.outerEnd
	}
	return Cursor{
		decoder:     m.decoder,
		offset:      end,
		originToken: m.valueOrigin + 1,
	}, nil
}

// ReadMapKey reads a map key and returns a cursor positioned at its value. The
// returned key aliases the decoder input and must not be modified. Copy it
// before retaining it. ReadMapKey supports the generated counted-map loop used
// by maxminddb-gen.
func (c Cursor) ReadMapKey() ([]byte, Cursor, error) {
	if err := c.validate(); err != nil {
		return nil, Cursor{}, err
	}
	key, valueOffset, err := c.decoder.decodeKey(c.offset)
	if err != nil {
		return nil, Cursor{}, c.wrapError(err)
	}
	return key, Cursor{decoder: c.decoder, offset: valueOffset}, nil
}

// Size validates and returns the number of elements declared by the slice.
func (s *SliceCursor) Size() (uint, error) {
	if err := s.Err(); err != nil {
		return 0, err
	}
	if err := validateCursorContainerSize(
		s.decoder,
		KindSlice,
		s.size,
		s.dataOffset,
	); err != nil {
		s.err = wrapErrorAtOffset(err, s.valueOrigin)
		return 0, s.err
	}
	return s.size, nil
}

// SizeForCapacity returns the declared size and true when the destination has
// sufficient capacity and the opening bounds check is enough to reuse it.
// Call Size when ok is false to perform the allocation preflight or retrieve
// an existing cursor error.
func (s *SliceCursor) SizeForCapacity(capacity int) (size uint, ok bool) {
	if s.err != nil || s.decoder == nil {
		return 0, false
	}
	if capacity >= 0 && uint(capacity) >= s.size && s.size < containerPreflightValueCount {
		return s.size, true
	}
	return 0, false
}

// Next consumes the proven successor of the previous slice value and returns
// the next index and value cursor. Pass a zero Cursor on the first call.
func (s *SliceCursor) Next(successor Cursor) (uint, Cursor, bool) {
	if s.err != nil {
		return 0, Cursor{}, false
	}
	if s.pending {
		if successor.decoder != s.decoder || successor.originToken != s.valueOffset+1 {
			s.err = errors.New("cursor is not the successor of the current slice value")
			return 0, Cursor{}, false
		}
		s.offset = successor.offset
		s.remaining--
		s.index++
		s.pending = false
	} else if successor.decoder != nil {
		s.err = errors.New("slice has no current value")
		return 0, Cursor{}, false
	}
	if s.remaining == 0 {
		return 0, Cursor{}, false
	}
	s.valueOffset = s.offset
	s.pending = true
	return s.index, Cursor{decoder: s.decoder, offset: s.offset}, true
}

// Err returns the first slice iteration error.
func (s *SliceCursor) Err() error {
	if s.err != nil {
		return s.err
	}
	if s.decoder == nil {
		return errInvalidZeroSliceCursor
	}
	return nil
}

// End returns the proven successor of the complete slice.
func (s *SliceCursor) End() (Cursor, error) {
	if err := s.Err(); err != nil {
		return Cursor{}, err
	}
	if s.pending || s.remaining != 0 {
		return Cursor{}, errors.New("slice was not completely consumed")
	}
	end := s.offset
	if s.outerEnd != 0 {
		end = s.outerEnd
	}
	return Cursor{
		decoder:     s.decoder,
		offset:      end,
		originToken: s.valueOrigin + 1,
	}, nil
}

//nolint:nestif // Keep common direct encodings inline on this hot path.
func (c Cursor) scalar(expected Kind) (uint, uint, uint, error) {
	if err := c.validate(); err != nil {
		return 0, 0, 0, err
	}
	buffer := c.decoder.buffer
	if c.offset < uint(len(buffer)) {
		ctrlByte := buffer[c.offset]
		size := uint(ctrlByte & 0x1f)
		if size < 29 {
			kind := Kind(ctrlByte >> 5)
			dataOffset := c.offset + 1
			if kind == KindExtended && dataOffset < uint(len(buffer)) {
				kind = Kind(buffer[dataOffset] + 7)
				dataOffset++
			}
			if kind == expected {
				next := dataOffset + size
				if kind == KindBool {
					next = dataOffset
				}
				return size, dataOffset, next, nil
			}
		}
	}
	kind, size, dataOffset, pointerEnd, err := c.decoder.resolveCtrlData(c.offset)
	if err != nil {
		return 0, 0, 0, c.wrapError(err)
	}
	if kind != expected {
		return 0, 0, 0, c.unexpectedKind(expected, kind)
	}
	next := dataOffset + size
	if kind == KindBool {
		next = dataOffset
	}
	if pointerEnd != 0 {
		next = pointerEnd
	}
	return size, dataOffset, next, nil
}

func (c Cursor) successor(offset uint) Cursor {
	return Cursor{
		decoder:     c.decoder,
		offset:      offset,
		originToken: c.offset + 1,
	}
}

func (c Cursor) validate() error {
	if c.decoder == nil {
		return errors.New("invalid zero cursor")
	}
	return nil
}

func (c Cursor) wrapError(err error) error {
	return wrapErrorAtOffset(err, c.offset)
}

func (c Cursor) unexpectedKind(expected, actual Kind) error {
	return c.unexpectedKinds(NewKindSet(expected), actual)
}

func (c Cursor) unexpectedKinds(expected KindSet, actual Kind) error {
	// Reflection reports a container type mismatch after its header without
	// validating small container contents. Scalars, however, are decoded and
	// bounds-checked before their destination type is rejected.
	if !actual.IsContainer() {
		validator := ReflectionDecoder{DataDecoder: *c.decoder}
		if _, err := validator.validateValueForAllocation(c.offset, 0, false); err != nil {
			return c.wrapError(err)
		}
	}
	return c.wrapError(unexpectedKindsErr(expected, actual))
}

func validateCursorContainerSize(d *DataDecoder, kind Kind, size, offset uint) error {
	// Ordinary containers receive cheap size and bounds checks here, then full
	// validation while they are traversed. Large containers use reflection's
	// complete structural preflight before allocation.
	if cursorContainerSizeIsCheap(kind, size, offset, uint(len(d.buffer))) {
		return nil
	}
	validator := ReflectionDecoder{DataDecoder: *d}
	return validator.validateContainerSize(kind, size, offset, 0)
}

func cursorContainerSizeIsCheap(kind Kind, size, offset, bufferLen uint) bool {
	valueCount := size
	if kind == KindMap {
		if size > ^uint(0)/2 {
			return false
		}
		valueCount = size * 2
	}
	return offset <= bufferLen && valueCount <= bufferLen-offset &&
		valueCount < containerPreflightValueCount
}

func malformedRangeError(offset, size uint, length int) error {
	return mmdberrors.NewInvalidDatabaseError(
		"the MaxMind DB file's data section contains bad data (offset+size %d exceeds buffer length %d)",
		offset+size,
		length,
	)
}
