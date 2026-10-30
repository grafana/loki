// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package variant

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"time"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/parquet/internal/debug"
	"github.com/google/uuid"
)

//go:generate go tool stringer -type=BasicType -linecomment -output=basic_type_stringer.go
//go:generate go tool stringer -type=PrimitiveType -linecomment -output=primitive_type_stringer.go

// BasicType represents the fundamental type category of a variant value.
type BasicType int

const (
	BasicUndefined   BasicType = iota - 1 // Unknown
	BasicPrimitive                        // Primitive
	BasicShortString                      // ShortString
	BasicObject                           // Object
	BasicArray                            // Array
)

func basicTypeFromHeader(hdr byte) BasicType {
	// because we're doing hdr & 0x3, it is impossible for the result
	// to be outside of the range of BasicType. Therefore, we don't
	// need to perform any checks. The value will always be [0,3]
	return BasicType(hdr & basicTypeMask)
}

// PrimitiveType represents specific primitive data types within the variant format.
type PrimitiveType int

const (
	PrimitiveInvalid            PrimitiveType = iota - 1 // Unknown
	PrimitiveNull                                        // Null
	PrimitiveBoolTrue                                    // BoolTrue
	PrimitiveBoolFalse                                   // BoolFalse
	PrimitiveInt8                                        // Int8
	PrimitiveInt16                                       // Int16
	PrimitiveInt32                                       // Int32
	PrimitiveInt64                                       // Int64
	PrimitiveDouble                                      // Double
	PrimitiveDecimal4                                    // Decimal32
	PrimitiveDecimal8                                    // Decimal64
	PrimitiveDecimal16                                   // Decimal128
	PrimitiveDate                                        // Date
	PrimitiveTimestampMicros                             // Timestamp(micros)
	PrimitiveTimestampMicrosNTZ                          // TimestampNTZ(micros)
	PrimitiveFloat                                       // Float
	PrimitiveBinary                                      // Binary
	PrimitiveString                                      // String
	PrimitiveTimeMicrosNTZ                               // TimeNTZ(micros)
	PrimitiveTimestampNanos                              // Timestamp(nanos)
	PrimitiveTimestampNanosNTZ                           // TimestampNTZ(nanos)
	PrimitiveUUID                                        // UUID
)

func primitiveTypeFromHeader(hdr byte) PrimitiveType {
	return PrimitiveType((hdr >> basicTypeBits) & typeInfoMask)
}

// Type represents the high-level variant data type.
// This is what applications typically use to identify the type of a variant value.
type Type int

const (
	Object Type = iota
	Array
	Null
	Bool
	Int8
	Int16
	Int32
	Int64
	String
	Double
	Decimal4
	Decimal8
	Decimal16
	Date
	TimestampMicros
	TimestampMicrosNTZ
	Float
	Binary
	Time
	TimestampNanos
	TimestampNanosNTZ
	UUID
)

const (
	versionMask        uint8 = 0x0F
	sortedStrMask      uint8 = 0b10000
	basicTypeMask      uint8 = 0x3
	basicTypeBits      uint8 = 2
	typeInfoMask       uint8 = 0x3F
	hdrSizeBytes             = 1
	minOffsetSizeBytes       = 1
	maxOffsetSizeBytes       = 4

	// mask is applied after shift
	offsetSizeMask       uint8 = 0b11
	offsetSizeBitShift   uint8 = 6
	supportedVersion           = 1
	maxShortStringSize         = 0x3F
	metadataMaxSizeLimit       = 128 * 1024 * 1024 // 128MB
	maxValidationDepth         = 1024              // bounds memory used while validating nested values.
)

var (
	// EmptyMetadataBytes contains a minimal valid metadata section with no dictionary entries.
	EmptyMetadataBytes = [3]byte{0x1, 0, 0}

	ErrInvalidMetadata = errors.New("invalid variant metadata")
)

// Metadata represents the dictionary part of a variant value, which stores
// the keys used in object values.
type Metadata struct {
	data []byte
	keys [][]byte
}

// NewMetadata creates a Metadata instance from a raw byte slice.
// It validates the metadata format and loads the key dictionary.
func NewMetadata(data []byte) (Metadata, error) {
	m := Metadata{data: data}
	if len(data) < hdrSizeBytes+minOffsetSizeBytes*2 {
		return m, fmt.Errorf("%w: too short: size=%d", ErrInvalidMetadata, len(data))
	}

	if m.Version() != supportedVersion {
		return m, fmt.Errorf("%w: unsupported version: %d", ErrInvalidMetadata, m.Version())
	}

	offsetSz := m.OffsetSize()
	return m, m.loadDictionary(offsetSz)
}

// Clone creates a deep copy of the metadata.
func (m *Metadata) Clone() Metadata {
	clone := Metadata{data: bytes.Clone(m.data)}
	if len(clone.data) > 0 && len(m.keys) > 0 {
		if err := clone.loadDictionary(clone.OffsetSize()); err == nil {
			return clone
		}
	}

	clone.keys = make([][]byte, len(m.keys))
	for i, key := range m.keys {
		clone.keys[i] = bytes.Clone(key)
	}
	return clone
}

func (m *Metadata) loadDictionary(offsetSz uint8) error {
	if int(offsetSz+hdrSizeBytes) > len(m.data) {
		return fmt.Errorf("%w: too short for dictionary size", ErrInvalidMetadata)
	}

	dictSize := readLEU32(m.data[hdrSizeBytes : hdrSizeBytes+offsetSz])
	if dictSize == 0 {
		m.keys = nil
		return nil
	}

	valuesStart := uint64(hdrSizeBytes) + (uint64(dictSize)+2)*uint64(offsetSz)
	if valuesStart > uint64(len(m.data)) {
		return fmt.Errorf("%w: offset table out of range: %d > %d",
			ErrInvalidMetadata, valuesStart, len(m.data))
	}

	offsetPos := hdrSizeBytes + offsetSz
	if first := readLEU32(m.data[offsetPos : offsetPos+offsetSz]); first != 0 {
		return fmt.Errorf("%w: first offset must be zero: %d", ErrInvalidMetadata, first)
	}

	m.keys = make([][]byte, dictSize)
	offsetStart := uint32(0)
	for i := range dictSize {
		offsetPos += offsetSz
		end := readLEU32(m.data[offsetPos : offsetPos+offsetSz])
		if end < offsetStart {
			return fmt.Errorf("%w: offsets are not monotonic: %d < %d",
				ErrInvalidMetadata, end, offsetStart)
		}
		if valuesStart+uint64(end) > uint64(len(m.data)) {
			return fmt.Errorf("%w: string data out of range: %d + %d > %d",
				ErrInvalidMetadata, valuesStart, end, len(m.data))
		}

		m.keys[i] = m.data[valuesStart+uint64(offsetStart) : valuesStart+uint64(end)]
		offsetStart = end
	}

	return nil
}

// Bytes returns the raw byte representation of the metadata.
func (m Metadata) Bytes() []byte { return m.data }

// Version returns the metadata format version.
func (m Metadata) Version() uint8 { return m.data[0] & versionMask }

// SortedAndUnique returns whether the keys in the metadata dictionary are sorted and unique.
func (m Metadata) SortedAndUnique() bool { return m.data[0]&sortedStrMask != 0 }

// OffsetSize returns the size in bytes used to store offsets in the metadata.
func (m Metadata) OffsetSize() uint8 {
	return ((m.data[0] >> offsetSizeBitShift) & offsetSizeMask) + 1
}

// DictionarySize returns the number of keys in the metadata dictionary.
func (m Metadata) DictionarySize() uint32 { return uint32(len(m.keys)) }

// SizeBytes returns the metadata's own byte length, so a caller can split a
// buffer that stores metadata concatenated with a value at data[m.SizeBytes():].
func (m Metadata) SizeBytes() int {
	offsetSz := uint32(m.OffsetSize())
	if uint32(len(m.data)) < uint32(hdrSizeBytes)+offsetSz {
		return len(m.data)
	}

	dictSize := readLEU32(m.data[hdrSizeBytes : uint32(hdrSizeBytes)+offsetSz])
	// Final offset (index dictSize) is the string-region length; it starts at valuesStart.
	lastOffsetPos := uint32(hdrSizeBytes) + offsetSz*(1+dictSize)
	valuesStart := lastOffsetPos + offsetSz
	if valuesStart > uint32(len(m.data)) {
		return len(m.data)
	}

	return int(valuesStart + readLEU32(m.data[lastOffsetPos:valuesStart]))
}

// KeyAt returns the string key at the given dictionary ID.
// Returns an error if the ID is out of range.
func (m Metadata) KeyAt(id uint32) (string, error) {
	if id >= uint32(len(m.keys)) {
		return "", fmt.Errorf("invalid variant metadata: id out of range: %d >= %d",
			id, len(m.keys))
	}

	key := m.keys[id]
	return unsafe.String(unsafe.SliceData(key), len(key)), nil
}

// IdFor returns the dictionary IDs for the given key.
// If the metadata is sorted and unique, this performs a binary search.
// Otherwise, it performs a linear search.
//
// If the metadata is not sorted and unique, then it's possible that multiple
// IDs will be returned for the same key.
func (m Metadata) IdFor(key string) []uint32 {
	k := unsafe.Slice(unsafe.StringData(key), len(key))

	var ret []uint32
	if m.SortedAndUnique() {
		idx, found := slices.BinarySearchFunc(m.keys, k, bytes.Compare)
		if found {
			ret = append(ret, uint32(idx))
		}

		return ret
	}

	for i, kb := range m.keys {
		if bytes.Equal(kb, k) {
			ret = append(ret, uint32(i))
		}
	}

	return ret
}

// DecimalValue represents a decimal number with a specified scale.
// The generic parameter T can be any supported variant decimal type (Decimal32, Decimal64, Decimal128).
type DecimalValue[T decimal.DecimalTypes] struct {
	Scale uint8
	Value decimal.Num[T]
}

// MarshalJSON implements the json.Marshaler interface for DecimalValue.
func (v DecimalValue[T]) MarshalJSON() ([]byte, error) {
	return []byte(v.Value.ToString(int32(v.Scale))), nil
}

// ArrayValue represents an array of variant values.
type ArrayValue struct {
	value []byte
	meta  Metadata

	numElements uint32
	dataStart   uint64
	offsetSize  uint8
	offsetStart uint64
}

// MarshalJSON implements the json.Marshaler interface for ArrayValue.
func (v ArrayValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(slices.Collect(v.Values()))
}

// Len returns the number of elements in the array.
func (v ArrayValue) Len() uint32 { return v.numElements }

// Values returns an iterator for the elements in the array, allowing
// for lazy evaluation of the offsets (for the situation where not all elements
// are iterated).
func (v ArrayValue) Values() iter.Seq[Value] {
	return func(yield func(Value) bool) {
		for i := range v.numElements {
			idx := v.offsetStart + uint64(i)*uint64(v.offsetSize)
			offset := readLEU32(v.value[idx : idx+uint64(v.offsetSize)])

			if !yield(trimValue(v.meta, v.value[v.dataStart+uint64(offset):])) {
				return
			}
		}
	}
}

// Value returns the Value at the specified index.
// Returns an error if the index is out of range.
func (v ArrayValue) Value(i uint32) (Value, error) {
	if i >= v.numElements {
		return Value{}, fmt.Errorf("%w: invalid array value: index out of range: %d >= %d",
			arrow.ErrIndex, i, v.numElements)
	}

	idx := v.offsetStart + uint64(i)*uint64(v.offsetSize)
	offset := readLEU32(v.value[idx : idx+uint64(v.offsetSize)])

	return trimValue(v.meta, v.value[v.dataStart+uint64(offset):]), nil
}

// ObjectValue represents an object (map/dictionary) of key-value pairs.
type ObjectValue struct {
	value []byte
	meta  Metadata

	numElements uint32
	offsetStart uint64
	dataStart   uint64
	idSize      uint8
	offsetSize  uint8
	idStart     uint64
}

// ObjectField represents a key-value pair in an object.
type ObjectField struct {
	Key   string
	Value Value
}

// NumElements returns the number of fields in the object.
func (v ObjectValue) NumElements() uint32 { return v.numElements }

// ValueByKey returns the field with the specified key.
// Returns arrow.ErrNotFound if the key doesn't exist.
func (v ObjectValue) ValueByKey(key string) (ObjectField, error) {
	n := v.numElements

	// if total list size is smaller than threshold, linear search will
	// likely be faster than a binary search
	const binarySearchThreshold = 32
	if n < binarySearchThreshold {
		for i := range n {
			idx := v.idStart + uint64(i)*uint64(v.idSize)
			id := readLEU32(v.value[idx : idx+uint64(v.idSize)])
			k, err := v.meta.KeyAt(id)
			if err != nil {
				return ObjectField{}, fmt.Errorf("invalid object value: fieldID at idx %d is not in metadata", idx)
			}
			if k == key {
				idx := v.offsetStart + uint64(v.offsetSize)*uint64(i)
				offset := readLEU32(v.value[idx : idx+uint64(v.offsetSize)])
				return ObjectField{
					Key:   key,
					Value: trimValue(v.meta, v.value[v.dataStart+uint64(offset):])}, nil
			}
		}
		return ObjectField{}, arrow.ErrNotFound
	}

	i, j := uint32(0), n
	for i < j {
		mid := (i + j) >> 1
		idx := v.idStart + uint64(mid)*uint64(v.idSize)
		id := readLEU32(v.value[idx : idx+uint64(v.idSize)])
		k, err := v.meta.KeyAt(id)
		if err != nil {
			return ObjectField{}, fmt.Errorf("invalid object value: fieldID at idx %d is not in metadata", idx)
		}

		switch strings.Compare(k, key) {
		case -1:
			i = mid + 1
		case 0:
			idx := v.offsetStart + uint64(v.offsetSize)*uint64(mid)
			offset := readLEU32(v.value[idx : idx+uint64(v.offsetSize)])

			return ObjectField{
				Key:   key,
				Value: trimValue(v.meta, v.value[v.dataStart+uint64(offset):])}, nil
		case 1:
			j = mid
		}
	}

	return ObjectField{}, arrow.ErrNotFound
}

// FieldAt returns the field at the specified index.
// Returns an error if the index is out of range.
func (v ObjectValue) FieldAt(i uint32) (ObjectField, error) {
	if i >= v.numElements {
		return ObjectField{}, fmt.Errorf("%w: invalid object value: index out of range: %d >= %d",
			arrow.ErrIndex, i, v.numElements)
	}

	idx := v.idStart + uint64(i)*uint64(v.idSize)
	id := readLEU32(v.value[idx : idx+uint64(v.idSize)])
	k, err := v.meta.KeyAt(id)
	if err != nil {
		return ObjectField{}, fmt.Errorf("invalid object value: fieldID at idx %d is not in metadata", idx)
	}

	offsetIdx := v.offsetStart + uint64(i)*uint64(v.offsetSize)
	offset := readLEU32(v.value[offsetIdx : offsetIdx+uint64(v.offsetSize)])

	return ObjectField{
		Key:   k,
		Value: trimValue(v.meta, v.value[v.dataStart+uint64(offset):])}, nil
}

// Values returns an iterator over all key-value pairs in the object.
func (v ObjectValue) Values() iter.Seq2[string, Value] {
	return func(yield func(string, Value) bool) {
		for i := range v.numElements {
			idx := v.idStart + uint64(i)*uint64(v.idSize)
			id := readLEU32(v.value[idx : idx+uint64(v.idSize)])
			k, err := v.meta.KeyAt(id)
			if err != nil {
				return
			}

			offsetIdx := v.offsetStart + uint64(i)*uint64(v.offsetSize)
			offset := readLEU32(v.value[offsetIdx : offsetIdx+uint64(v.offsetSize)])

			if !yield(k, trimValue(v.meta, v.value[v.dataStart+uint64(offset):])) {
				return
			}
		}
	}
}

// MarshalJSON implements the json.Marshaler interface for ObjectValue.
func (v ObjectValue) MarshalJSON() ([]byte, error) {
	// for now we'll use a naive approach and just build a map
	// then marshal it. This is not the most efficient way to do this
	// but it is the simplest and most straightforward.
	mapping := make(map[string]Value)
	maps.Insert(mapping, v.Values())
	return json.Marshal(mapping)
}

var NullValue = Value{meta: Metadata{data: EmptyMetadataBytes[:]}, value: []byte{0}}

// Value represents a variant value of any type.
type Value struct {
	value []byte
	meta  Metadata
}

func trimValue(meta Metadata, value []byte) Value {
	return Value{value: value[:valueSize(value)], meta: meta}
}

// NewWithMetadata creates a Value with the provided metadata and value bytes.
func NewWithMetadata(meta Metadata, value []byte) (Value, error) {
	if len(value) == 0 {
		return Value{}, errors.New("invalid variant value: empty")
	}
	if err := validateValueBytes(meta, value); err != nil {
		return Value{}, err
	}

	return Value{value: value, meta: meta}, nil
}

func validateValueBytes(meta Metadata, value []byte) error {
	size, err := validateValue(meta, value)
	if err != nil {
		return err
	}
	if size != len(value) {
		return fmt.Errorf("invalid variant value: trailing bytes")
	}
	return nil
}

func validatePrimitiveValue(value []byte) (int, error) {
	primitiveType := primitiveTypeFromHeader(value[0])
	want := 0
	switch primitiveType {
	case PrimitiveNull, PrimitiveBoolTrue, PrimitiveBoolFalse:
		want = 1
	case PrimitiveInt8:
		want = 2
	case PrimitiveInt16:
		want = 3
	case PrimitiveInt32, PrimitiveDate, PrimitiveFloat:
		want = 5
	case PrimitiveInt64, PrimitiveDouble, PrimitiveTimeMicrosNTZ,
		PrimitiveTimestampMicros, PrimitiveTimestampMicrosNTZ,
		PrimitiveTimestampNanos, PrimitiveTimestampNanosNTZ:
		want = 9
	case PrimitiveDecimal4:
		want = 6
	case PrimitiveDecimal8:
		want = 10
	case PrimitiveDecimal16:
		want = 18
	case PrimitiveUUID:
		want = 17
	case PrimitiveBinary, PrimitiveString:
		if len(value) < 5 {
			return 0, fmt.Errorf("invalid variant value: %s length prefix requires 5 bytes, got %d", primitiveType, len(value))
		}
		dataLen := uint64(binary.LittleEndian.Uint32(value[1:5]))
		if dataLen > uint64(len(value)-5) {
			return 0, fmt.Errorf("invalid variant value: %s data requires %d bytes, got %d", primitiveType, dataLen, len(value)-5)
		}
		return 5 + int(dataLen), nil
	default:
		return 0, fmt.Errorf("invalid variant value: unknown primitive type %d", primitiveType)
	}

	if len(value) < want {
		return 0, fmt.Errorf("invalid variant value: %s requires %d bytes, got %d", primitiveType, want, len(value))
	}
	return want, nil
}

type validationRange struct {
	start uint64
	end   uint64
	field int
}

type validationFrame struct {
	value               []byte
	size                uint64
	dataSize            uint32
	dataStart           uint64
	offsetStart         uint64
	numChildren         uint32
	nextChild           uint32
	pendingIndex        uint32
	pendingStart        uint32
	pendingExpectedSize uint32
	rangeStart          uint32
	offsetSize          uint8
	kind                uint8
	initialized         bool
}

const (
	validationStackInlineCapacity = 32
	// Values that exceed the inline stack commonly need only a modest amount
	// of additional depth, so avoid allocating the maximum stack for them.
	validationStackIntermediateCapacity = 128
	// The root value is at depth zero, so the stack needs one more frame than
	// the maximum allowed nesting depth. Keeping this storage fixed prevents
	// untrusted values from growing the validation stack on the heap.
	validationStackCapacity       = maxValidationDepth + 1
	validationRangeInlineCapacity = 64
)

// validateValue walks compound values with an explicit stack so valid values
// do not consume the Go call stack. Nesting is bounded to keep validation
// memory usage independent of attacker-controlled input depth.
func validateValue(meta Metadata, value []byte) (int, error) {
	var stackStorage [validationStackInlineCapacity]validationFrame
	stack := stackStorage[:1]
	stack[0].value = value

	var rangeStorage [validationRangeInlineCapacity]validationRange
	ranges := rangeStorage[:0]
	return validateValueLoop(meta, value, stack, ranges, 0)
}

func validateValueLoop(meta Metadata, value []byte, stack []validationFrame, ranges []validationRange, rangeTop int) (int, error) {
	var (
		resultSize int
		resultErr  error
		hasResult  bool
	)

	for len(stack) > 0 {
		frame := &stack[len(stack)-1]
		if hasResult {
			hasResult = false

			if resultErr != nil {
				switch BasicType(frame.kind) {
				case BasicArray:
					return 0, fmt.Errorf("invalid variant value: array element %d: %w", frame.pendingIndex, resultErr)
				case BasicObject:
					return 0, fmt.Errorf("invalid variant value: object field %d: %w", frame.pendingIndex, resultErr)
				default:
					return 0, resultErr
				}
			}

			switch BasicType(frame.kind) {
			case BasicArray:
				if uint64(resultSize) != uint64(frame.pendingExpectedSize) {
					return 0, fmt.Errorf("invalid variant value: array element %d has trailing bytes", frame.pendingIndex)
				}
			case BasicObject:
				end := uint64(frame.pendingStart) + uint64(resultSize)
				if end > uint64(frame.dataSize) {
					return 0, fmt.Errorf("invalid variant value: object field %d extends beyond data", frame.pendingIndex)
				}
				if rangeTop < len(ranges) {
					ranges[rangeTop] = validationRange{
						start: uint64(frame.pendingStart),
						end:   end,
						field: int(frame.pendingIndex),
					}
				} else {
					ranges = append(ranges, validationRange{
						start: uint64(frame.pendingStart),
						end:   end,
						field: int(frame.pendingIndex),
					})
				}
				rangeTop++
			}
			continue
		}

		if !frame.initialized {
			frame.initialized = true
			if err := prepareValidationFrame(meta, frame); err != nil {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return 0, err
				}
				resultErr = err
				hasResult = true
				continue
			}
			if BasicType(frame.kind) == BasicObject {
				frame.rangeStart = uint32(rangeTop)
			}
		}

		if frame.kind == uint8(BasicArray) || frame.kind == uint8(BasicObject) {
			if frame.nextChild < frame.numChildren {
				if len(stack) == validationStackCapacity {
					return 0, fmt.Errorf("invalid variant value: maximum nesting depth exceeded")
				}
				if len(stack) == cap(stack) {
					if cap(stack) == validationStackInlineCapacity {
						return validateValueIntermediate(meta, value, stack, ranges, rangeTop)
					}
					return validateValueDeep(meta, value, stack, ranges, rangeTop)
				}

				child, index, start, expectedSize, err := nextValidationChild(frame)
				if err != nil {
					stack = stack[:len(stack)-1]
					if len(stack) == 0 {
						return 0, err
					}
					resultErr = err
					hasResult = true
					continue
				}

				frame.nextChild++
				frame.pendingIndex = uint32(index)
				frame.pendingStart = uint32(start)
				frame.pendingExpectedSize = uint32(expectedSize)
				stack = append(stack, validationFrame{value: child})
				continue
			}

			if err := finishValidationFrame(frame, ranges[int(frame.rangeStart):rangeTop]); err != nil {
				if BasicType(frame.kind) == BasicObject {
					rangeTop = int(frame.rangeStart)
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return 0, err
				}
				resultErr = err
				hasResult = true
				continue
			}
			if BasicType(frame.kind) == BasicObject {
				rangeTop = int(frame.rangeStart)
			}
		}

		resultSize = int(frame.size)
		stack = stack[:len(stack)-1]
		if len(stack) == 0 {
			return resultSize, nil
		}
		hasResult = true
	}

	return 0, errors.New("invalid variant value: validation stack exhausted")
}

func validateValueIntermediate(meta Metadata, value []byte, initialStack []validationFrame, ranges []validationRange, rangeTop int) (int, error) {
	var stackStorage [validationStackIntermediateCapacity]validationFrame
	stack := stackStorage[:len(initialStack)]
	copy(stack, initialStack)
	return validateValueLoop(meta, value, stack, ranges, rangeTop)
}

func validateValueDeep(meta Metadata, value []byte, initialStack []validationFrame, ranges []validationRange, rangeTop int) (int, error) {
	var stackStorage [validationStackCapacity]validationFrame
	stack := stackStorage[:len(initialStack)]
	copy(stack, initialStack)
	return validateValueLoop(meta, value, stack, ranges, rangeTop)
}

func finishValidationFrame(frame *validationFrame, ranges []validationRange) error {
	if BasicType(frame.kind) != BasicObject {
		return nil
	}

	slices.SortFunc(ranges, func(a, b validationRange) int {
		switch {
		case a.start < b.start:
			return -1
		case a.start > b.start:
			return 1
		default:
			return 0
		}
	})

	var (
		next          uint64
		previousField int
	)
	for _, child := range ranges {
		switch {
		case child.start < next:
			return fmt.Errorf("invalid variant value: object fields %d and %d overlap", previousField, child.field)
		case child.start > next:
			return fmt.Errorf("invalid variant value: object data has a gap before field %d", child.field)
		}
		next = child.end
		previousField = child.field
	}
	if next != uint64(frame.dataSize) {
		return fmt.Errorf("invalid variant value: object data has trailing bytes")
	}
	return nil
}

func prepareValidationFrame(meta Metadata, frame *validationFrame) error {
	if len(frame.value) == 0 {
		return errors.New("invalid variant value: empty")
	}

	frame.kind = uint8(basicTypeFromHeader(frame.value[0]))
	switch BasicType(frame.kind) {
	case BasicShortString:
		want := 1 + int(frame.value[0]>>basicTypeBits)
		if len(frame.value) < want {
			return fmt.Errorf("invalid variant value: short string requires %d bytes, got %d", want, len(frame.value))
		}
		frame.size = uint64(want)
	case BasicObject:
		return prepareObjectValidationFrame(meta, frame)
	case BasicArray:
		return prepareArrayValidationFrame(frame)
	case BasicPrimitive:
		size, err := validatePrimitiveValue(frame.value)
		frame.size = uint64(size)
		return err
	default:
		return fmt.Errorf("invalid variant value: unknown basic type %d", BasicType(frame.kind))
	}
	return nil
}

func prepareArrayValidationFrame(frame *validationFrame) error {
	value := frame.value
	typeInfo := value[0] >> basicTypeBits
	offsetSize := uint8(typeInfo&0b11) + 1
	isLarge := ((typeInfo >> 2) & 0x1) != 0

	var (
		numElements uint32
		offsetStart uint64
	)
	if isLarge {
		if len(value) < 5 {
			return fmt.Errorf("invalid variant value: array size requires 5 bytes, got %d", len(value))
		}
		numElements = readLEU32(value[1:5])
		offsetStart = 5
	} else {
		if len(value) < 2 {
			return fmt.Errorf("invalid variant value: array size requires 2 bytes, got %d", len(value))
		}
		numElements = uint32(value[1])
		offsetStart = 2
	}

	dataStart := offsetStart + (uint64(numElements)+1)*uint64(offsetSize)
	if dataStart > uint64(len(value)) {
		return fmt.Errorf("invalid variant value: array offset table ends at %d, got %d bytes", dataStart, len(value))
	}

	var previousOffset uint32
	for i := uint64(0); i <= uint64(numElements); i++ {
		pos := offsetStart + uint64(i)*uint64(offsetSize)
		offset := readLEU32(value[int(pos) : int(pos)+int(offsetSize)])
		if i == 0 && offset != 0 {
			return fmt.Errorf("invalid variant value: array first offset must be zero, got %d", offset)
		}
		if i > 0 && offset < previousOffset {
			return fmt.Errorf("invalid variant value: array offsets are not monotonic")
		}
		if dataStart+uint64(offset) > uint64(len(value)) {
			return fmt.Errorf("invalid variant value: array offset %d is out of range", offset)
		}
		previousOffset = offset
	}

	frame.dataStart = dataStart
	frame.offsetStart = offsetStart
	frame.offsetSize = offsetSize
	frame.numChildren = numElements
	frame.size = dataStart + uint64(previousOffset)
	return nil
}

func prepareObjectValidationFrame(meta Metadata, frame *validationFrame) error {
	value := frame.value
	typeInfo := value[0] >> basicTypeBits
	offsetSize := uint8(typeInfo&0b11) + 1
	idSize := uint8((typeInfo>>2)&0b11) + 1
	isLarge := ((typeInfo >> 4) & 0x1) != 0

	var (
		numElements uint32
		elementSize uint64 = 1
	)
	if isLarge {
		elementSize = 4
	}
	if uint64(len(value)) < 1+elementSize {
		return fmt.Errorf("invalid variant value: object size requires %d bytes, got %d", 1+elementSize, len(value))
	}
	numElements = readLEU32(value[1 : 1+elementSize])

	idStart := 1 + elementSize
	offsetStart := idStart + uint64(numElements)*uint64(idSize)
	dataStart := offsetStart + (uint64(numElements)+1)*uint64(offsetSize)
	if dataStart > uint64(len(value)) {
		return fmt.Errorf("invalid variant value: object offset table ends at %d, got %d bytes", dataStart, len(value))
	}
	finalOffsetPos := offsetStart + uint64(numElements)*uint64(offsetSize)
	dataSize := readLEU32(value[int(finalOffsetPos) : int(finalOffsetPos)+int(offsetSize)])
	if dataStart+uint64(dataSize) > uint64(len(value)) {
		return fmt.Errorf("invalid variant value: object data ends at %d, got %d bytes", dataStart+uint64(dataSize), len(value))
	}

	var previousKey string
	for i := range numElements {
		idPos := idStart + uint64(i)*uint64(idSize)
		id := readLEU32(value[int(idPos) : int(idPos)+int(idSize)])
		key, err := meta.KeyAt(id)
		if err != nil {
			return fmt.Errorf("invalid variant value: object field %d has invalid field ID %d: %w", i, id, err)
		}
		if i > 0 && strings.Compare(previousKey, key) >= 0 {
			return fmt.Errorf("invalid variant value: object field names are not strictly sorted at field %d", i)
		}
		previousKey = key

		offsetPos := offsetStart + uint64(i)*uint64(offsetSize)
		offset := readLEU32(value[int(offsetPos) : int(offsetPos)+int(offsetSize)])
		if uint64(offset) > uint64(dataSize) {
			return fmt.Errorf("invalid variant value: object field %d offset %d is out of range", i, offset)
		}
	}

	frame.dataStart = dataStart
	frame.offsetStart = offsetStart
	frame.offsetSize = offsetSize
	frame.numChildren = numElements
	frame.dataSize = dataSize
	frame.size = dataStart + uint64(dataSize)
	return nil
}

func nextValidationChild(frame *validationFrame) ([]byte, int, uint64, uint64, error) {
	index := int(frame.nextChild)
	position := frame.offsetStart + uint64(frame.nextChild)*uint64(frame.offsetSize)
	offset := readLEU32(frame.value[int(position) : int(position)+int(frame.offsetSize)])
	start := frame.dataStart + uint64(offset)

	if BasicType(frame.kind) == BasicArray {
		nextPosition := position + uint64(frame.offsetSize)
		nextOffset := readLEU32(frame.value[int(nextPosition) : int(nextPosition)+int(frame.offsetSize)])
		end := frame.dataStart + uint64(nextOffset)
		return frame.value[int(start):int(end)], index, 0, end - start, nil
	}

	if uint64(offset) > uint64(frame.dataSize) {
		return nil, index, 0, 0, fmt.Errorf("invalid variant value: object field %d offset %d is out of range", index, offset)
	}
	return frame.value[int(start):], index, uint64(offset), 0, nil
}

// New creates a Value by parsing both the metadata and value bytes.
func New(meta, value []byte) (Value, error) {
	m, err := NewMetadata(meta)
	if err != nil {
		return Value{}, err
	}

	return NewWithMetadata(m, value)
}

func (v Value) String() string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Bytes returns the raw byte representation of the value (excluding metadata).
func (v Value) Bytes() []byte { return v.value }

// Clone creates a deep copy of the value including its metadata.
func (v Value) Clone() Value {
	return Value{
		meta:  v.meta.Clone(),
		value: bytes.Clone(v.value),
	}
}

// Metadata returns the metadata associated with the value.
func (v Value) Metadata() Metadata { return v.meta }

// BasicType returns the fundamental type category of the value.
func (v Value) BasicType() BasicType {
	return basicTypeFromHeader(v.value[0])
}

// Type returns the specific data type of the value.
func (v Value) Type() Type {
	switch t := v.BasicType(); t {
	case BasicPrimitive:
		switch primType := primitiveTypeFromHeader(v.value[0]); primType {
		case PrimitiveNull:
			return Null
		case PrimitiveBoolTrue, PrimitiveBoolFalse:
			return Bool
		case PrimitiveInt8:
			return Int8
		case PrimitiveInt16:
			return Int16
		case PrimitiveInt32:
			return Int32
		case PrimitiveInt64:
			return Int64
		case PrimitiveDouble:
			return Double
		case PrimitiveDecimal4:
			return Decimal4
		case PrimitiveDecimal8:
			return Decimal8
		case PrimitiveDecimal16:
			return Decimal16
		case PrimitiveDate:
			return Date
		case PrimitiveTimestampMicros:
			return TimestampMicros
		case PrimitiveTimestampMicrosNTZ:
			return TimestampMicrosNTZ
		case PrimitiveFloat:
			return Float
		case PrimitiveBinary:
			return Binary
		case PrimitiveString:
			return String
		case PrimitiveTimeMicrosNTZ:
			return Time
		case PrimitiveTimestampNanos:
			return TimestampNanos
		case PrimitiveTimestampNanosNTZ:
			return TimestampNanosNTZ
		case PrimitiveUUID:
			return UUID
		default:
			panic(fmt.Errorf("invalid primitive type found: %d", primType))
		}
	case BasicShortString:
		return String
	case BasicObject:
		return Object
	case BasicArray:
		return Array
	default:
		panic(fmt.Errorf("invalid basic type found: %d", t))
	}
}

// Value returns the Go value representation of the variant.
// The returned type depends on the variant type:
//   - Null: nil
//   - Bool: bool
//   - Int8/16/32/64: corresponding int type
//   - Float/Double: float32/float64
//   - String: string
//   - Binary: []byte
//   - Decimal: DecimalValue
//   - Date: arrow.Date32
//   - Time: arrow.Time64
//   - Timestamp: arrow.Timestamp
//   - UUID: uuid.UUID
//   - Object: ObjectValue
//   - Array: ArrayValue
func (v Value) Value() any {
	switch t := v.BasicType(); t {
	case BasicPrimitive:
		switch primType := primitiveTypeFromHeader(v.value[0]); primType {
		case PrimitiveNull:
			return nil
		case PrimitiveBoolTrue:
			return true
		case PrimitiveBoolFalse:
			return false
		case PrimitiveInt8:
			return readExact[int8](v.value[1:])
		case PrimitiveInt16:
			return readExact[int16](v.value[1:])
		case PrimitiveInt32:
			return readExact[int32](v.value[1:])
		case PrimitiveInt64:
			return readExact[int64](v.value[1:])
		case PrimitiveDouble:
			return readExact[float64](v.value[1:])
		case PrimitiveFloat:
			return readExact[float32](v.value[1:])
		case PrimitiveDate:
			return arrow.Date32(readExact[int32](v.value[1:]))
		case PrimitiveTimestampMicros, PrimitiveTimestampMicrosNTZ,
			PrimitiveTimestampNanos, PrimitiveTimestampNanosNTZ:
			return arrow.Timestamp(readExact[int64](v.value[1:]))
		case PrimitiveTimeMicrosNTZ:
			return arrow.Time64(readExact[int64](v.value[1:]))
		case PrimitiveUUID:
			debug.Assert(len(v.value[1:]) == 16, "invalid UUID length")
			return uuid.Must(uuid.FromBytes(v.value[1:]))
		case PrimitiveBinary:
			sz := binary.LittleEndian.Uint32(v.value[1:5])
			return v.value[5 : 5+sz]
		case PrimitiveString:
			sz := binary.LittleEndian.Uint32(v.value[1:5])
			return unsafe.String(&v.value[5], sz)
		case PrimitiveDecimal4:
			scale := uint8(v.value[1])
			val := decimal.Decimal32(readExact[int32](v.value[2:]))
			return DecimalValue[decimal.Decimal32]{Scale: scale, Value: val}
		case PrimitiveDecimal8:
			scale := uint8(v.value[1])
			val := decimal.Decimal64(readExact[int64](v.value[2:]))
			return DecimalValue[decimal.Decimal64]{Scale: scale, Value: val}
		case PrimitiveDecimal16:
			scale := uint8(v.value[1])
			lowBits := readLEU64(v.value[2:10])
			highBits := readExact[int64](v.value[10:])
			return DecimalValue[decimal.Decimal128]{
				Scale: scale,
				Value: decimal128.New(highBits, lowBits),
			}
		}
	case BasicShortString:
		sz := int(v.value[0] >> 2)
		if sz > 0 {
			return unsafe.String(&v.value[1], sz)
		}
		return ""
	case BasicObject:
		valueHdr := (v.value[0] >> basicTypeBits)
		fieldOffsetSz := (valueHdr & 0b11) + 1
		fieldIdSz := ((valueHdr >> 2) & 0b11) + 1
		isLarge := ((valueHdr >> 4) & 0b1) == 1

		var nelemSize uint8 = 1
		if isLarge {
			nelemSize = 4
		}

		debug.Assert(len(v.value) >= int(1+nelemSize), "invalid object value: too short")
		numElements := readLEU32(v.value[1 : 1+nelemSize])
		idStart := uint64(1 + nelemSize)
		offsetStart := idStart + uint64(numElements)*uint64(fieldIdSz)
		dataStart := offsetStart + (uint64(numElements)+1)*uint64(fieldOffsetSz)

		debug.Assert(dataStart <= uint64(len(v.value)), "invalid object value: dataStart out of range")
		return ObjectValue{
			value:       v.value,
			meta:        v.meta,
			numElements: numElements,
			offsetStart: offsetStart,
			dataStart:   dataStart,
			idSize:      fieldIdSz,
			offsetSize:  fieldOffsetSz,
			idStart:     idStart,
		}
	case BasicArray:
		valueHdr := (v.value[0] >> basicTypeBits)
		fieldOffsetSz := (valueHdr & 0b11) + 1
		isLarge := ((valueHdr >> 2) & 0b1) == 1

		var (
			sz          uint32
			offsetStart uint64
		)

		if isLarge {
			sz, offsetStart = readLEU32(v.value[1:5]), 5
		} else {
			sz, offsetStart = uint32(v.value[1]), 2
		}

		dataStart := offsetStart + (uint64(sz)+1)*uint64(fieldOffsetSz)
		debug.Assert(dataStart <= uint64(len(v.value)), "invalid array value: dataStart out of range")
		return ArrayValue{
			value:       v.value,
			meta:        v.meta,
			numElements: sz,
			dataStart:   dataStart,
			offsetSize:  fieldOffsetSz,
			offsetStart: offsetStart,
		}
	}

	debug.Assert(false, "unsupported type")
	return nil
}

// MarshalJSON implements the json.Marshaler interface for Value.
func (v Value) MarshalJSON() ([]byte, error) {
	result := v.Value()
	switch t := result.(type) {
	case arrow.Date32:
		result = t.FormattedString()
	case arrow.Timestamp:
		switch primType := primitiveTypeFromHeader(v.value[0]); primType {
		case PrimitiveTimestampMicros:
			result = t.ToTime(arrow.Microsecond).Format("2006-01-02 15:04:05.999999Z0700")
		case PrimitiveTimestampMicrosNTZ:
			result = t.ToTime(arrow.Microsecond).In(time.Local).Format("2006-01-02 15:04:05.999999Z0700")
		case PrimitiveTimestampNanos:
			result = t.ToTime(arrow.Nanosecond).Format("2006-01-02 15:04:05.999999999Z0700")
		case PrimitiveTimestampNanosNTZ:
			result = t.ToTime(arrow.Nanosecond).In(time.Local).Format("2006-01-02 15:04:05.999999999Z0700")
		}
	case arrow.Time64:
		result = t.ToTime(arrow.Microsecond).In(time.Local).Format("15:04:05.999999Z0700")
	}

	return json.Marshal(result)
}
