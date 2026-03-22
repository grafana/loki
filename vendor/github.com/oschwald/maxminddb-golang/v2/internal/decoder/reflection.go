package decoder

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sync"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// Unmarshaler is implemented by types that can unmarshal MaxMind DB data.
// This is used internally for reflection-based decoding.
type Unmarshaler interface {
	UnmarshalMaxMindDB(d *Decoder) error
}

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
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
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
	if result.Kind() != reflect.Ptr || result.IsNil() {
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
			return fmt.Errorf("unexpected type for %d value in path, %v: %T", i, v, v)
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
	if err == nil {
		return nil
	}

	// Build path context retroactively by checking if the error already has context
	var pathBuilder *mmdberrors.PathBuilder
	var contextErr mmdberrors.ContextualError
	if errors.As(err, &contextErr) {
		// Error already has context, extract existing path and extend it
		pathBuilder = mmdberrors.NewPathBuilder()
		if contextErr.Path != "" && contextErr.Path != "/" {
			// Parse existing path and rebuild
			pathBuilder.ParseAndExtend(contextErr.Path)
		}
		pathBuilder.PrependMap(key)
		// Return unwrapped error with extended path, preserving original offset
		return mmdberrors.WrapWithContext(contextErr.Err, contextErr.Offset, pathBuilder)
	}

	// New error, start building path - extract offset if it's already a contextual error
	pathBuilder = mmdberrors.NewPathBuilder()
	pathBuilder.PrependMap(key)

	// Try to get existing offset from any wrapped contextual error
	var existingOffset uint
	var existingErr mmdberrors.ContextualError
	if errors.As(err, &existingErr) {
		existingOffset = existingErr.Offset
	}

	return mmdberrors.WrapWithContext(err, existingOffset, pathBuilder)
}

// wrapErrorWithSliceIndex wraps an error with slice index context, building path retroactively.
// Zero allocation on happy path - only allocates when error != nil.
func (*ReflectionDecoder) wrapErrorWithSliceIndex(err error, index int) error {
	if err == nil {
		return nil
	}

	// Build path context retroactively by checking if the error already has context
	var pathBuilder *mmdberrors.PathBuilder
	var contextErr mmdberrors.ContextualError
	if errors.As(err, &contextErr) {
		// Error already has context, extract existing path and extend it
		pathBuilder = mmdberrors.NewPathBuilder()
		if contextErr.Path != "" && contextErr.Path != "/" {
			// Parse existing path and rebuild
			pathBuilder.ParseAndExtend(contextErr.Path)
		}
		pathBuilder.PrependSlice(index)
		// Return unwrapped error with extended path, preserving original offset
		return mmdberrors.WrapWithContext(contextErr.Err, contextErr.Offset, pathBuilder)
	}

	// New error, start building path - extract offset if it's already a contextual error
	pathBuilder = mmdberrors.NewPathBuilder()
	pathBuilder.PrependSlice(index)

	// Try to get existing offset from any wrapped contextual error
	var existingOffset uint
	var existingErr mmdberrors.ContextualError
	if errors.As(err, &existingErr) {
		existingOffset = existingErr.Offset
	}

	return mmdberrors.WrapWithContext(err, existingOffset, pathBuilder)
}

func (d *ReflectionDecoder) decode(offset uint, result reflect.Value, depth int) (uint, error) {
	// Convert to addressableValue and delegate to internal method
	// Use fast path for already addressable values to avoid allocation
	if result.CanAddr() {
		av := addressableValue{Value: result, forcedAddr: false}
		return d.decodeValue(offset, av, depth)
	}
	av := makeAddressable(result)
	return d.decodeValue(offset, av, depth)
}

// decodeValue is the internal decode method that works with addressableValue
// for consistent optimization throughout the decoder.
func (d *ReflectionDecoder) decodeValue(
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	if depth > maximumDataStructureDepth {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"exceeded maximum data structure depth; database is likely corrupt",
		)
	}

	// Apply the original indirect logic to handle pointers and interfaces properly
	for {
		// Load value from interface, but only if the result will be
		// usefully addressable.
		if result.Kind() == reflect.Interface && !result.IsNil() {
			e := result.Elem()
			if e.Kind() == reflect.Ptr && !e.IsNil() {
				result = addressableValue{e, result.forcedAddr}
				continue
			}
		}

		if result.Kind() != reflect.Ptr {
			break
		}

		if result.IsNil() {
			result.Set(reflect.New(result.Type().Elem()))
		}

		result = addressableValue{
			result.Elem(),
			false,
		} // dereferenced pointer is always addressable
	}

	// Check if the value implements Unmarshaler interface using type assertion
	if result.CanAddr() {
		if unmarshaler, ok := tryTypeAssert(result.Addr()); ok {
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
			rv := addressableValue{Value: mapVal, forcedAddr: false}
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
		if (controlByte >> 5) == 1 { // KindPointer = 1, stored in top 3 bits
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
			rv := addressableValue{Value: sliceVal, forcedAddr: false}
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
	keyValue := addressableValue{Value: keyVal, forcedAddr: false}
	elemType := mapType.Elem()
	var elemValue addressableValue
	// Pre-allocate element value to reduce allocations
	elemVal := reflect.New(elemType).Elem()
	elemValue = addressableValue{Value: elemVal, forcedAddr: false}
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
	result.Set(reflect.MakeSlice(result.Type(), int(size), int(size)))
	for i := range size {
		var err error
		// Use slice element directly to avoid allocation
		elemVal := result.Index(int(i))
		elemValue := addressableValue{Value: elemVal, forcedAddr: false}
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

		// Fast path for common simple field types
		if len(fieldInfo.index) == 0 && fieldInfo.isFastType {
			// Try fast decode path for pre-identified simple types
			if fastOffset, ok := d.tryFastDecodeTyped(offset, fieldValue, fieldInfo.fieldType); ok {
				offset = fastOffset
				continue
			}
		}

		offset, err = d.decodeValue(offset, fieldValue, depth)
		if err != nil {
			return 0, d.wrapErrorWithMapKey(err, string(key))
		}
	}
	return offset, nil
}

type fieldInfo struct {
	fieldType  reflect.Type
	name       string
	index      []int
	index0     int
	depth      int
	hasTag     bool
	isFastType bool
}

type fieldsType struct {
	namedFields map[string]*fieldInfo // Map from field name to field info
}

type queueEntry struct {
	typ   reflect.Type
	index []int // Field index path
	depth int   // Embedding depth
}

// getEmbeddedStructType returns the struct type for embedded fields.
// Returns nil if the field is not an embeddable struct type.
func getEmbeddedStructType(fieldType reflect.Type) reflect.Type {
	if fieldType.Kind() == reflect.Struct {
		return fieldType
	}
	if fieldType.Kind() == reflect.Ptr && fieldType.Elem().Kind() == reflect.Struct {
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

// validateTag performs basic validation of maxminddb struct tags.
func validateTag(field reflect.StructField, tag string) error {
	if tag == "" || tag == "-" {
		return nil
	}

	// Check for invalid UTF-8
	if !utf8.ValidString(tag) {
		return fmt.Errorf("field %s has tag with invalid UTF-8: %q", field.Name, tag)
	}

	// Only flag very obvious mistakes - don't be too restrictive
	return nil
}

var fieldsMap sync.Map

func cachedFields(result reflect.Value) *fieldsType {
	resultType := result.Type()

	if fields, ok := fieldsMap.Load(resultType); ok {
		return fields.(*fieldsType)
	}

	fields := makeStructFields(resultType)
	fieldsMap.Store(resultType, fields)

	return fields
}

// makeStructFields implements json/v2 style field precedence rules.
func makeStructFields(rootType reflect.Type) *fieldsType {
	// Breadth-first traversal to collect all fields with depth information

	queue := []queueEntry{{rootType, nil, 0}}
	var allFields []fieldInfo
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
			if tag := field.Tag.Get("maxminddb"); tag != "" {
				// Validate tag syntax
				if err := validateTag(field, tag); err != nil {
					// Log warning but continue processing
					// In a real implementation, you might want to use a proper logger
					_ = err // For now, just ignore validation errors
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

			// Add field to collection with optimization hints
			fieldType := field.Type
			isFast := isFastDecodeType(fieldType)
			allFields = append(allFields, fieldInfo{
				index:      fieldIndex, // Will be reindexed later for optimization
				name:       fieldName,
				hasTag:     hasTag,
				depth:      entry.depth,
				fieldType:  fieldType,
				isFastType: isFast,
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
		namedFields: namedFields,
	}

	// Reindex all fields for optimized access
	fields.reindex()

	return fields
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

// newAddressableValue creates an addressable value wrapper.
// If the value is not addressable, it wraps it to make it addressable.
func newAddressableValue(v reflect.Value) addressableValue {
	if v.CanAddr() {
		return addressableValue{Value: v, forcedAddr: false}
	}
	// Make non-addressable values addressable by boxing them
	addressable := reflect.New(v.Type()).Elem()
	addressable.Set(v)
	return addressableValue{Value: addressable, forcedAddr: true}
}

// makeAddressable efficiently converts a reflect.Value to addressableValue
// with minimal allocations when possible.
func makeAddressable(v reflect.Value) addressableValue {
	// Fast path for already addressable values
	if v.CanAddr() {
		return addressableValue{Value: v, forcedAddr: false}
	}
	return newAddressableValue(v)
}

// isFastDecodeType determines if a field type can use optimized decode paths.
func isFastDecodeType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String,
		reflect.Bool,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float64:
		return true
	case reflect.Ptr:
		// Pointer to fast types are also fast
		return isFastDecodeType(t.Elem())
	default:
		return false
	}
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
	if av.Kind() == reflect.Ptr {
		if av.IsNil() {
			if !mayAlloc || !av.CanSet() {
				return addressableValue{} // Return invalid value
			}
			av.Set(reflect.New(av.Type().Elem()))
		}
		av = addressableValue{av.Elem(), false}
	}
	return av
}

// tryFastDecodeTyped attempts to decode using pre-computed type information.
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
	case reflect.Ptr:
		// Handle pointer to fast types
		if result.IsNil() {
			result.Set(reflect.New(expectedType.Elem()))
		}
		return d.tryFastDecodeTyped(
			offset,
			addressableValue{result.Elem(), false},
			expectedType.Elem(),
		)
	default:
		// Type not supported for fast path
	}

	return 0, false
}
