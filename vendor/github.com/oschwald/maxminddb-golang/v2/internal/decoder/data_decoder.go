package decoder

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// Kind constants for the different MMDB data kinds.
type Kind int

// MMDB data kind constants.
const (
	// KindExtended indicates an extended kind.
	KindExtended Kind = iota
	// KindPointer is a pointer to another location in the data section.
	KindPointer
	// KindString is a UTF-8 string.
	KindString
	// KindFloat64 is a 64-bit floating point number.
	KindFloat64
	// KindBytes is a byte slice.
	KindBytes
	// KindUint16 is a 16-bit unsigned integer.
	KindUint16
	// KindUint32 is a 32-bit unsigned integer.
	KindUint32
	// KindMap is a map from strings to other data types.
	KindMap
	// KindInt32 is a 32-bit signed integer.
	KindInt32
	// KindUint64 is a 64-bit unsigned integer.
	KindUint64
	// KindUint128 is a 128-bit unsigned integer.
	KindUint128
	// KindSlice is an array of values.
	KindSlice
	// KindContainer is a data cache container.
	KindContainer
	// KindEndMarker marks the end of the data section.
	KindEndMarker
	// KindBool is a boolean value.
	KindBool
	// KindFloat32 is a 32-bit floating point number.
	KindFloat32
)

// String returns a human-readable name for the Kind.
func (k Kind) String() string {
	switch k {
	case KindExtended:
		return "Extended"
	case KindPointer:
		return "Pointer"
	case KindString:
		return "String"
	case KindFloat64:
		return "Float64"
	case KindBytes:
		return "Bytes"
	case KindUint16:
		return "Uint16"
	case KindUint32:
		return "Uint32"
	case KindMap:
		return "Map"
	case KindInt32:
		return "Int32"
	case KindUint64:
		return "Uint64"
	case KindUint128:
		return "Uint128"
	case KindSlice:
		return "Slice"
	case KindContainer:
		return "Container"
	case KindEndMarker:
		return "EndMarker"
	case KindBool:
		return "Bool"
	case KindFloat32:
		return "Float32"
	default:
		return fmt.Sprintf("Unknown(%d)", int(k))
	}
}

// IsContainer returns true if the Kind represents a container type (Map or Slice).
func (k Kind) IsContainer() bool {
	return k == KindMap || k == KindSlice
}

// IsScalar returns true if the Kind represents a scalar value type.
func (k Kind) IsScalar() bool {
	switch k {
	case KindString, KindFloat64, KindBytes, KindUint16, KindUint32,
		KindInt32, KindUint64, KindUint128, KindBool, KindFloat32:
		return true
	default:
		return false
	}
}

// DataDecoder is a decoder for the MMDB data section.
// This is exported so mmdbdata package can use it, but still internal.
type DataDecoder struct {
	stringCache *stringCache
	buffer      []byte
}

const (
	// This is the value used in libmaxminddb.
	maximumDataStructureDepth    = 512
	containerPreflightValueCount = 1024
	pointerBase2                 = 2048
	pointerBase3                 = 526336
)

// NewDataDecoder creates a [DataDecoder].
func NewDataDecoder(buffer []byte) DataDecoder {
	return DataDecoder{
		buffer:      buffer,
		stringCache: newStringCache(),
	}
}

// NewDataDecoderWithoutStringCache creates a DataDecoder that does not retain
// decoded strings. It is intended for one-shot decoding where cache setup
// would cost more than repeated string allocation.
func NewDataDecoderWithoutStringCache(buffer []byte) DataDecoder {
	return DataDecoder{buffer: buffer}
}

// getBuffer returns the underlying buffer for direct access.
func (d *DataDecoder) getBuffer() []byte {
	return d.buffer
}

// decodeCtrlData decodes the control byte and data info at the given offset.
// Encoding follows the MaxMind DB spec: the control byte's high 3 bits
// encode the type (or KindExtended if zero, in which case the next byte
// holds kind-7 and the decoder adds 7 back), and the low 5 bits encode
// size. Sizes 0..28 are encoded directly; 29 reads 1 extra byte (+29),
// 30 reads 2 (+285), and 31 reads 3 (+65821).
func (d *DataDecoder) decodeCtrlData(offset uint) (Kind, uint, uint, error) {
	bufferLen := uint(len(d.buffer))
	newOffset := offset + 1
	if offset >= bufferLen {
		return 0, 0, 0, mmdberrors.NewOffsetError()
	}
	ctrlByte := d.buffer[offset]

	kindNum := Kind(ctrlByte >> 5)
	if kindNum == KindExtended {
		if newOffset >= bufferLen {
			return 0, 0, 0, mmdberrors.NewOffsetError()
		}
		kindNum = Kind(d.buffer[newOffset] + 7)
		newOffset++
	}

	size := uint(ctrlByte & 0x1f)
	if size < 29 {
		return kindNum, size, newOffset, nil
	}
	// Pointer control bits encode the pointer width and high address bits, not
	// an extended value size. Width-four pointers may use all three high address
	// bits even though they are ignored, producing raw values 29 through 31.
	if kindNum == KindPointer {
		return kindNum, size, newOffset, nil
	}

	endOffset := newOffset + size - 28
	if endOffset > bufferLen {
		return 0, 0, 0, mmdberrors.NewOffsetError()
	}

	switch size {
	case 29:
		return kindNum, 29 + uint(d.buffer[newOffset]), newOffset + 1, nil
	case 30:
		value := uint(d.buffer[newOffset])<<8 | uint(d.buffer[newOffset+1])
		return kindNum, 285 + value, endOffset, nil
	default: // size == 31
		value := uint(d.buffer[newOffset])<<16 |
			uint(d.buffer[newOffset+1])<<8 |
			uint(d.buffer[newOffset+2])
		return kindNum, 65821 + value, endOffset, nil
	}
}

// decodeBytes decodes a byte slice from the given offset with the given size.
func (d *DataDecoder) decodeBytes(size, offset uint) ([]byte, uint, error) {
	if offset+size > uint(len(d.buffer)) {
		return nil, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	bytes := make([]byte, size)
	copy(bytes, d.buffer[offset:newOffset])
	return bytes, newOffset, nil
}

// DecodeFloat64 decodes a 64-bit float from the given offset.
func (d *DataDecoder) decodeFloat64(size, offset uint) (float64, uint, error) {
	if size != 8 {
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (float 64 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	bits := binary.BigEndian.Uint64(d.buffer[offset:newOffset])
	return math.Float64frombits(bits), newOffset, nil
}

// DecodeFloat32 decodes a 32-bit float from the given offset.
func (d *DataDecoder) decodeFloat32(size, offset uint) (float32, uint, error) {
	if size != 4 {
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (float32 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	bits := binary.BigEndian.Uint32(d.buffer[offset:newOffset])
	return math.Float32frombits(bits), newOffset, nil
}

// DecodeInt32 decodes a 32-bit signed integer from the given offset.
func (d *DataDecoder) decodeInt32(size, offset uint) (int32, uint, error) {
	if size > 4 {
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (int32 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	var val int32
	for _, b := range d.buffer[offset:newOffset] {
		val = (val << 8) | int32(b)
	}
	return val, newOffset, nil
}

// DecodePointer decodes a pointer from the given offset.
func (d *DataDecoder) decodePointer(
	size uint,
	offset uint,
) (uint, uint, error) {
	pointerSize := ((size >> 3) & 0x3) + 1
	newOffset := offset + pointerSize
	if newOffset > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}
	pointerBytes := d.buffer[offset:newOffset]
	var prefix uint
	if pointerSize == 4 {
		prefix = 0
	} else {
		prefix = size & 0x7
	}
	unpacked := uintFromBytes(prefix, pointerBytes)

	var pointerValueOffset uint
	switch pointerSize {
	case 1, 4:
		pointerValueOffset = 0
	case 2:
		pointerValueOffset = pointerBase2
	case 3:
		pointerValueOffset = pointerBase3
	default:
		return 0, 0, mmdberrors.NewInvalidDatabaseError("invalid pointer size: %d", pointerSize)
	}

	pointer := unpacked + pointerValueOffset

	return pointer, newOffset, nil
}

// DecodeBool decodes a boolean from the given offset.
func (*DataDecoder) decodeBool(size, offset uint) (bool, uint, error) {
	if size > 1 {
		return false, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (bool size of %v)",
			size,
		)
	}
	value, newOffset := decodeBool(size, offset)
	return value, newOffset, nil
}

// DecodeString decodes a string from the given offset.
func (d *DataDecoder) decodeString(size, dataOffset uint) (string, uint, error) {
	if dataOffset+size > uint(len(d.buffer)) {
		return "", 0, mmdberrors.NewOffsetError()
	}

	newOffset := dataOffset + size
	if d.stringCache == nil {
		return string(d.buffer[dataOffset:newOffset]), newOffset, nil
	}
	headerSize := uint(1)
	if size >= 29 {
		headerSize = 2
		if size >= 285 {
			headerSize = 3
			if size >= 65821 {
				headerSize = 4
			}
		}
	}
	if dataOffset < headerSize {
		return "", 0, mmdberrors.NewOffsetError()
	}
	cacheOffset := dataOffset - headerSize
	value := d.stringCache.internAt(cacheOffset, d.buffer[dataOffset:newOffset])
	return value, newOffset, nil
}

// decodeStringValue decodes a string or one pointer to a string and returns
// the successor in the original containing stream.
//
//nolint:nestif // Keep common compact encodings inline on this hot path.
func (d *DataDecoder) decodeStringValue(offset uint) (string, uint, error) {
	bufferLen := uint(len(d.buffer))
	if offset < bufferLen {
		ctrlByte := d.buffer[offset]
		kind := Kind(ctrlByte >> 5)
		size := uint(ctrlByte & 0x1f)
		switch kind {
		case KindString:
			if size < 29 {
				dataOffset := offset + 1
				nextOffset := dataOffset + size
				if nextOffset <= bufferLen {
					value, _, err := d.decodeString(size, dataOffset)
					return value, nextOffset, err
				}
			}
		case KindPointer:
			if size < 8 && offset+2 <= bufferLen {
				pointer := (size&0x7)<<8 | uint(d.buffer[offset+1])
				if pointer < bufferLen {
					pointedCtrlByte := d.buffer[pointer]
					if Kind(pointedCtrlByte>>5) == KindString {
						pointedSize := uint(pointedCtrlByte & 0x1f)
						dataOffset := pointer + 1
						if pointedSize < 29 && dataOffset+pointedSize <= bufferLen {
							value, _, err := d.decodeString(pointedSize, dataOffset)
							return value, offset + 2, err
						}
					}
				}
			}
			if size >= 8 {
				payloadOffset := offset + 1
				pointerSize := ((size >> 3) & 0x3) + 1
				pointerEnd := payloadOffset + pointerSize
				if pointerEnd <= bufferLen {
					var pointer uint
					switch pointerSize {
					case 2:
						pointer = ((size&0x7)<<16 |
							uint(d.buffer[payloadOffset])<<8 |
							uint(d.buffer[payloadOffset+1])) + pointerBase2
					case 3:
						pointer = ((size&0x7)<<24 |
							uint(d.buffer[payloadOffset])<<16 |
							uint(d.buffer[payloadOffset+1])<<8 |
							uint(d.buffer[payloadOffset+2])) + pointerBase3
					case 4:
						pointer = uint(d.buffer[payloadOffset])<<24 |
							uint(d.buffer[payloadOffset+1])<<16 |
							uint(d.buffer[payloadOffset+2])<<8 |
							uint(d.buffer[payloadOffset+3])
					}
					if pointer < bufferLen {
						pointedCtrlByte := d.buffer[pointer]
						if Kind(pointedCtrlByte>>5) == KindString {
							pointedSize := uint(pointedCtrlByte & 0x1f)
							dataOffset := pointer + 1
							if pointedSize < 29 && pointedSize <= bufferLen-dataOffset {
								value, _, err := d.decodeString(pointedSize, dataOffset)
								return value, pointerEnd, err
							}
						}
					}
				}
			}
		default:
		}
	}

	kind, size, dataOffset, nextOffset, err := d.resolveCtrlData(offset)
	if err != nil {
		return "", 0, err
	}
	if nextOffset == 0 {
		nextOffset = dataOffset + size
	}
	if kind != KindString {
		return "", 0, unexpectedKindErr(KindString, kind)
	}
	value, _, err := d.decodeString(size, dataOffset)
	if err != nil {
		return "", 0, err
	}
	return value, nextOffset, nil
}

// DecodeUint16 decodes a 16-bit unsigned integer from the given offset.
func (d *DataDecoder) decodeUint16(size, offset uint) (uint16, uint, error) {
	if size > 2 {
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (uint16 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	bytes := d.buffer[offset:newOffset]

	var val uint16
	for _, b := range bytes {
		val = (val << 8) | uint16(b)
	}
	return val, newOffset, nil
}

// DecodeUint32 decodes a 32-bit unsigned integer from the given offset.
func (d *DataDecoder) decodeUint32(size, offset uint) (uint32, uint, error) {
	if size > 4 {
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (uint32 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	bytes := d.buffer[offset:newOffset]

	var val uint32
	for _, b := range bytes {
		val = (val << 8) | uint32(b)
	}
	return val, newOffset, nil
}

// DecodeUint64 decodes a 64-bit unsigned integer from the given offset.
func (d *DataDecoder) decodeUint64(size, offset uint) (uint64, uint, error) {
	if size > 8 {
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (uint64 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset := offset + size
	bytes := d.buffer[offset:newOffset]

	var val uint64
	for _, b := range bytes {
		val = (val << 8) | uint64(b)
	}
	return val, newOffset, nil
}

// DecodeUint128 decodes a 128-bit unsigned integer from the given offset.
// Returns the value as high and low 64-bit unsigned integers.
func (d *DataDecoder) decodeUint128(size, offset uint) (hi, lo uint64, newOffset uint, err error) {
	if size > 16 {
		return 0, 0, 0, mmdberrors.NewInvalidDatabaseError(
			"the MaxMind DB file's data section contains bad data (uint128 size of %v)",
			size,
		)
	}
	if offset+size > uint(len(d.buffer)) {
		return 0, 0, 0, mmdberrors.NewOffsetError()
	}

	newOffset = offset + size

	// Process bytes from most significant to least significant
	for _, b := range d.buffer[offset:newOffset] {
		var carry byte
		lo, carry = append64(lo, b)
		hi, _ = append64(hi, carry)
	}

	return hi, lo, newOffset, nil
}

func append64(val uint64, b byte) (uint64, byte) {
	return (val << 8) | uint64(b), byte(val >> 56)
}

// decodePointerKeyFast is an allocation-free inline of decodePointer followed
// by decodeKeyAt for the dominant case of a map key encoded as a pointer to a
// short string. On success, returns the key bytes (aliasing d.buffer), their
// data offset, and the offset past the pointer. On any deviation from the
// fast-path shape it returns ok=false, signaling the caller to fall through to
// the slow path — this is not an error and is re-validated downstream:
//
//   - bail-outs for OOB pointer reads are re-encountered by decodeCtrlData /
//     decodePointer in the slow path and surfaced as typed errors.
//   - pointedSize >= 29 selects the extended-size encoding, which requires
//     additional control bytes the slow path knows how to read.
//   - non-KindString targets and pointer-to-pointer chains are spec violations
//     that the slow path reports.
//
// pointerSize == 4 uses no bias (pointerBase 0); sizes 2 and 3 use
// pointerBase2 / pointerBase3. Size 1 also has no bias.
func (d *DataDecoder) decodePointerKeyFast(
	offset, ctrlByte, bufferLen uint,
) ([]byte, uint, uint, bool) {
	size := ctrlByte & 0x1f
	pointerSize := ((size >> 3) & 0x3) + 1
	newOffset := offset + 1 + pointerSize
	if newOffset > bufferLen {
		return nil, 0, 0, false
	}
	buffer := d.buffer
	payloadOffset := offset + 1
	var pointer uint
	switch pointerSize {
	case 1:
		pointer = (size&0x7)<<8 | uint(buffer[payloadOffset])
	case 2:
		pointer = ((size&0x7)<<16 |
			uint(buffer[payloadOffset])<<8 |
			uint(buffer[payloadOffset+1])) + pointerBase2
	case 3:
		pointer = ((size&0x7)<<24 |
			uint(buffer[payloadOffset])<<16 |
			uint(buffer[payloadOffset+1])<<8 |
			uint(buffer[payloadOffset+2])) + pointerBase3
	case 4:
		pointer = uint(buffer[payloadOffset])<<24 |
			uint(buffer[payloadOffset+1])<<16 |
			uint(buffer[payloadOffset+2])<<8 |
			uint(buffer[payloadOffset+3])
	default:
		return nil, 0, 0, false
	}
	if pointer >= bufferLen {
		return nil, 0, 0, false
	}
	pointedCtrlByte := buffer[pointer]
	if Kind(pointedCtrlByte>>5) != KindString {
		return nil, 0, 0, false
	}
	pointedSize := uint(pointedCtrlByte & 0x1f)
	if pointedSize >= 29 {
		return nil, 0, 0, false
	}
	dataOffset := pointer + 1
	if dataOffset+pointedSize > bufferLen {
		return nil, 0, 0, false
	}
	return buffer[dataOffset : dataOffset+pointedSize], dataOffset, newOffset, true
}

// decodeKey decodes a map key into a []byte slice. We use []byte so that we
// can take advantage of https://github.com/golang/go/issues/3512 to avoid
// copying the bytes when decoding a struct. Previously, we achieved this by
// using unsafe.
func (d *DataDecoder) decodeKey(offset uint) ([]byte, uint, error) {
	key, _, nextOffset, err := d.decodeKeyAt(offset)
	return key, nextOffset, err
}

// decodeStringKey validates and decodes a map key while preserving the source
// control-record offset needed by the string cache. Returned strings never
// alias d.buffer.
func (d *DataDecoder) decodeStringKey(offset uint) (string, uint, error) {
	key, cacheOffset, nextOffset, err := d.decodeKeyAt(offset)
	if err != nil {
		return "", 0, err
	}
	if d.stringCache == nil {
		return string(key), nextOffset, nil
	}
	return d.stringCache.internAt(cacheOffset, key), nextOffset, nil
}

// decodeKeyAt also returns the key's control-record offset for callers that
// need to retain a safe copy through the string cache.
//
//nolint:nestif // Keep common compact encodings inline on this hot path.
func (d *DataDecoder) decodeKeyAt(offset uint) ([]byte, uint, uint, error) {
	bufferLen := uint(len(d.buffer))
	if offset >= bufferLen {
		return nil, 0, 0, mmdberrors.NewOffsetError()
	}

	ctrlByte := d.buffer[offset]
	kind := Kind(ctrlByte >> 5)
	// Fast paths for the two dominant key shapes. Everything else — including
	// KindString with size >= 29 (extended encoding) and any fast-path
	// bail-out — falls through to the slow path below.
	switch kind {
	case KindString:
		size := uint(ctrlByte & 0x1f)
		if size < 29 {
			dataOffset := offset + 1
			newOffset := dataOffset + size
			if newOffset > bufferLen {
				return nil, 0, 0, mmdberrors.NewOffsetError()
			}
			return d.buffer[dataOffset:newOffset], offset, newOffset, nil
		}
	case KindPointer:
		// Database map keys overwhelmingly use the compact one-byte pointer
		// representation. Keep that case in decodeKeyAt so it does not pay a
		// second function call and pointer-size dispatch for every field.
		size := uint(ctrlByte & 0x1f)
		if size < 8 {
			newOffset := offset + 2
			if newOffset <= bufferLen {
				pointer := (size&0x7)<<8 | uint(d.buffer[offset+1])
				if pointer < bufferLen {
					pointedCtrlByte := d.buffer[pointer]
					if Kind(pointedCtrlByte>>5) == KindString {
						pointedSize := uint(pointedCtrlByte & 0x1f)
						dataOffset := pointer + 1
						if pointedSize < 29 && dataOffset+pointedSize <= bufferLen {
							return d.buffer[dataOffset : dataOffset+pointedSize],
								pointer, newOffset, nil
						}
					}
				}
			}
		}
		if key, dataOffset, newOffset, ok := d.decodePointerKeyFast(
			offset,
			uint(ctrlByte),
			bufferLen,
		); ok {
			return key, dataOffset - 1, newOffset, nil
		}
	default:
	}

	kindNum, size, dataOffset, nextOffset, err := d.resolveCtrlData(offset)
	if err != nil {
		return nil, 0, 0, err
	}
	if nextOffset == 0 {
		nextOffset = dataOffset + size
	}

	if kindNum != KindString {
		return nil, 0, 0, d.unexpectedMapKeyKind(offset, kindNum)
	}
	newOffset := dataOffset + size
	if newOffset > bufferLen {
		return nil, 0, 0, mmdberrors.NewOffsetError()
	}
	headerSize := uint(1)
	if size >= 29 {
		headerSize = 2
		if size >= 285 {
			headerSize = 3
			if size >= 65821 {
				headerSize = 4
			}
		}
	}
	if dataOffset < headerSize {
		return nil, 0, 0, mmdberrors.NewOffsetError()
	}
	return d.buffer[dataOffset:newOffset], dataOffset - headerSize, nextOffset, nil
}

//go:noinline
func (d *DataDecoder) unexpectedMapKeyKind(offset uint, kind Kind) error {
	if !kind.IsContainer() {
		validator := ReflectionDecoder{DataDecoder: *d}
		if _, err := validator.validateValueForAllocation(offset, 0, false); err != nil {
			return err
		}
	}
	return mmdberrors.NewInvalidDatabaseError("unexpected map key type: %s", kind)
}

// NextValueOffset skips ahead to the next value without decoding
// the one at the offset passed in. The size bits have different meanings for
// different data types.
func (d *DataDecoder) nextValueOffset(offset, numberToSkip uint) (uint, error) {
	bufferLen := uint(len(d.buffer))
	for numberToSkip > 0 {
		kindNum, size, newOffset, err := d.decodeCtrlData(offset)
		if err != nil {
			return 0, err
		}

		switch kindNum {
		case KindPointer:
			// A pointer value is represented by its pointer token only.
			// To skip it, just move past the pointer bytes; do NOT follow
			// the pointer target here.
			pointerSize := ((size >> 3) & 0x3) + 1
			ptrEndOffset := newOffset + pointerSize
			if ptrEndOffset > uint(len(d.buffer)) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset = ptrEndOffset
		case KindMap:
			if size > (^uint(0)-numberToSkip)/2 {
				return 0, mmdberrors.NewInvalidDatabaseError("container size overflow")
			}
			numberToSkip += 2 * size
		case KindSlice:
			if size > ^uint(0)-numberToSkip {
				return 0, mmdberrors.NewInvalidDatabaseError("container size overflow")
			}
			numberToSkip += size
		case KindBool:
			// size encodes the boolean; nothing else to skip
		default:
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		}

		offset = newOffset
		numberToSkip--
	}
	return offset, nil
}

func hasBufferRange(bufferLen, offset, size uint) bool {
	return size <= bufferLen && offset <= bufferLen-size
}

func decodeBool(size, offset uint) (bool, uint) {
	return size != 0, offset
}

func uintFromBytes(prefix uint, uintBytes []byte) uint {
	val := prefix
	for _, b := range uintBytes {
		val = (val << 8) | uint(b)
	}
	return val
}
