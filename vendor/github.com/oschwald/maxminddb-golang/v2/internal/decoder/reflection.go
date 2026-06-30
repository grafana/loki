package decoder

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// Unmarshaler is implemented by types that can unmarshal MaxMind DB data.
// This is used internally for reflection-based decoding.
type Unmarshaler interface {
	UnmarshalMaxMindDB(d *Decoder) error
}

var unmarshalerType = reflect.TypeFor[Unmarshaler]()

// ReflectionDecoder is a decoder for the MMDB data section.
type ReflectionDecoder struct {
	DataDecoder
}

// New creates a [ReflectionDecoder].
func New(buffer []byte) ReflectionDecoder {
	return ReflectionDecoder{
		DataDecoder: NewDataDecoder(buffer),
	}
}

// IsEmptyValueAt checks if the value at the given offset is an empty map or array.
// Returns true if the value is a map or array with size 0.
func (d *ReflectionDecoder) IsEmptyValueAt(offset uint) (bool, error) {
	dataOffset := offset
	for {
		kindNum, size, newOffset, err := d.decodeCtrlData(dataOffset)
		if err != nil {
			return false, err
		}

		if kindNum == KindPointer {
			dataOffset, _, err = d.decodePointer(size, newOffset)
			if err != nil {
				return false, err
			}
			continue
		}

		// Check if it's a map or array with size 0
		return (kindNum == KindMap || kindNum == KindSlice) && size == 0, nil
	}
}

// Decode decodes the data value at offset and stores it in the value
// pointed at by v.
func (d *ReflectionDecoder) Decode(offset uint, v any) error {
	// Check if the type implements Unmarshaler interface without reflection
	if unmarshaler, ok := v.(Unmarshaler); ok {
		decoder := NewDecoder(d.DataDecoder, offset)
		return unmarshaler.UnmarshalMaxMindDB(decoder)
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("result param must be a pointer")
	}

	_, err := d.decode(offset, rv, 0)
	if err == nil {
		return nil
	}

	// Check if error already has context (including path), if so just add offset if missing
	var contextErr mmdberrors.ContextualError
	if errors.As(err, &contextErr) {
		// If the outermost error already has offset and path info, return as-is
		if contextErr.Offset != 0 || contextErr.Path != "" {
			return err
		}
		// Otherwise, just add offset to root
		return mmdberrors.WrapWithContext(contextErr.Err, offset, nil)
	}

	// Plain error, add offset
	return mmdberrors.WrapWithContext(err, offset, nil)
}

// DecodePath decodes the data value at offset and stores the value associated
// with the path in the value pointed at by v.
func (d *ReflectionDecoder) DecodePath(
	offset uint,
	path []any,
	v any,
) error {
	result := reflect.ValueOf(v)
	if result.Kind() != reflect.Pointer || result.IsNil() {
		return errors.New("result param must be a pointer")
	}

PATH:
	for i, v := range path {
		var (
			typeNum Kind
			size    uint
			err     error
		)
		typeNum, size, offset, err = d.decodeCtrlData(offset)
		if err != nil {
			return err
		}

		if typeNum == KindPointer {
			pointer, _, err := d.decodePointer(size, offset)
			if err != nil {
				return err
			}

			typeNum, size, offset, err = d.decodeCtrlData(pointer)
			if err != nil {
				return err
			}

			// Check for pointer-to-pointer after we've already read the data
			if typeNum == KindPointer {
				return mmdberrors.NewInvalidDatabaseError(
					"invalid pointer to pointer at offset %d",
					pointer,
				)
			}
		}

		switch v := v.(type) {
		case string:
			// We are expecting a map
			if typeNum != KindMap {
				return fmt.Errorf("expected a map for %s but found %s", v, typeNum.String())
			}
			for range size {
				var key []byte
				key, offset, err = d.decodeKey(offset)
				if err != nil {
					return err
				}
				if string(key) == v {
					continue PATH
				}
				offset, err = d.nextValueOffset(offset, 1)
				if err != nil {
					return err
				}
			}
			// Not found. Maybe return a boolean?
			return nil
		case int:
			// We are expecting an array
			if typeNum != KindSlice {
				return fmt.Errorf("expected a slice for %d but found %s", v, typeNum.String())
			}
			var i uint
			if v < 0 {
				if size < uint(-v) {
					// Slice is smaller than negative index, not found
					return nil
				}
				i = size - uint(-v)
			} else {
				if size <= uint(v) {
					// Slice is smaller than index, not found
					return nil
				}
				i = uint(v)
			}
			offset, err = d.nextValueOffset(offset, i)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected path element at index %d (%v): %T", i, v, v)
		}
	}
	_, err := d.decode(offset, result, len(path))
	return d.wrapError(err, offset)
}

// wrapError wraps an error with context information when an error occurs.
// Zero allocation on happy path - only allocates when error != nil.
func (*ReflectionDecoder) wrapError(err error, offset uint) error {
	if err == nil {
		return nil
	}
	// Only wrap with context when an error actually occurs
	return mmdberrors.WrapWithContext(err, offset, nil)
}

// wrapErrorWithMapKey wraps an error with map key context, building path retroactively.
// Zero allocation on happy path - only allocates when error != nil.
func (*ReflectionDecoder) wrapErrorWithMapKey(err error, key string) error {
	return wrapErrorWithPath(err, func(pathBuilder *mmdberrors.PathBuilder) {
		pathBuilder.PrependMap(key)
	})
}

// wrapErrorWithSliceIndex wraps an error with slice index context, building path retroactively.
// Zero allocation on happy path - only allocates when error != nil.
func (*ReflectionDecoder) wrapErrorWithSliceIndex(err error, index int) error {
	return wrapErrorWithPath(err, func(pathBuilder *mmdberrors.PathBuilder) {
		pathBuilder.PrependSlice(index)
	})
}

func wrapErrorWithPath(err error, prepend func(*mmdberrors.PathBuilder)) error {
	if err == nil {
		return nil
	}

	var contextErr mmdberrors.ContextualError
	if errors.As(err, &contextErr) {
		pathBuilder := mmdberrors.NewPathBuilder()
		if contextErr.Path != "" && contextErr.Path != "/" {
			pathBuilder.ParseAndExtend(contextErr.Path)
		}
		prepend(pathBuilder)
		return mmdberrors.WrapWithContext(contextErr.Err, contextErr.Offset, pathBuilder)
	}

	pathBuilder := mmdberrors.NewPathBuilder()
	prepend(pathBuilder)
	return mmdberrors.WrapWithContext(err, 0, pathBuilder)
}

func (d *ReflectionDecoder) decode(offset uint, result reflect.Value, depth int) (uint, error) {
	// Skip makeAddressable's boxing copy whenever result is already addressable.
	// The common Decode(&v) entry passes a non-addressable pointer, but its
	// Elem() is addressable, so we can decode through it directly. Callers that
	// already supplied an addressable Value (e.g., a struct field) take the
	// CanAddr branch. Only non-addressable, non-pointer values need
	// makeAddressable, which allocates.
	if result.Kind() == reflect.Pointer && !result.IsNil() {
		return d.decodeValue(offset, addressableValue{Value: result.Elem()}, depth)
	}
	if result.CanAddr() {
		return d.decodeValue(offset, addressableValue{Value: result}, depth)
	}
	return d.decodeValue(offset, makeAddressable(result), depth)
}

func (d *ReflectionDecoder) decodeValue(
	offset uint,
	result addressableValue,
	depth int,
) (newOffset uint, retErr error) {
	return d.decodeValueImpl(offset, result, depth, true)
}

// decodeValueSkipUnmarshaler is the struct-field fast path: when the
// destination type was precomputed to not implement Unmarshaler, we skip
// the reflective type-assert that decodeValue would otherwise perform.
func (d *ReflectionDecoder) decodeValueSkipUnmarshaler(
	offset uint,
	result addressableValue,
	depth int,
) (newOffset uint, retErr error) {
	return d.decodeValueImpl(offset, result, depth, false)
}

func (d *ReflectionDecoder) decodeValueImpl(
	offset uint,
	result addressableValue,
	depth int,
	checkUnmarshaler bool,
) (newOffset uint, retErr error) {
	if depth > maximumDataStructureDepth {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"exceeded maximum data structure depth; database is likely corrupt",
		)
	}

	var allocated1, allocated2 reflect.Value
	var allocatedMore []reflect.Value
	allocatedCount := 0

	defer func() {
		if retErr == nil {
			return
		}
		switch allocatedCount {
		case 0:
			// no-op
		case 1:
			allocated1.SetZero()
		case 2:
			allocated2.SetZero()
			allocated1.SetZero()
		default:
			for _, pointer := range slices.Backward(allocatedMore) {
				pointer.SetZero()
			}
			allocated2.SetZero()
			allocated1.SetZero()
		}
	}()

	// Apply the original indirect logic to handle pointers and interfaces properly
	for {
		// Load value from interface, but only if the result will be
		// usefully addressable.
		if result.Kind() == reflect.Interface && !result.IsNil() {
			e := result.Elem()
			if e.Kind() == reflect.Pointer && !e.IsNil() {
				result = addressableValue{e, result.forcedAddr}
				continue
			}
		}

		if result.Kind() != reflect.Pointer {
			break
		}

		if result.IsNil() {
			result.Set(reflect.New(result.Type().Elem()))
			switch allocatedCount {
			case 0:
				allocated1 = result.Value
			case 1:
				allocated2 = result.Value
			default:
				allocatedMore = append(allocatedMore, result.Value)
			}
			allocatedCount++
		}

		result = addressableValue{
			result.Elem(),
			false,
		} // dereferenced pointer is always addressable
	}

	// Try Unmarshaler dispatch only when the type might actually implement
	// the interface. Struct decoding passes checkUnmarshaler=false when the
	// per-field precomputation already established the destination cannot
	// match, avoiding the reflective type assertion entirely.
	if checkUnmarshaler && result.CanAddr() && mayImplementUnmarshaler(result.Type()) {
		if unmarshaler, ok := reflect.TypeAssert[Unmarshaler](result.Addr()); ok {
			decoder := NewDecoder(d.DataDecoder, offset)
			if err := unmarshaler.UnmarshalMaxMindDB(decoder); err != nil {
				return 0, err
			}
			return d.nextValueOffset(offset, 1)
		}
	}

	typeNum, size, newOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return 0, err
	}

	if typeNum != KindPointer && result.Kind() == reflect.Uintptr {
		result.Set(reflect.ValueOf(uintptr(offset)))
		return d.nextValueOffset(offset, 1)
	}
	return d.decodeFromType(typeNum, size, newOffset, result, depth+1)
}

func (d *ReflectionDecoder) decodeFromType(
	dtype Kind,
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	// For these types, size has a special meaning
	switch dtype {
	case KindBool:
		return d.unmarshalBool(size, offset, result)
	case KindMap:
		return d.unmarshalMap(size, offset, result, depth)
	case KindPointer:
		return d.unmarshalPointer(size, offset, result, depth)
	case KindSlice:
		return d.unmarshalSlice(size, offset, result, depth)
	case KindBytes:
		return d.unmarshalBytes(size, offset, result)
	case KindFloat32:
		return d.unmarshalFloat32(size, offset, result)
	case KindFloat64:
		return d.unmarshalFloat64(size, offset, result)
	case KindInt32:
		return d.unmarshalInt32(size, offset, result)
	case KindUint16:
		return d.unmarshalUint(size, offset, result, 16)
	case KindUint32:
		return d.unmarshalUint(size, offset, result, 32)
	case KindUint64:
		return d.unmarshalUint(size, offset, result, 64)
	case KindString:
		return d.unmarshalString(size, offset, result)
	case KindUint128:
		return d.unmarshalUint128(size, offset, result)
	default:
		return 0, mmdberrors.NewInvalidDatabaseError("unknown type: %d", dtype)
	}
}

func (d *ReflectionDecoder) unmarshalBool(
	size, offset uint,
	result addressableValue,
) (uint, error) {
	value, newOffset, err := d.decodeBool(size, offset)
	if err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.Bool:
		result.SetBool(value)
		return newOffset, nil
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

var sliceType = reflect.TypeFor[[]byte]()

func (d *ReflectionDecoder) unmarshalBytes(
	size, offset uint,
	result addressableValue,
) (uint, error) {
	value, newOffset, err := d.decodeBytes(size, offset)
	if err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.Slice:
		if result.Type() == sliceType {
			result.SetBytes(value)
			return newOffset, nil
		}
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

func (d *ReflectionDecoder) unmarshalFloat32(
	size, offset uint, result addressableValue,
) (uint, error) {
	value, newOffset, err := d.decodeFloat32(size, offset)
	if err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.Float32, reflect.Float64:
		result.SetFloat(float64(value))
		return newOffset, nil
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

func (d *ReflectionDecoder) unmarshalFloat64(
	size, offset uint, result addressableValue,
) (uint, error) {
	value, newOffset, err := d.decodeFloat64(size, offset)
	if err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.Float32, reflect.Float64:
		if result.OverflowFloat(value) {
			return 0, mmdberrors.NewUnmarshalTypeError(value, result.Type())
		}
		result.SetFloat(value)
		return newOffset, nil
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

func (d *ReflectionDecoder) unmarshalInt32(
	size, offset uint,
	result addressableValue,
) (uint, error) {
	value, newOffset, err := d.decodeInt32(size, offset)
	if err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := int64(value)
		if !result.OverflowInt(n) {
			result.SetInt(n)
			return newOffset, nil
		}
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr:
		if value < 0 {
			break
		}
		n := uint64(value)
		if !result.OverflowUint(n) {
			result.SetUint(n)
			return newOffset, nil
		}
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

func (d *ReflectionDecoder) unmarshalMap(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	switch result.Kind() {
	case reflect.Struct:
		return d.decodeStruct(size, offset, result, depth)
	case reflect.Map:
		return d.decodeMap(size, offset, result, depth)
	case reflect.Interface:
		if result.NumMethod() == 0 {
			// Create map directly without makeAddressable wrapper
			mapVal := reflect.ValueOf(make(map[string]any, size))
			rv := addressableValue{Value: mapVal}
			newOffset, err := d.decodeMap(size, offset, rv, depth)
			result.Set(rv.Value)
			return newOffset, err
		}
		return 0, mmdberrors.NewUnmarshalTypeStrError("map", result.Type())
	default:
		return 0, mmdberrors.NewUnmarshalTypeStrError("map", result.Type())
	}
}

func (d *ReflectionDecoder) unmarshalPointer(
	size, offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	pointer, newOffset, err := d.decodePointer(size, offset)
	if err != nil {
		return 0, err
	}

	// Check for pointer-to-pointer by looking at what we're about to decode
	// This is done efficiently by checking the control byte at the pointer location
	if len(d.buffer) > int(pointer) {
		controlByte := d.buffer[pointer]
		if Kind(controlByte>>5) == KindPointer {
			return 0, mmdberrors.NewInvalidDatabaseError(
				"invalid pointer to pointer at offset %d",
				pointer,
			)
		}
	}

	_, err = d.decodeValue(pointer, result, depth)
	return newOffset, err
}

func (d *ReflectionDecoder) unmarshalSlice(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	switch result.Kind() {
	case reflect.Slice:
		return d.decodeSlice(size, offset, result, depth)
	case reflect.Interface:
		if result.NumMethod() == 0 {
			a := []any{}
			// Create slice directly without makeAddressable wrapper
			sliceVal := reflect.ValueOf(&a).Elem()
			rv := addressableValue{Value: sliceVal}
			newOffset, err := d.decodeSlice(size, offset, rv, depth)
			result.Set(rv.Value)
			return newOffset, err
		}
	default:
		// Fall through to error return
	}
	return 0, mmdberrors.NewUnmarshalTypeStrError("array", result.Type())
}

func (d *ReflectionDecoder) unmarshalString(
	size, offset uint,
	result addressableValue,
) (uint, error) {
	value, newOffset, err := d.decodeString(size, offset)
	if err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.String:
		result.SetString(value)
		return newOffset, nil
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

func (d *ReflectionDecoder) unmarshalUint(
	size, offset uint,
	result addressableValue,
	uintType uint,
) (uint, error) {
	// Use the appropriate DataDecoder method based on uint type
	var value uint64
	var newOffset uint
	var err error

	switch uintType {
	case 16:
		v16, off, e := d.decodeUint16(size, offset)
		value, newOffset, err = uint64(v16), off, e
	case 32:
		v32, off, e := d.decodeUint32(size, offset)
		value, newOffset, err = uint64(v32), off, e
	case 64:
		value, newOffset, err = d.decodeUint64(size, offset)
	default:
		return 0, mmdberrors.NewInvalidDatabaseError(
			"unsupported uint type: %d", uintType)
	}

	if err != nil {
		return 0, err
	}

	// Fast path for exact type matches (inspired by json/v2 fast paths)
	switch result.Kind() {
	case reflect.Uint32:
		if uintType == 32 && value <= 0xFFFFFFFF {
			result.SetUint(value)
			return newOffset, nil
		}
	case reflect.Uint64:
		if uintType == 64 {
			result.SetUint(value)
			return newOffset, nil
		}
	case reflect.Uint16:
		if uintType == 16 && value <= 0xFFFF {
			result.SetUint(value)
			return newOffset, nil
		}
	case reflect.Uint8:
		if uintType == 16 && value <= 0xFF { // uint8 often stored as uint16 in MMDB
			result.SetUint(value)
			return newOffset, nil
		}
	default:
		// Fall through to general unmarshaling logic
	}

	switch result.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := int64(value)
		if !result.OverflowInt(n) {
			result.SetInt(n)
			return newOffset, nil
		}
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr:
		if !result.OverflowUint(value) {
			result.SetUint(value)
			return newOffset, nil
		}
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

var bigIntType = reflect.TypeFor[big.Int]()

func (d *ReflectionDecoder) unmarshalUint128(
	size, offset uint, result addressableValue,
) (uint, error) {
	hi, lo, newOffset, err := d.decodeUint128(size, offset)
	if err != nil {
		return 0, err
	}

	// Convert hi/lo representation to big.Int
	value := new(big.Int)
	if hi == 0 {
		value.SetUint64(lo)
	} else {
		value.SetUint64(hi)
		value.Lsh(value, 64)                        // Shift high part left by 64 bits
		value.Or(value, new(big.Int).SetUint64(lo)) // OR with low part
	}

	switch result.Kind() {
	case reflect.Struct:
		if result.Type() == bigIntType {
			result.Set(reflect.ValueOf(*value))
			return newOffset, nil
		}
	case reflect.Interface:
		if result.NumMethod() == 0 {
			result.Set(reflect.ValueOf(value))
			return newOffset, nil
		}
	default:
		// Fall through to error return
	}
	return newOffset, mmdberrors.NewUnmarshalTypeError(value, result.Type())
}

func (d *ReflectionDecoder) decodeMap(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	if result.IsNil() {
		result.Set(reflect.MakeMapWithSize(result.Type(), int(size)))
	}

	mapType := result.Type()

	// Pre-allocated values for efficient reuse
	keyVal := reflect.New(mapType.Key()).Elem()
	keyValue := addressableValue{Value: keyVal}
	elemType := mapType.Elem()
	var elemValue addressableValue
	// Pre-allocate element value to reduce allocations
	elemVal := reflect.New(elemType).Elem()
	elemValue = addressableValue{Value: elemVal}
	for range size {
		var err error

		// Reuse keyValue by zeroing it
		keyValue.SetZero()
		offset, err = d.decodeValue(offset, keyValue, depth)
		if err != nil {
			return 0, err
		}

		// Reuse elemValue by zeroing it
		elemValue.SetZero()

		offset, err = d.decodeValue(offset, elemValue, depth)
		if err != nil {
			return 0, d.wrapErrorWithMapKey(err, keyValue.String())
		}

		result.SetMapIndex(keyValue.Value, elemValue.Value)
	}
	return offset, nil
}

func (d *ReflectionDecoder) decodeSlice(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	sliceLen := int(size)
	if result.IsNil() || result.Cap() < sliceLen {
		result.Set(reflect.MakeSlice(result.Type(), sliceLen, sliceLen))
	} else {
		// Reuse the caller's backing array. Two clears are needed: the first
		// zeroes [0:sliceLen] so element fields not present in the new data
		// (e.g., omitted struct keys) don't carry forward; the second zeroes
		// (sliceLen:oldLen] so the now-hidden tail drops any pointer-like
		// references it held, letting the GC reclaim them.
		oldLen := result.Len()
		result.SetLen(sliceLen)
		result.Clear()
		if oldLen > sliceLen {
			result.Slice(sliceLen, oldLen).Clear()
		}
	}

	for i := range size {
		var err error
		elemValue := addressableValue{Value: result.Index(int(i))}
		offset, err = d.decodeValue(offset, elemValue, depth)
		if err != nil {
			return 0, d.wrapErrorWithSliceIndex(err, int(i))
		}
	}
	return offset, nil
}

func (d *ReflectionDecoder) decodeStruct(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	fields := cachedFields(result.Value)
	return d.decodeStructWithFields(size, offset, result, depth, fields)
}

func (d *ReflectionDecoder) decodeStructWithFields(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
	fields *fieldsType,
) (uint, error) {
	if fields.validationErr != nil {
		return 0, fields.validationErr
	}

	// Single-phase processing: decode only the dominant fields
	for range size {
		var (
			err error
			key []byte
		)
		key, offset, err = d.decodeKey(offset)
		if err != nil {
			return 0, err
		}
		// The string() does not create a copy due to this compiler
		// optimization: https://github.com/golang/go/issues/3512
		fieldInfo, ok := fields.namedFields[string(key)]
		if !ok {
			offset, err = d.nextValueOffset(offset, 1)
			if err != nil {
				return 0, err
			}
			continue
		}

		// Use optimized field access with addressable value wrapper
		fieldValue := result.fieldByIndex(fieldInfo.index0, fieldInfo.index, true)
		if !fieldValue.IsValid() {
			// Field access failed, skip this field
			offset, err = d.nextValueOffset(offset, 1)
			if err != nil {
				return 0, err
			}
			continue
		}

		// Dispatch on the precomputed strategy. The fast-path case has a
		// runtime guard for embedded fields (len(index) > 0): the
		// fast-path decoders bypass embedded-pointer initialization,
		// which is the caller's responsibility on the slow path.
		switch fieldInfo.dispatch {
		case dispatchFast:
			if len(fieldInfo.index) == 0 {
				if fastOffset, ok := d.tryFastDecodeTyped(
					offset,
					fieldValue,
					fieldInfo.fieldType,
				); ok {
					offset = fastOffset
					continue
				}
			}
			offset, err = d.decodeValueSkipUnmarshaler(offset, fieldValue, depth)
		case dispatchUnmarshaler:
			offset, err = d.decodeValue(offset, fieldValue, depth)
		case dispatchStruct:
			var ok bool
			offset, ok, err = d.tryDecodeStructWithFields(
				offset,
				fieldValue,
				depth,
				fieldInfo.structFields,
			)
			if !ok {
				offset, err = d.decodeValueSkipUnmarshaler(offset, fieldValue, depth)
			}
		case dispatchPointerStruct:
			var ok bool
			offset, ok, err = d.tryDecodePointerStructWithFields(
				offset,
				fieldValue,
				depth,
				fieldInfo.structFields,
			)
			if !ok {
				offset, err = d.decodeValueSkipUnmarshaler(offset, fieldValue, depth)
			}
		default: // dispatchPlain
			offset, err = d.decodeValueSkipUnmarshaler(offset, fieldValue, depth)
		}
		if err != nil {
			return 0, d.wrapErrorWithMapKey(err, string(key))
		}
	}
	return offset, nil
}

func (d *ReflectionDecoder) tryDecodeStructWithFields(
	offset uint,
	result addressableValue,
	depth int,
	fields *fieldsType,
) (newOffset uint, ok bool, err error) {
	typeNum, size, dataOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return 0, true, err
	}

	switch typeNum {
	case KindMap:
		if err := checkNestedDepth(depth); err != nil {
			return 0, true, err
		}
		newOffset, err = d.decodeStructWithFields(size, dataOffset, result, depth+1, fields)
		return newOffset, true, err
	case KindPointer:
		pointer, pointerEndOffset, err := d.decodePointer(size, dataOffset)
		if err != nil {
			return 0, true, err
		}
		if err := checkNestedDepth(depth); err != nil {
			return 0, true, err
		}
		typeNum, size, dataOffset, err = d.decodeCtrlData(pointer)
		if err != nil {
			return 0, true, err
		}
		if typeNum == KindPointer {
			return 0, true, mmdberrors.NewInvalidDatabaseError(
				"invalid pointer to pointer at offset %d",
				pointer,
			)
		}
		if typeNum != KindMap {
			return 0, false, nil
		}
		_, err = d.decodeStructWithFields(size, dataOffset, result, depth+2, fields)
		return pointerEndOffset, true, err
	default:
		return 0, false, nil
	}
}

func (d *ReflectionDecoder) tryDecodePointerStructWithFields(
	offset uint,
	result addressableValue,
	depth int,
	fields *fieldsType,
) (newOffset uint, ok bool, err error) {
	typeNum, size, dataOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return 0, true, err
	}

	var pointerEndOffset uint
	decodeDepth := depth + 1
	switch typeNum {
	case KindMap:
		if err := checkNestedDepth(depth); err != nil {
			return 0, true, err
		}
		// Use the map control record we already decoded.
	case KindPointer:
		var pointer uint
		pointer, pointerEndOffset, err = d.decodePointer(size, dataOffset)
		if err != nil {
			return 0, true, err
		}
		if err := checkNestedDepth(depth); err != nil {
			return 0, true, err
		}
		typeNum, size, dataOffset, err = d.decodeCtrlData(pointer)
		if err != nil {
			return 0, true, err
		}
		if typeNum == KindPointer {
			return 0, true, mmdberrors.NewInvalidDatabaseError(
				"invalid pointer to pointer at offset %d",
				pointer,
			)
		}
		if typeNum != KindMap {
			return 0, false, nil
		}
		decodeDepth = depth + 2
	default:
		return 0, false, nil
	}

	return d.decodePointerStructWithFields(
		size,
		dataOffset,
		pointerEndOffset,
		result,
		decodeDepth,
		fields,
	)
}

func checkNestedDepth(depth int) error {
	if depth < maximumDataStructureDepth {
		return nil
	}
	return mmdberrors.NewInvalidDatabaseError(
		"exceeded maximum data structure depth; database is likely corrupt",
	)
}

func (d *ReflectionDecoder) decodePointerStructWithFields(
	size uint,
	dataOffset uint,
	pointerEndOffset uint,
	result addressableValue,
	decodeDepth int,
	fields *fieldsType,
) (newOffset uint, ok bool, err error) {
	var allocated1, allocated2 reflect.Value
	var allocatedMore []reflect.Value
	allocatedCount := 0
	for result.Kind() == reflect.Pointer {
		if result.IsNil() {
			result.Set(reflect.New(result.Type().Elem()))
			switch allocatedCount {
			case 0:
				allocated1 = result.Value
			case 1:
				allocated2 = result.Value
			default:
				allocatedMore = append(allocatedMore, result.Value)
			}
			allocatedCount++
		}
		result = addressableValue{Value: result.Elem()}
	}

	if result.Kind() != reflect.Struct {
		cleanupAllocatedPointers(allocatedCount, allocated1, allocated2, allocatedMore)
		return 0, false, nil
	}

	newOffset, err = d.decodeStructWithFields(size, dataOffset, result, decodeDepth, fields)
	if err != nil {
		cleanupAllocatedPointers(allocatedCount, allocated1, allocated2, allocatedMore)
		return 0, true, err
	}
	if pointerEndOffset != 0 {
		return pointerEndOffset, true, nil
	}
	return newOffset, true, nil
}

func cleanupAllocatedPointers(
	allocatedCount int,
	allocated1, allocated2 reflect.Value,
	allocatedMore []reflect.Value,
) {
	switch allocatedCount {
	case 0:
		// no-op
	case 1:
		allocated1.SetZero()
	case 2:
		allocated2.SetZero()
		allocated1.SetZero()
	default:
		for _, pointer := range slices.Backward(allocatedMore) {
			pointer.SetZero()
		}
		allocated2.SetZero()
		allocated1.SetZero()
	}
}

// fieldDispatch encodes the decode strategy for a struct field, computed
// once at struct-cache build time. Encoding the three-way choice as a
// single enum (rather than non-orthogonal booleans) makes illegal
// combinations like "fast path AND Unmarshaler" unrepresentable.
type fieldDispatch uint8

const (
	// dispatchFast: field type is one of the primitive Go kinds the
	// fast path supports (string/bool/uint*/float64 or pointer to such)
	// and its unwrapped type does not implement Unmarshaler. The fast
	// path is attempted; on type-num mismatch it falls back to
	// decodeValueSkipUnmarshaler (which is sound: the field type cannot
	// implement Unmarshaler by construction).
	dispatchFast fieldDispatch = iota
	// dispatchUnmarshaler: field's unwrapped type is an interface or
	// implements Unmarshaler via its pointer receiver. Goes through
	// decodeValue, which performs the type assertion.
	dispatchUnmarshaler
	// dispatchStruct: field is a nested struct whose field set is
	// precomputed and can be decoded without consulting the field cache.
	dispatchStruct
	// dispatchPointerStruct: field is a pointer to a nested struct whose
	// field set is precomputed and whose pointer chain may need allocation.
	dispatchPointerStruct
	// dispatchPlain: everything else (structs, slices, maps, named
	// types without Unmarshaler). Uses decodeValueSkipUnmarshaler.
	dispatchPlain
)

type fieldInfo struct {
	fieldType    reflect.Type
	structFields *fieldsType
	name         string
	index        []int
	index0       int
	depth        int
	hasTag       bool
	dispatch     fieldDispatch
}

type fieldsType struct {
	namedFields   map[string]*fieldInfo // Map from field name to field info
	validationErr error
}

type queueEntry struct {
	typ   reflect.Type
	index []int // Field index path
	depth int   // Embedding depth
}

// validateTag performs basic validation of maxminddb struct tags.
func validateTag(field reflect.StructField, tag string) error {
	if tag == "" || tag == "-" {
		return nil
	}

	if !utf8.ValidString(tag) {
		return invalidMaxMindDBTagError(field.Name)
	}

	return nil
}

func invalidMaxMindDBTagError(fieldName string) error {
	return fmt.Errorf(
		"invalid maxminddb struct tag on field %q: must be valid UTF-8",
		fieldName,
	)
}

// getEmbeddedStructType returns the struct type for embedded fields.
// Returns nil if the field is not an embeddable struct type.
func getEmbeddedStructType(fieldType reflect.Type) reflect.Type {
	if fieldType.Kind() == reflect.Struct {
		return fieldType
	}
	if fieldType.Kind() == reflect.Pointer && fieldType.Elem().Kind() == reflect.Struct {
		return fieldType.Elem()
	}
	return nil
}

// handleEmbeddedField processes an embedded struct field and returns true if the field should be skipped.
func handleEmbeddedField(
	field reflect.StructField,
	hasTag bool,
	queue *[]queueEntry,
	seen *map[reflect.Type]bool,
	fieldIndex []int,
	depth int,
) bool {
	embeddedType := getEmbeddedStructType(field.Type)
	if embeddedType == nil {
		return false
	}

	// For embedded structs (and pointer to structs), add to queue for further traversal
	if !(*seen)[embeddedType] {
		*queue = append(*queue, queueEntry{embeddedType, fieldIndex, depth + 1})
		(*seen)[embeddedType] = true
	}

	// If embedded struct has no explicit tag, don't add it as a named field
	return !hasTag
}

var (
	fieldsMap        sync.Map
	unmarshalerCache sync.Map
)

func mayImplementUnmarshaler(t reflect.Type) bool {
	if t.PkgPath() == "" || t.Kind() == reflect.Interface {
		return false
	}

	if cached, ok := unmarshalerCache.Load(t); ok {
		return cached.(bool)
	}

	implements := reflect.PointerTo(t).Implements(unmarshalerType)
	unmarshalerCache.Store(t, implements)
	return implements
}

func cachedFields(result reflect.Value) *fieldsType {
	return cachedFieldsForType(result.Type())
}

func cachedFieldsForType(resultType reflect.Type) *fieldsType {
	return cachedFieldsForTypeWithStack(resultType, nil)
}

func cachedFieldsForTypeWithStack(
	resultType reflect.Type,
	stack map[reflect.Type]bool,
) *fieldsType {
	if fields, ok := fieldsMap.Load(resultType); ok {
		return fields.(*fieldsType)
	}

	if stack != nil && stack[resultType] {
		return nil
	}

	nextStack := make(map[reflect.Type]bool, len(stack)+1)
	for typ := range stack {
		nextStack[typ] = true
	}
	nextStack[resultType] = true

	fields := makeStructFieldsWithStack(resultType, nextStack)
	actual, _ := fieldsMap.LoadOrStore(resultType, fields)

	return actual.(*fieldsType)
}

// makeStructFields implements json/v2 style field precedence rules.
func makeStructFields(rootType reflect.Type) *fieldsType {
	return makeStructFieldsWithStack(rootType, map[reflect.Type]bool{rootType: true})
}

func makeStructFieldsWithStack(
	rootType reflect.Type,
	stack map[reflect.Type]bool,
) *fieldsType {
	// Breadth-first traversal to collect all fields with depth information

	queue := []queueEntry{{rootType, nil, 0}}
	var allFields []fieldInfo
	var validationErr error
	seen := make(map[reflect.Type]bool)
	seen[rootType] = true

	// Collect all reachable fields using breadth-first search
	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]

		for i := range entry.typ.NumField() {
			field := entry.typ.Field(i)

			// Skip unexported fields (except embedded structs)
			if !field.IsExported() && (!field.Anonymous || field.Type.Kind() != reflect.Struct) {
				continue
			}

			// Build field index path
			fieldIndex := make([]int, len(entry.index)+1)
			copy(fieldIndex, entry.index)
			fieldIndex[len(entry.index)] = i

			// Parse maxminddb tag
			fieldName := field.Name
			hasTag := false
			if validationErr == nil {
				validationErr = validateRawMaxMindDBTagValue(field, string(field.Tag))
			}
			if tag := field.Tag.Get("maxminddb"); tag != "" {
				if validationErr == nil {
					validationErr = validateTag(field, tag)
				}

				if tag == "-" {
					continue // Skip ignored fields
				}
				fieldName = tag
				hasTag = true
			}

			// Handle embedded structs and embedded pointers to structs
			if field.Anonymous && handleEmbeddedField(
				field, hasTag, &queue, &seen, fieldIndex, entry.depth,
			) {
				continue
			}

			// Resolve dispatch strategy once per field. Unmarshaler
			// possibility takes precedence over fast-path eligibility so
			// a named primitive type whose pointer receiver implements
			// UnmarshalMaxMindDB always takes the slow path. The case
			// order matches that precedence; the switch is exhaustive
			// because dispatchPlain is the default fallback.
			fieldType := field.Type
			unwrappedFieldType := unwrapPtrType(fieldType)
			var dispatch fieldDispatch
			var structFields *fieldsType
			switch {
			case mayImplementUnmarshaler(unwrappedFieldType) ||
				unwrappedFieldType.Kind() == reflect.Interface:
				dispatch = dispatchUnmarshaler
			case isFastDecodeType(fieldType):
				dispatch = dispatchFast
			default:
				dispatch = dispatchPlain
				if unwrappedFieldType.Kind() == reflect.Struct &&
					unwrappedFieldType != bigIntType &&
					!stack[unwrappedFieldType] {
					structFields = cachedFieldsForTypeWithStack(unwrappedFieldType, stack)
					if fieldType.Kind() == reflect.Pointer {
						dispatch = dispatchPointerStruct
					} else {
						dispatch = dispatchStruct
					}
				}
			}
			allFields = append(allFields, fieldInfo{
				index:        fieldIndex, // Will be reindexed later for optimization
				name:         fieldName,
				hasTag:       hasTag,
				depth:        entry.depth,
				fieldType:    fieldType,
				structFields: structFields,
				dispatch:     dispatch,
			})
		}
	}

	// Apply precedence rules to resolve field conflicts
	// Pre-size the map based on field count for better memory efficiency
	namedFields := make(map[string]*fieldInfo, len(allFields))
	fieldsByName := make(map[string][]fieldInfo, len(allFields))

	// Group fields by name
	for _, field := range allFields {
		fieldsByName[field.name] = append(fieldsByName[field.name], field)
	}

	// Apply precedence rules for each field name
	// Store results in a flattened slice to allow pointer references
	flatFields := make([]fieldInfo, 0, len(fieldsByName))

	for name, fields := range fieldsByName {
		if len(fields) == 1 {
			// No conflict, use the field
			flatFields = append(flatFields, fields[0])
			namedFields[name] = &flatFields[len(flatFields)-1]
			continue
		}

		// Find the dominant field using json/v2 precedence rules:
		// 1. Shallowest depth wins
		// 2. Among same depth, explicitly tagged field wins
		// 3. Among same depth with same tag status, first declared wins

		dominant := fields[0]
		for i := 1; i < len(fields); i++ {
			candidate := fields[i]

			// Shallowest depth wins
			if candidate.depth < dominant.depth {
				dominant = candidate
				continue
			}
			if candidate.depth > dominant.depth {
				continue
			}

			// Same depth: explicitly tagged field wins
			if candidate.hasTag && !dominant.hasTag {
				dominant = candidate
				continue
			}
			if !candidate.hasTag && dominant.hasTag {
				continue
			}

			// Same depth and tag status: first declared wins (keep current dominant)
		}

		flatFields = append(flatFields, dominant)
		namedFields[name] = &flatFields[len(flatFields)-1]
	}

	fields := &fieldsType{
		namedFields:   namedFields,
		validationErr: validationErr,
	}

	// Reindex all fields for optimized access
	fields.reindex()

	return fields
}

func validateRawMaxMindDBTagValue(field reflect.StructField, rawTag string) error {
	const key = `maxminddb:"`

	start := strings.Index(rawTag, key)
	if start == -1 {
		return nil
	}

	start += len(key)
	end := strings.IndexByte(rawTag[start:], '"')
	if end == -1 {
		return nil
	}

	if !utf8.ValidString(rawTag[start : start+end]) {
		return invalidMaxMindDBTagError(field.Name)
	}

	return nil
}

// reindex optimizes field indices to avoid bounds checks during runtime.
// This follows the json/v2 pattern of splitting the first index from the remainder.
func (fs *fieldsType) reindex() {
	for _, field := range fs.namedFields {
		if len(field.index) > 0 {
			field.index0 = field.index[0]
			field.index = field.index[1:]
			if len(field.index) == 0 {
				field.index = nil // avoid pinning the backing slice
			}
		}
	}
}

// addressableValue wraps a reflect.Value to optimize field access and
// embedded pointer handling. Based on encoding/json/v2 patterns.
type addressableValue struct {
	reflect.Value

	forcedAddr bool
}

// makeAddressable converts a reflect.Value to addressableValue, short-circuiting
// the reflect.New allocation when the value is already addressable. Non-addressable
// values are boxed via reflect.New so a pointer can be taken for downstream code
// that requires it.
func makeAddressable(v reflect.Value) addressableValue {
	if v.CanAddr() {
		return addressableValue{Value: v}
	}
	addressable := reflect.New(v.Type()).Elem()
	addressable.Set(v)
	return addressableValue{Value: addressable, forcedAddr: true}
}

// isFastDecodeType determines if a field type can use optimized decode paths.
func isFastDecodeType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String,
		reflect.Bool,
		reflect.Uint,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float64:
		return true
	case reflect.Pointer:
		return isFastDecodeType(t.Elem())
	default:
		return false
	}
}

// unwrapPtrType strips all pointer indirection from t and returns the
// underlying element type. Used to find the addressable receiver type that
// mayImplementUnmarshaler should check, since decoding allocates and
// dereferences as many *T layers as the field declares.
func unwrapPtrType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// fieldByIndex efficiently accesses a field by its index path,
// initializing embedded pointers as needed.
func (av addressableValue) fieldByIndex(
	index0 int,
	remainingIndex []int,
	mayAlloc bool,
) addressableValue {
	// First field access (optimized with no bounds check)
	av = addressableValue{av.Field(index0), av.forcedAddr}

	// Handle remaining indices if any
	if len(remainingIndex) > 0 {
		for _, i := range remainingIndex {
			av = av.indirect(mayAlloc)
			if !av.IsValid() {
				return av
			}
			av = addressableValue{av.Field(i), av.forcedAddr}
		}
	}

	return av
}

// indirect handles pointer dereferencing and initialization.
func (av addressableValue) indirect(mayAlloc bool) addressableValue {
	if av.Kind() == reflect.Pointer {
		if av.IsNil() {
			if !mayAlloc || !av.CanSet() {
				return addressableValue{} // Return invalid value
			}
			av.Set(reflect.New(av.Type().Elem()))
		}
		av = addressableValue{Value: av.Elem()}
	}
	return av
}

// tryFastDecodeTyped returns (newOffset, true) on success and (0, false)
// on any failure: a malformed buffer, a DB-type/Go-kind mismatch, or an
// inner decode error. Error surfacing relies on the caller (decodeStruct
// in the dispatchFast case) re-decoding from the same offset via the
// slow path, which re-encounters the underlying error and propagates it
// with proper context. The fast path itself never logs or wraps.
//
//nolint:gocyclo // fairly readable and this is optimized code.
func (d *ReflectionDecoder) tryFastDecodeTyped(
	offset uint,
	result addressableValue,
	expectedType reflect.Type,
) (uint, bool) {
	typeNum, size, newOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return 0, false
	}

	// Use pre-computed type information for faster matching
	switch expectedType.Kind() {
	case reflect.String:
		if typeNum == KindString {
			value, finalOffset, err := d.decodeString(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetString(value)
			return finalOffset, true
		}
	case reflect.Uint:
		switch typeNum {
		case KindUint16:
			value, finalOffset, err := d.decodeUint16(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetUint(uint64(value))
			return finalOffset, true
		case KindUint32:
			value, finalOffset, err := d.decodeUint32(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetUint(uint64(value))
			return finalOffset, true
		case KindUint64:
			value, finalOffset, err := d.decodeUint64(size, newOffset)
			if err != nil || uint64(uint(value)) != value {
				return 0, false
			}
			result.SetUint(value)
			return finalOffset, true
		}
	case reflect.Uint32:
		if typeNum == KindUint32 {
			value, finalOffset, err := d.decodeUint32(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetUint(uint64(value))
			return finalOffset, true
		}
	case reflect.Uint16:
		if typeNum == KindUint16 {
			value, finalOffset, err := d.decodeUint16(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetUint(uint64(value))
			return finalOffset, true
		}
	case reflect.Uint64:
		if typeNum == KindUint64 {
			value, finalOffset, err := d.decodeUint64(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetUint(value)
			return finalOffset, true
		}
	case reflect.Bool:
		if typeNum == KindBool {
			value, finalOffset, err := d.decodeBool(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetBool(value)
			return finalOffset, true
		}
	case reflect.Float64:
		if typeNum == KindFloat64 {
			value, finalOffset, err := d.decodeFloat64(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetFloat(value)
			return finalOffset, true
		}
	case reflect.Pointer:
		// Handle pointer to fast types
		if result.IsNil() {
			elem := reflect.New(expectedType.Elem()).Elem()
			finalOffset, ok := d.tryFastDecodeTyped(
				offset,
				addressableValue{Value: elem},
				expectedType.Elem(),
			)
			if !ok {
				return 0, false
			}
			result.Set(elem.Addr())
			return finalOffset, true
		}
		return d.tryFastDecodeTyped(
			offset,
			addressableValue{Value: result.Elem()},
			expectedType.Elem(),
		)
	default:
		// Type not supported for fast path
	}

	return 0, false
}
