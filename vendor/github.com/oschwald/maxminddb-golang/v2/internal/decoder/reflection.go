package decoder

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2/internal/maxminddbtag"
	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// Unmarshaler is implemented by types that can unmarshal MaxMind DB data.
// This is used internally for reflection-based decoding.
type Unmarshaler interface {
	UnmarshalMaxMindDB(d *Decoder) error
}

// CursorUnmarshaler is implemented by decoders that can decode directly from
// an immutable cursor and return its proven successor without acquiring a
// stateful Decoder.
type CursorUnmarshaler interface {
	UnmarshalMaxMindDBCursor(cursor Cursor) (Cursor, error)
}

var (
	unmarshalerType       = reflect.TypeFor[Unmarshaler]()
	cursorUnmarshalerType = reflect.TypeFor[CursorUnmarshaler]()
	stringType            = reflect.TypeFor[string]()
)

// ReflectionDecoder is a decoder for the MMDB data section.
type ReflectionDecoder struct { //nolint:govet // Keep the embedded decoder before regular fields.
	DataDecoder

	callbackDecoder *DataDecoder

	// budgetRemaining uses 0 for inactive limits, 1 for exhausted child slots,
	// and N for N-1 child slots remaining. Limits activate at the first map or
	// slice, or at a dynamically typed entry point.
	budgetRemaining  uint32
	payloadRemaining uint32
}

// New creates a [ReflectionDecoder].
func New(buffer []byte) ReflectionDecoder {
	return ReflectionDecoder{
		DataDecoder: NewDataDecoder(buffer),
	}
}

// NewWithoutStringCache creates a ReflectionDecoder without a string cache.
// It is intended for one-shot decoding such as database metadata parsing.
func NewWithoutStringCache(buffer []byte) ReflectionDecoder {
	return ReflectionDecoder{
		DataDecoder: NewDataDecoderWithoutStringCache(buffer),
	}
}

// IsEmptyValueAt checks if the value at the given offset is an empty map or array.
// Returns true if the value is a map or array with size 0.
func (d *ReflectionDecoder) IsEmptyValueAt(offset uint) (bool, error) {
	dataOffset := offset
	followedPointers := 0
	for {
		kindNum, size, newOffset, err := d.decodeCtrlData(dataOffset)
		if err != nil {
			return false, err
		}

		if kindNum == KindPointer {
			if followedPointers > 0 {
				return false, mmdberrors.NewInvalidDatabaseError(
					"invalid pointer to pointer at offset %d",
					dataOffset,
				)
			}
			followedPointers++
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
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("result param must be a pointer")
	}

	switch unmarshaler := v.(type) {
	case CursorUnmarshaler:
		_, err := (Cursor{
			decoder: d.callbackDataDecoder(),
			offset:  offset,
		}).UnmarshalCursor(unmarshaler)
		return err
	case Unmarshaler:
		decoder := acquireDecoder(d.callbackDataDecoder(), offset)
		err := unmarshaler.UnmarshalMaxMindDB(decoder)
		releaseDecoder(decoder)
		return err
	case *any:
		result := addressableValue{Value: rv.Elem()}
		err := d.decodeAnyWithBudget(offset, result, 0)
		if err != nil {
			return wrapRootDecodeError(err, offset)
		}
		return nil
	case *string:
		result := addressableValue{Value: rv.Elem()}
		if d.tryFastDecodeUnbudgetedString(offset, result) {
			return nil
		}
	case *[]byte:
		result := addressableValue{Value: rv.Elem()}
		if d.tryFastDecodeUnbudgetedBytes(offset, result) {
			return nil
		}
	case *bool, *int32, *uint, *uint16, *uint32, *uint64, *float32, *float64:
		result := addressableValue{Value: rv.Elem()}
		if _, ok := d.tryFastDecodeTyped(offset, result, result.Type()); ok {
			return nil
		}
	default:
	}

	result := addressableValue{Value: rv.Elem()}
	_, err := d.decodeValue(offset, result, 0)
	if err != nil {
		return wrapRootDecodeError(err, offset)
	}
	return nil
}

// DecodeWithBudget supplies a fresh expansion budget for destinations that use
// reflection's bounded container or dynamic decoding paths. Exact scalar fast
// paths remain unbudgeted. Reader metadata uses this cold-path entry point
// because it is decoded without a string cache and may own copied payloads.
func (d *ReflectionDecoder) DecodeWithBudget(offset uint, v any) error {
	bounded := newBudgetedDecoder(d)
	return bounded.Decode(offset, v)
}

// DecodePath decodes the data value at offset and stores the value associated
// with the path in the value pointed at by v.
func (d *ReflectionDecoder) DecodePath(
	offset uint,
	path []any,
	v any,
) error {
	if len(path) != 0 && d.budgetRemaining == 0 {
		bounded := newBudgetedDecoder(d)
		return bounded.decodePath(offset, path, v)
	}
	return d.decodePath(offset, path, v)
}

// PrepareForConcurrentUse initializes immutable callback state after a decoder
// reaches stable storage and before it is published for concurrent use.
func (d *ReflectionDecoder) PrepareForConcurrentUse() {
	d.callbackDecoder = &d.DataDecoder
}

func newBudgetedDecoder(d *ReflectionDecoder) ReflectionDecoder {
	return ReflectionDecoder{
		DataDecoder:      d.DataDecoder,
		callbackDecoder:  d.callbackDecoder,
		budgetRemaining:  (decodeExpansionBudgetBytes >> decodeBudgetUnitShift) + 1,
		payloadRemaining: decodePayloadBudgetBytes,
	}
}

func (d *ReflectionDecoder) decodeAnyWithBudget(
	offset uint,
	result addressableValue,
	depth int,
) error {
	if d.budgetRemaining != 0 {
		return d.decodeAny(offset, result, depth)
	}
	bounded := newBudgetedDecoder(d)
	return bounded.decodeAny(offset, result, depth)
}

func (d *ReflectionDecoder) callbackDataDecoder() *DataDecoder {
	// A callback may retain a cursor after its per-call ReflectionDecoder
	// returns. Lazily allocate one stable descriptor for operation-local
	// decoders. Reader initializes this field before publishing the decoder.
	if d.callbackDecoder == nil {
		d.callbackDecoder = new(DataDecoder)
		*d.callbackDecoder = d.DataDecoder
	}
	return d.callbackDecoder
}

func (d *ReflectionDecoder) reserveActiveContainer(kind Kind, size uint) error {
	if kind == KindMap {
		if size >= uint((d.budgetRemaining+1)/2) {
			return errDecodedRecordTooLarge
		}
		d.budgetRemaining -= uint32(size * 2)
		return nil
	}
	if size >= uint(d.budgetRemaining) {
		return errDecodedRecordTooLarge
	}
	d.budgetRemaining -= uint32(size)
	return nil
}

func (d *ReflectionDecoder) reserveExactPayload(size uint) error {
	if d.budgetRemaining == 0 {
		return nil
	}
	if size > uint(d.payloadRemaining) {
		return errDecodedRecordTooLarge
	}
	d.payloadRemaining -= uint32(size)
	return nil
}

// nextValueOffsetBudgeted skips values without following pointer targets while
// reserving every inline container it must traverse. Parent containers already
// reserved the skipped values themselves; only containers discovered inside
// those values add work here. Skipped scalar payload is never materialized and
// therefore does not consume the payload allowance.
func (d *ReflectionDecoder) nextValueOffsetBudgeted(
	offset uint,
	numberToSkip uint,
) (uint, error) {
	if numberToSkip == 1 && offset < uint(len(d.buffer)) {
		ctrlByte := d.buffer[offset]
		kind := Kind(ctrlByte >> 5)
		size := uint(ctrlByte & 0x1f)
		switch kind {
		case KindPointer, KindString, KindBytes:
			return d.nextValueOffset(offset, 1)
		case KindFloat64:
			if size == 8 {
				return d.nextValueOffset(offset, 1)
			}
		case KindUint16:
			if size <= 2 {
				return d.nextValueOffset(offset, 1)
			}
		case KindUint32:
			if size <= 4 {
				return d.nextValueOffset(offset, 1)
			}
		default:
		}
	}
	if d.budgetRemaining == 0 {
		bounded := newBudgetedDecoder(d)
		return bounded.nextValueOffsetBudgetedSlow(offset, numberToSkip)
	}
	return d.nextValueOffsetBudgetedSlow(offset, numberToSkip)
}

func (d *ReflectionDecoder) structFieldValueIsInlineContainer(offset uint) bool {
	if offset < uint(len(d.buffer)) {
		ctrlByte := d.buffer[offset]
		if ctrlByte>>5 == byte(KindMap) ||
			(ctrlByte>>5 == byte(KindExtended) && offset+1 < uint(len(d.buffer)) &&
				(d.buffer[offset+1] == byte(KindMap-7) ||
					d.buffer[offset+1] == byte(KindSlice-7))) {
			return true
		}
	}
	return false
}

//go:noinline
//nolint:gocyclo // The kind switch keeps scalar validation and container charging in one pass.
func (d *ReflectionDecoder) nextValueOffsetBudgetedSlow(
	offset uint,
	numberToSkip uint,
) (uint, error) {
	bufferLen := uint(len(d.buffer))
	for numberToSkip > 0 {
		if offset >= bufferLen {
			return 0, mmdberrors.NewOffsetError()
		}
		// Read compact headers inline; extended kinds and sizes still use
		// the general parser. Pointer size bits describe the token width.
		ctrlByte := d.buffer[offset]
		kind, size, newOffset := Kind(ctrlByte>>5), uint(ctrlByte&0x1f), offset+1
		if kind == KindExtended || (kind != KindPointer && size >= 29) {
			var err error
			kind, size, newOffset, err = d.decodeCtrlData(offset)
			if err != nil {
				return 0, err
			}
		}

		switch kind {
		case KindPointer:
			pointerSize := ((size >> 3) & 0x3) + 1
			if !hasBufferRange(bufferLen, newOffset, pointerSize) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += pointerSize
		case KindMap:
			if err := d.reserveActiveContainer(KindMap, size); err != nil {
				return 0, err
			}
			if size > (^uint(0)-numberToSkip)/2 {
				return 0, mmdberrors.NewInvalidDatabaseError("container size overflow")
			}
			numberToSkip += 2 * size
		case KindSlice:
			if err := d.reserveActiveContainer(KindSlice, size); err != nil {
				return 0, err
			}
			if size > ^uint(0)-numberToSkip {
				return 0, mmdberrors.NewInvalidDatabaseError("container size overflow")
			}
			numberToSkip += size
		case KindBool:
			if size > 1 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid bool size: %d", size)
			}
		case KindFloat64:
			if size != 8 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid Float64 size: %d", size)
			}
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		case KindFloat32:
			if size != 4 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid Float32 size: %d", size)
			}
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		case KindInt32, KindUint32:
			if size > 4 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid %s size: %d", kind, size)
			}
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		case KindUint16:
			if size > 2 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid Uint16 size: %d", size)
			}
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		case KindUint64:
			if size > 8 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid Uint64 size: %d", size)
			}
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		case KindUint128:
			if size > 16 {
				return 0, mmdberrors.NewInvalidDatabaseError("invalid Uint128 size: %d", size)
			}
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		case KindString, KindBytes:
			if !hasBufferRange(bufferLen, newOffset, size) {
				return 0, mmdberrors.NewOffsetError()
			}
			newOffset += size
		default:
			return 0, mmdberrors.NewInvalidDatabaseError("unknown type: %d", kind)
		}

		offset = newOffset
		numberToSkip--
	}
	return offset, nil
}

func wrapRootDecodeError(err error, offset uint) error {
	// Check if error already has context (including path), if so just add offset if missing
	var contextErr mmdberrors.ContextualError
	if errors.As(err, &contextErr) {
		if contextErr.Offset != 0 || offset == 0 {
			return err
		}
		pathBuilder := mmdberrors.NewPathBuilder()
		if contextErr.Path != "" && contextErr.Path != "/" {
			pathBuilder.ParseAndExtend(contextErr.Path)
		}
		return mmdberrors.WrapWithContext(contextErr.Err, offset, pathBuilder)
	}

	// Plain error, add offset
	return mmdberrors.WrapWithContext(err, offset, nil)
}

//nolint:gocyclo // Keep path navigation and final depth validation together.
func (d *ReflectionDecoder) decodePath(
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
		if typeNum.IsContainer() {
			if err := d.reserveActiveContainer(typeNum, size); err != nil {
				return d.wrapError(err, offset)
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
				key, offset, err = d.decodePathKey(offset)
				if err != nil {
					return err
				}
				if string(key) == v {
					continue PATH
				}
				offset, err = d.nextValueOffsetBudgeted(offset, 1)
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
			offset, err = d.nextValueOffsetBudgeted(offset, i)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected path element at index %d (%v): %T", i, v, v)
		}
	}
	resultValue := addressableValue{Value: result.Elem()}
	// Let decodeValue report excessive depth before any fast path can succeed.
	if len(path) <= maximumDataStructureDepth {
		switch v.(type) {
		case *any:
			err := d.decodeAnyWithBudget(offset, resultValue, len(path))
			if err != nil {
				return d.wrapError(err, offset)
			}
			return nil
		case *string, *[]byte:
			if d.tryFastDecodePathPayload(offset, resultValue) {
				return nil
			}
		case *bool, *int32, *uint, *uint16, *uint32, *uint64, *float32, *float64:
			if _, ok := d.tryFastDecodeTyped(offset, resultValue, resultValue.Type()); ok {
				return nil
			}
		default:
		}
	}
	_, err := d.decodeValue(
		offset,
		resultValue,
		len(path),
	)
	if err != nil {
		return d.wrapError(err, offset)
	}
	return nil
}

func (d *ReflectionDecoder) tryFastDecodePathPayload(
	offset uint,
	result addressableValue,
) bool {
	if d.budgetRemaining == 0 {
		if result.Type() == stringType {
			return d.tryFastDecodeUnbudgetedString(offset, result)
		}
		return d.tryFastDecodeUnbudgetedBytes(offset, result)
	}
	_, ok := d.tryFastDecodeTyped(offset, result, result.Type())
	return ok
}

func (d *ReflectionDecoder) decodePathKey(offset uint) ([]byte, uint, error) {
	key, nextOffset, err := d.decodeKey(offset)
	if err != nil {
		return nil, 0, err
	}
	if err := d.reserveExactPayload(uint(len(key))); err != nil {
		return nil, 0, err
	}
	return key, nextOffset, nil
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

func (d *ReflectionDecoder) decode(
	offset uint,
	result reflect.Value,
) (uint, error) {
	// Skip makeAddressable's boxing copy whenever result is already addressable.
	// The common Decode(&v) entry passes a non-addressable pointer, but its
	// Elem() is addressable, so we can decode through it directly. Callers that
	// already supplied an addressable Value (e.g., a struct field) take the
	// CanAddr branch. Only non-addressable, non-pointer values need
	// makeAddressable, which allocates.
	if result.Kind() == reflect.Pointer && !result.IsNil() {
		return d.decodeValue(offset, addressableValue{Value: result.Elem()}, 0)
	}
	if result.CanAddr() {
		return d.decodeValue(offset, addressableValue{Value: result}, 0)
	}
	return d.decodeValue(offset, makeAddressable(result), 0)
}

func (d *ReflectionDecoder) decodeValue(
	offset uint,
	result addressableValue,
	depth int,
) (newOffset uint, retErr error) {
	return d.decodeValueImpl(offset, result, depth, true)
}

// decodeValueSkipUnmarshaler decodes a destination that was preclassified as
// implementing neither CursorUnmarshaler nor Unmarshaler. Struct fields and
// map and slice elements use it to skip both reflective type assertions that
// decodeValue would otherwise perform.
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

	// Try custom unmarshaler dispatch only when the type might actually
	// implement one of the interfaces. Struct decoding passes
	// checkUnmarshaler=false when the per-field precomputation established the
	// destination cannot match, avoiding reflective type assertions entirely.
	if checkUnmarshaler && result.CanAddr() && mayImplementUnmarshaler(result.Type()) {
		if next, handled, err := d.tryCustomUnmarshal(offset, result.Addr()); handled {
			return next, err
		}
	}

	typeNum, size, newOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return 0, err
	}

	if typeNum != KindPointer && result.Kind() == reflect.Uintptr {
		result.Set(reflect.ValueOf(uintptr(offset)))
		return d.nextValueOffsetBudgeted(offset, 1)
	}
	return d.decodeFromType(typeNum, size, newOffset, result, depth+1)
}

func (d *ReflectionDecoder) tryCustomUnmarshal(
	offset uint,
	result reflect.Value,
) (uint, bool, error) {
	if unmarshaler, ok := reflect.TypeAssert[CursorUnmarshaler](result); ok {
		next, err := (Cursor{
			decoder: d.callbackDataDecoder(),
			offset:  offset,
		}).UnmarshalCursor(unmarshaler)
		if err != nil {
			return 0, true, err
		}
		return next.offset, true, nil
	}
	if unmarshaler, ok := reflect.TypeAssert[Unmarshaler](result); ok {
		decoder := acquireDecoder(d.callbackDataDecoder(), offset)
		err := unmarshaler.UnmarshalMaxMindDB(decoder)
		releaseDecoder(decoder)
		if err != nil {
			return 0, true, err
		}
		next, err := d.nextValueOffset(offset, 1)
		return next, true, err
	}
	return 0, false, nil
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
	if d.budgetRemaining != 0 && hasBufferRange(uint(len(d.buffer)), offset, size) {
		if err := d.reserveExactPayload(size); err != nil {
			return 0, err
		}
	}
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
	// A scalar decoded by itself cannot fan out. The first container activates
	// one operation-wide budget regardless of the destination's Go shape.
	if d.budgetRemaining == 0 {
		bounded := newBudgetedDecoder(d)
		return bounded.unmarshalMap(size, offset, result, depth)
	}
	if err := d.reserveActiveContainer(KindMap, size); err != nil {
		return 0, err
	}

	switch result.Kind() {
	case reflect.Struct:
		return d.decodeStruct(size, offset, result, depth)
	case reflect.Map:
		if err := d.validateContainerSize(KindMap, size, offset, depth); err != nil {
			return 0, err
		}
		return d.decodeMap(size, offset, result, depth)
	case reflect.Interface:
		if result.NumMethod() == 0 {
			if err := d.validateContainerSize(KindMap, size, offset, depth); err != nil {
				return 0, err
			}
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
	if pointer >= uint(len(d.buffer)) {
		_, err = d.decodeValue(pointer, result, depth)
		return newOffset, err
	}

	// Check for pointer-to-pointer by looking at what we're about to decode
	// This is done efficiently by checking the control byte at the pointer location
	controlByte := d.buffer[pointer]
	kind := Kind(controlByte >> 5)
	if kind == KindExtended && pointer+1 < uint(len(d.buffer)) {
		kind = Kind(d.buffer[pointer+1] + 7)
	}
	if kind == KindPointer {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"invalid pointer to pointer at offset %d",
			pointer,
		)
	}
	// The destination has already passed unmarshaler dispatch. Decode
	// compact strings without repeating reflection on the pointer target,
	// while preserving the target's depth and payload checks.
	size = uint(controlByte & 0x1f)
	dataOffset := pointer + 1
	if Kind(controlByte>>5) == KindString && result.Kind() == reflect.String &&
		depth <= maximumDataStructureDepth &&
		size < 29 && size <= uint(len(d.buffer))-dataOffset {
		if err := d.reserveExactPayload(size); err != nil {
			return newOffset, err
		}
		result.SetString(d.decodeCompactString(size, dataOffset))
		return newOffset, nil
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
	// Reserve declared children before validation can expose an allocation hint.
	if d.budgetRemaining == 0 {
		bounded := newBudgetedDecoder(d)
		return bounded.unmarshalSlice(size, offset, result, depth)
	}
	if err := d.reserveActiveContainer(KindSlice, size); err != nil {
		return 0, err
	}
	if (result.Kind() != reflect.Slice || result.IsNil() || result.Cap() < int(size)) && size > 0 {
		if err := d.validateContainerSize(KindSlice, size, offset, depth); err != nil {
			return 0, err
		}
	}

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

func (d *ReflectionDecoder) validateContainerSize(
	kind Kind,
	size, offset uint,
	depth int,
) error {
	if err := d.validateContainerBounds(kind, size, offset); err != nil {
		return err
	}

	valueCount := size
	if kind == KindMap {
		valueCount = size * 2
	}

	// Large allocations are uncommon in MMDB records. Validate their complete
	// encoded structure first, while keeping ordinary records single-pass.
	if valueCount >= containerPreflightValueCount {
		validator := newAllocationValidator(&d.DataDecoder, kind, size)
		_, err := validator.validateContainerContents(kind, size, offset, depth)
		return err
	}
	return nil
}

func (d *ReflectionDecoder) validateContainerBounds(kind Kind, size, offset uint) error {
	return validateContainerBounds(uint(len(d.buffer)), kind, size, offset)
}

func validateContainerBounds(bufferLen uint, kind Kind, size, offset uint) error {
	if offset > bufferLen {
		return mmdberrors.NewOffsetError()
	}

	valueCount := size
	if kind == KindMap {
		if size > ^uint(0)/2 {
			return mmdberrors.NewInvalidDatabaseError("container size overflow")
		}
		valueCount = size * 2
	}

	// Every encoded value occupies at least one byte. Reject impossible counts
	// before using an attacker-controlled size as an allocation hint.
	if valueCount > bufferLen-offset {
		return mmdberrors.NewOffsetError()
	}
	return nil
}

// structuralValidator bounds allocation-free validation work for large
// allocation hints and cursor sizing. Pointer targets are visited every time
// they are referenced, so compact fan-out and cycles cannot make this walk
// effectively unbounded.
type structuralValidator struct {
	decoder   *DataDecoder
	remaining uint32
}

func newStructuralValidator(d *DataDecoder) structuralValidator {
	return structuralValidator{
		decoder:   d,
		remaining: decodeExpansionBudgetBytes >> decodeBudgetUnitShift,
	}
}

// newAllocationValidator gives a large concrete destination's direct children
// a free pass: the ordinary bounds check already proves that their count is no
// larger than the input. The fixed allowance then bounds only recursively
// expanded containers during the pre-allocation walk. Payloads are not charged
// because this validator protects structural work rather than dynamic output.
func newAllocationValidator(d *DataDecoder, kind Kind, size uint) structuralValidator {
	validator := newStructuralValidator(d)
	validator.remaining += containerCost(kind, size)
	return validator
}

func (v *structuralValidator) reserve(cost uint32) error {
	if cost > v.remaining {
		return errDecodedRecordTooLarge
	}
	v.remaining -= cost
	return nil
}

func (v *structuralValidator) reserveContainer(kind Kind, size uint) error {
	return v.reserve(containerCost(kind, size))
}

func containerCost(kind Kind, size uint) uint32 {
	cost := uint32(size)
	if kind == KindMap {
		cost *= 2
	}
	return cost
}

//nolint:nestif // Keeping compact values inline avoids a call for every entry.
func (v *structuralValidator) validateContainerContents(
	kind Kind,
	size uint,
	offset uint,
	depth int,
) (uint, error) {
	bufferLen := uint(len(v.decoder.buffer))
	if err := validateContainerBounds(bufferLen, kind, size, offset); err != nil {
		return 0, err
	}
	if err := v.reserveContainer(kind, size); err != nil {
		return 0, err
	}
	for range size {
		if kind == KindMap {
			// Map keys are ordinarily directly encoded short strings. Validate
			// that form inline and leave pointers and extended sizes to the
			// complete validator below.
			ctrlByte := byte(0)
			if offset < bufferLen {
				ctrlByte = v.decoder.buffer[offset]
			}
			keySize := uint(ctrlByte & 0x1f)
			if offset >= bufferLen || Kind(ctrlByte>>5) != KindString || keySize >= 29 ||
				!hasBufferRange(bufferLen, offset+1, keySize) {
				var err error
				offset, err = v.validateValue(offset, depth, true)
				if err != nil {
					return 0, err
				}
			} else {
				offset += 1 + keySize
			}
		}

		// Booleans have a fixed two-byte encoding and no payload. They are
		// common in large generated containers.
		if offset+1 < bufferLen && v.decoder.buffer[offset] <= 1 &&
			v.decoder.buffer[offset+1] == 7 {
			offset += 2
			continue
		}

		// Direct strings and compact integers can be validated without the
		// full control-data dispatch. This is especially valuable at the large
		// container threshold, where every child is preflighted before allocation.
		if offset < bufferLen {
			ctrlByte := v.decoder.buffer[offset]
			valueSize := uint(ctrlByte & 0x1f)
			valid := false
			switch Kind(ctrlByte >> 5) {
			case KindString:
				valid = valueSize < 29
			case KindUint16:
				valid = valueSize <= 2
			case KindUint32:
				valid = valueSize <= 4
			default:
				// Use the complete validator below.
			}
			if valid && hasBufferRange(bufferLen, offset+1, valueSize) {
				offset += 1 + valueSize
				continue
			}
		}

		var err error
		offset, err = v.validateValue(offset, depth, false)
		if err != nil {
			return 0, err
		}
	}
	return offset, nil
}

func (v *structuralValidator) validateValue(
	offset uint,
	depth int,
	requireString bool,
) (uint, error) {
	if depth > maximumDataStructureDepth {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"exceeded maximum data structure depth; database is likely corrupt",
		)
	}

	kind, size, dataOffset, err := v.decoder.decodeCtrlData(offset)
	if err != nil {
		return 0, err
	}
	if kind == KindPointer {
		pointer, nextOffset, err := v.decoder.decodePointer(size, dataOffset)
		if err != nil {
			return 0, err
		}
		targetKind, _, _, err := v.decoder.decodeCtrlData(pointer)
		if err != nil {
			return 0, err
		}
		if targetKind == KindPointer {
			return 0, mmdberrors.NewInvalidDatabaseError(
				"invalid pointer to pointer at offset %d",
				pointer,
			)
		}
		_, err = v.validateValue(pointer, depth+1, requireString)
		return nextOffset, err
	}

	if requireString && kind != KindString {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"unexpected map key type: %s",
			kind.String(),
		)
	}

	bufferLen := uint(len(v.decoder.buffer))
	switch kind {
	case KindMap, KindSlice:
		return v.validateContainerContents(kind, size, dataOffset, depth+1)
	default:
		return v.validateScalar(kind, size, dataOffset, bufferLen)
	}
}

func (*structuralValidator) validateScalar(
	kind Kind,
	size uint,
	dataOffset uint,
	bufferLen uint,
) (uint, error) {
	switch kind {
	case KindString, KindBytes:
		if !hasBufferRange(bufferLen, dataOffset, size) {
			return 0, mmdberrors.NewOffsetError()
		}
		return dataOffset + size, nil
	case KindFloat64:
		return validateFixedSize(kind, size, dataOffset, bufferLen, 8)
	case KindFloat32:
		return validateFixedSize(kind, size, dataOffset, bufferLen, 4)
	case KindInt32, KindUint32:
		return validateMaximumSize(kind, size, dataOffset, bufferLen, 4)
	case KindUint16:
		return validateMaximumSize(kind, size, dataOffset, bufferLen, 2)
	case KindUint64:
		return validateMaximumSize(kind, size, dataOffset, bufferLen, 8)
	case KindUint128:
		return validateMaximumSize(kind, size, dataOffset, bufferLen, 16)
	case KindBool:
		if size > 1 {
			return 0, mmdberrors.NewInvalidDatabaseError("invalid bool size: %d", size)
		}
		return dataOffset, nil
	default:
		return 0, mmdberrors.NewInvalidDatabaseError("unknown type: %d", kind)
	}
}

func validateFixedSize(kind Kind, size, offset, bufferLen, expected uint) (uint, error) {
	if size != expected {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"invalid %s size: %d",
			kind.String(),
			size,
		)
	}
	return validateMaximumSize(kind, size, offset, bufferLen, expected)
}

func validateMaximumSize(kind Kind, size, offset, bufferLen, maximum uint) (uint, error) {
	if size > maximum {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"invalid %s size: %d",
			kind.String(),
			size,
		)
	}
	if !hasBufferRange(bufferLen, offset, size) {
		return 0, mmdberrors.NewOffsetError()
	}
	return offset + size, nil
}

//nolint:nestif // Keep compact bounded decoding inline on this hot path.
func (d *ReflectionDecoder) unmarshalString(
	size, offset uint,
	result addressableValue,
) (uint, error) {
	var value string
	var newOffset uint
	if d.budgetRemaining != 0 && size < 29 && offset > 0 &&
		hasBufferRange(uint(len(d.buffer)), offset, size) {
		if err := d.reserveExactPayload(size); err != nil {
			return 0, err
		}
		value = d.decodeCompactString(size, offset)
		newOffset = offset + size
	} else {
		if d.budgetRemaining != 0 && hasBufferRange(uint(len(d.buffer)), offset, size) {
			if err := d.reserveExactPayload(size); err != nil {
				return 0, err
			}
		}
		var err error
		value, newOffset, err = d.decodeString(size, offset)
		if err != nil {
			return 0, err
		}
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
			"unsupported uint type: %d", uintType,
		)
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

//nolint:nestif // Keeping the type-specialized hot paths inline avoids per-entry calls.
func (d *ReflectionDecoder) decodeMap(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	mapType := result.Type()
	keyType := mapType.Key()
	keyKind := keyType.Kind()
	customKey := keyType != stringType && keyKind == reflect.String &&
		reflect.PointerTo(keyType).NumMethod() != 0 &&
		mayImplementUnmarshaler(keyType)
	plainStringKey := keyKind == reflect.String && !customKey
	elemType := mapType.Elem()
	if result.IsNil() {
		result.Set(reflect.MakeMapWithSize(mapType, int(size)))
	}
	// Pre-allocated values for efficient reuse
	keyVal := reflect.New(keyType).Elem()
	keyValue := addressableValue{Value: keyVal}
	elemMayUnmarshal := typeMayImplementUnmarshaler(elemType)
	elemFast := !elemMayUnmarshal && isFastDecodeType(elemType)
	var elemValue addressableValue
	// Pre-allocate element value to reduce allocations
	elemVal := reflect.New(elemType).Elem()
	elemValue = addressableValue{Value: elemVal}
	for range size {
		var err error

		// Reuse keyValue by zeroing it
		keyValue.SetZero()
		keyOffset := offset
		if plainStringKey {
			var key string
			if d.budgetRemaining == 0 {
				key, offset, err = d.decodeStringKey(offset)
			} else {
				key, offset, err = d.decodeBudgetedStringKey(offset)
			}
			if err != nil {
				return 0, err
			}
			keyValue.SetString(key)
		} else {
			var key []byte
			key, offset, err = d.decodeKey(offset)
			if err != nil {
				return 0, err
			}
			// Custom callbacks bypass reflection's payload accounting.
			if customKey && d.budgetRemaining != 0 {
				if err := d.reserveExactPayload(uint(len(key))); err != nil {
					return 0, err
				}
			}
			if customKey {
				err = d.unmarshalValidatedMapKey(keyOffset, keyValue.Addr())
			} else {
				// Preserve destination-type errors after decodeKey has validated
				// the database's key encoding.
				_, err = d.decodeValue(keyOffset, keyValue, depth)
			}
			if err != nil {
				return 0, d.wrapErrorWithMapKey(err, string(key))
			}
		}

		// Reuse elemValue by zeroing it
		elemValue.SetZero()

		decoded := false
		if elemFast {
			if fastOffset, ok := d.tryFastDecodeTyped(offset, elemValue, elemType); ok {
				offset = fastOffset
				decoded = true
			}
		}
		if !decoded {
			if elemMayUnmarshal {
				offset, err = d.decodeValue(offset, elemValue, depth)
			} else {
				offset, err = d.decodeValueSkipUnmarshaler(offset, elemValue, depth)
			}
			if err != nil {
				return 0, d.wrapErrorWithMapKey(err, keyValue.String())
			}
		}

		result.SetMapIndex(keyValue.Value, elemValue.Value)
	}
	return offset, nil
}

func (d *ReflectionDecoder) decodeBudgetedStringKey(offset uint) (string, uint, error) {
	key, cacheOffset, nextOffset, err := d.decodeKeyAt(offset)
	if err != nil {
		return "", 0, err
	}
	if err := d.reserveExactPayload(uint(len(key))); err != nil {
		return "", 0, err
	}
	if d.stringCache == nil {
		return string(key), nextOffset, nil
	}
	return d.stringCache.internAt(cacheOffset, key), nextOffset, nil
}

// unmarshalValidatedMapKey invokes a custom map-key decoder after decodeKey
// has already validated the raw key and found the map value's offset. The
// cursor callback must still return a valid successor, but neither callback's
// successor needs to be recomputed here.
func (d *ReflectionDecoder) unmarshalValidatedMapKey(
	offset uint,
	result reflect.Value,
) error {
	if unmarshaler, ok := reflect.TypeAssert[CursorUnmarshaler](result); ok {
		_, err := (Cursor{
			decoder: d.callbackDataDecoder(),
			offset:  offset,
		}).UnmarshalCursor(unmarshaler)
		return err
	}

	unmarshaler, _ := reflect.TypeAssert[Unmarshaler](result)
	decoder := acquireDecoder(d.callbackDataDecoder(), offset)
	err := unmarshaler.UnmarshalMaxMindDB(decoder)
	releaseDecoder(decoder)
	return err
}

func (d *ReflectionDecoder) decodeSlice(
	size uint,
	offset uint,
	result addressableValue,
	depth int,
) (uint, error) {
	elemType := result.Type().Elem()
	elemMayUnmarshal := typeMayImplementUnmarshaler(elemType)
	elemFast := !elemMayUnmarshal && isFastDecodeType(elemType)
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
		decoded := false
		if elemFast {
			if fastOffset, ok := d.tryFastDecodeTyped(offset, elemValue, elemType); ok {
				offset = fastOffset
				decoded = true
			}
		}
		if !decoded {
			if elemMayUnmarshal {
				offset, err = d.decodeValue(offset, elemValue, depth)
			} else {
				offset, err = d.decodeValueSkipUnmarshaler(offset, elemValue, depth)
			}
			if err != nil {
				return 0, d.wrapErrorWithSliceIndex(err, int(i))
			}
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
	if len(fields.namedFields) == 0 {
		return d.skipStructFields(size, offset)
	}

	// Single-phase processing: decode only the dominant fields
	for range size {
		var (
			err error
			key []byte
		)
		var keyOffset uint
		key, keyOffset, offset, err = d.decodeKeyAt(offset)
		if err != nil {
			return 0, err
		}
		// The string() does not create a copy due to this compiler
		// optimization: https://github.com/golang/go/issues/3512
		entry := &fields.offsetFields[keyOffset&(fieldOffsetCacheSlots-1)]
		fieldInfo := entry.Load()
		ok := fieldInfo != nil && fieldInfo.name == string(key)
		if !ok {
			fieldInfo, ok = fields.fieldForKey(key)
			if ok && entry.Load() == nil {
				entry.CompareAndSwap(nil, fieldInfo)
			}
		}
		if !ok {
			if d.structFieldValueIsInlineContainer(offset) {
				offset, err = d.nextValueOffsetBudgetedSlow(offset, 1)
			} else {
				// Scalars consume work proportional to their inline encoding, and
				// pointer tokens are skipped without expanding their targets.
				offset, err = d.nextValueOffset(offset, 1)
			}
			if err != nil {
				return 0, err
			}
			continue
		}

		// Defer uncommon embedded-pointer initialization until the field is
		// actually needed. For maxsize fields, validate before that allocation.
		fieldValue := result.fieldByIndex(fieldInfo.index0, fieldInfo.index, false)
		if !fieldValue.IsValid() {
			if fieldInfo.dispatch == dispatchMaxSize {
				err = (Cursor{
					decoder: &d.DataDecoder,
					offset:  offset,
				}).CheckMaxSize(fieldInfo.maxSizeKinds, fieldInfo.maxSize)
				if err != nil {
					return 0, d.wrapErrorWithMapKey(err, string(key))
				}
			}
			fieldValue = result.fieldByIndex(fieldInfo.index0, fieldInfo.index, true)
		}
		if !fieldValue.IsValid() {
			// Field access failed, skip this field
			if d.structFieldValueIsInlineContainer(offset) {
				offset, err = d.nextValueOffsetBudgetedSlow(offset, 1)
			} else {
				offset, err = d.nextValueOffset(offset, 1)
			}
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
		case dispatchMaxSize:
			offset, err = d.decodeValueMaxSize(
				offset,
				fieldValue,
				depth,
				fieldInfo.maxSizeKinds,
				fieldInfo.maxSize,
				fieldInfo.maxSizeCustom,
			)
		default: // dispatchPlain
			offset, err = d.decodeValueSkipUnmarshaler(offset, fieldValue, depth)
		}
		if err != nil {
			return 0, d.wrapErrorWithMapKey(err, string(key))
		}
	}
	return offset, nil
}

func (d *ReflectionDecoder) skipStructFields(size, offset uint) (uint, error) {
	for range size {
		var err error
		_, offset, err = d.decodeKey(offset)
		if err != nil {
			return 0, err
		}
		if d.structFieldValueIsInlineContainer(offset) {
			offset, err = d.nextValueOffsetBudgetedSlow(offset, 1)
		} else {
			offset, err = d.nextValueOffset(offset, 1)
		}
		if err != nil {
			return 0, err
		}
	}
	return offset, nil
}

//nolint:nestif // Fast string decoding and generic fallback deliberately share this dispatch.
func (d *ReflectionDecoder) decodeValueMaxSize(
	offset uint,
	result addressableValue,
	depth int,
	expected KindSet,
	maximum uint64,
	customUnmarshaler bool,
) (uint, error) {
	if result.Kind() == reflect.Pointer ||
		(result.CanAddr() && customUnmarshaler) {
		return d.checkMaxSizeThenDecode(offset, result, depth, expected, maximum)
	}
	if depth > maximumDataStructureDepth {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"exceeded maximum data structure depth; database is likely corrupt",
		)
	}
	if result.Kind() == reflect.String && expected == NewKindSet(KindString) {
		cursor := Cursor{
			decoder: &d.DataDecoder,
			offset:  offset,
		}
		if d.budgetRemaining != 0 && uint64(d.payloadRemaining) < maximum {
			// Preserve maxsize error precedence when both the schema and
			// operation limits reject the value. These checks only run after
			// earlier payloads have brought the remaining allowance below the
			// field limit, leaving the ordinary bounded-string path unchanged.
			if err := cursor.CheckMaxSize(expected, maximum); err != nil {
				return 0, err
			}
			if err := cursor.CheckMaxSize(expected, uint64(d.payloadRemaining)); err != nil {
				return 0, errDecodedRecordTooLarge
			}
		}
		value, next, err := cursor.ReadStringMaxSize(maximum)
		if err != nil {
			var mismatch UnexpectedKindError
			if errors.As(err, &mismatch) {
				return d.decodeValueSkipUnmarshaler(offset, result, depth)
			}
			return 0, err
		}
		if d.budgetRemaining != 0 {
			size := uint(len(value))
			if size > uint(d.payloadRemaining) {
				return 0, errDecodedRecordTooLarge
			}
			d.payloadRemaining -= uint32(size)
		}
		result.SetString(value)
		return next.offset, nil
	}
	typeNum, size, dataOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return 0, err
	}
	if typeNum == KindPointer {
		return d.checkMaxSizeThenDecode(offset, result, depth, expected, maximum)
	}
	if expected.Contains(typeNum) && uint64(size) > maximum {
		return 0, d.wrapError(mmdberrors.NewInvalidDatabaseError(
			"%s size %d exceeds maxsize %d",
			typeNum,
			size,
			maximum,
		), offset)
	}
	return d.decodeFromType(typeNum, size, dataOffset, result, depth+1)
}

func (d *ReflectionDecoder) checkMaxSizeThenDecode(
	offset uint,
	result addressableValue,
	depth int,
	expected KindSet,
	maximum uint64,
) (uint, error) {
	err := (Cursor{
		decoder: &d.DataDecoder,
		offset:  offset,
	}).CheckMaxSize(expected, maximum)
	if err != nil {
		return 0, err
	}
	return d.decodeValue(offset, result, depth)
}

// tryDecodeStructWithFields returns the input offset when ok is false so its
// caller can retry the field with the general decoder.
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
		if err := d.reserveActiveContainer(KindMap, size); err != nil {
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
			return offset, false, nil
		}
		if err := d.reserveActiveContainer(KindMap, size); err != nil {
			return 0, true, err
		}
		_, err = d.decodeStructWithFields(size, dataOffset, result, depth+2, fields)
		return pointerEndOffset, true, err
	default:
		return offset, false, nil
	}
}

// tryDecodePointerStructWithFields returns the input offset when ok is false so
// its caller can retry the field with the general decoder.
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
			return offset, false, nil
		}
		decodeDepth = depth + 2
	default:
		return offset, false, nil
	}

	if err := d.reserveActiveContainer(KindMap, size); err != nil {
		return 0, true, err
	}
	newOffset, ok, err = d.decodePointerStructWithFields(
		size,
		dataOffset,
		pointerEndOffset,
		result,
		decodeDepth,
		fields,
	)
	return newOffset, ok, err
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
// once at struct-cache build time. Encoding the choice as a
// single enum (rather than non-orthogonal booleans) makes illegal
// combinations like "fast path AND custom unmarshaler" unrepresentable.
type fieldDispatch uint8

const (
	// dispatchFast: field type satisfies isFastDecodeType and its unwrapped type
	// does not implement a custom unmarshaler. The fast
	// path is attempted; on type-num mismatch it falls back to
	// decodeValueSkipUnmarshaler (which is sound: the field type cannot
	// implement either custom interface by construction).
	dispatchFast fieldDispatch = iota
	// dispatchUnmarshaler: field's unwrapped type is an interface, or its pointer
	// type implements CursorUnmarshaler or Unmarshaler through methods declared
	// on either a value or pointer receiver. Goes through decodeValue, which
	// performs cursor-first type assertions.
	dispatchUnmarshaler
	// dispatchStruct: field is a nested struct whose field set is
	// precomputed and can be decoded without consulting the field cache.
	dispatchStruct
	// dispatchPointerStruct: field is a pointer to a nested struct whose
	// field set is precomputed and whose pointer chain may need allocation.
	dispatchPointerStruct
	// dispatchMaxSize checks a field's schema limit before invoking the general
	// decoder. It is kept out of every unconstrained dispatch path so maxsize
	// support adds no per-field branch to existing schemas.
	dispatchMaxSize
	// dispatchPlain: fallback for fields not selected for custom-unmarshaler,
	// primitive fast, or cached nested-struct dispatch. Uses
	// decodeValueSkipUnmarshaler.
	dispatchPlain
)

type fieldInfo struct {
	fieldType    reflect.Type
	structFields *fieldsType
	name         string
	index        []int
	index0       int
	depth        int
	maxSize      uint64
	maxSizeKinds KindSet
	dispatch     fieldDispatch
	hasTag       bool
	// maxSizeCustom caches whether maxsize must precede custom unmarshaler
	// dispatch. Computing this from reflect.Type consults a sync.Map, so doing
	// it while decoding every tagged field would turn a schema property into a
	// recurring runtime cost.
	maxSizeCustom bool
}

const fieldOffsetCacheSlots = 64

type fieldsType struct {
	validationErr error
	namedFields   map[string]*fieldInfo // Map from field name to field info
	// These fill-once hints avoid fingerprint lookup for recurring key offsets
	// without writes on collisions. Schemas are shared across databases, so
	// callers must compare the complete key with the cached field's name.
	offsetFields      [fieldOffsetCacheSlots]atomic.Pointer[fieldInfo]
	fingerprintFields []fingerprintField
}

type fingerprintField struct {
	field       *fieldInfo
	fingerprint uint64
}

func (fs *fieldsType) fieldForKey(key []byte) (*fieldInfo, bool) {
	field, ok := fs.fieldForFingerprint(fieldKeyFingerprint(key))
	if ok && (field == nil || field.name != string(key)) {
		field, ok = fs.namedFields[string(key)]
	}
	return field, ok
}

func (fs *fieldsType) fieldForFingerprint(fingerprint uint64) (*fieldInfo, bool) {
	mask := uint64(len(fs.fingerprintFields) - 1)
	index := (fingerprint ^ (fingerprint >> 16) ^ (fingerprint >> 32)) & mask
	for {
		entry := fs.fingerprintFields[index]
		if entry.fingerprint == 0 {
			return nil, false
		}
		if entry.fingerprint == fingerprint {
			return entry.field, true
		}
		index = (index + 1) & mask
	}
}

func fieldKeyFingerprint(key []byte) uint64 {
	// Length plus the first and last two bytes distinguish ordinary MMDB
	// field names cheaply. Collisions fall back to the full string map.
	n := len(key)
	// Add one to the length so the explicitly supported empty name does not
	// collide with the zero value used to mark an unoccupied table entry.
	fingerprint := uint64(n+1) << 32
	if n > 0 {
		fingerprint |= uint64(key[0]) << 24
		fingerprint |= uint64(key[n-1]) << 16
	}
	if n > 1 {
		fingerprint |= uint64(key[1]) << 8
		fingerprint |= uint64(key[n-2])
	}
	return fingerprint
}

func makeFingerprintFields(namedFields map[string]*fieldInfo) []fingerprintField {
	tableSize := 1
	for tableSize < len(namedFields)*2 {
		tableSize *= 2
	}
	fingerprintFields := make([]fingerprintField, tableSize)
	mask := uint64(tableSize - 1)
	for _, field := range namedFields {
		fingerprint := fieldKeyFingerprint([]byte(field.name))
		index := (fingerprint ^ (fingerprint >> 16) ^ (fingerprint >> 32)) & mask
		for fingerprintFields[index].fingerprint != 0 &&
			fingerprintFields[index].fingerprint != fingerprint {
			index = (index + 1) & mask
		}
		entry := &fingerprintFields[index]
		if entry.fingerprint != 0 {
			entry.field = nil
			continue
		}
		*entry = fingerprintField{fingerprint: fingerprint, field: field}
	}
	return fingerprintFields
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

	_, err := maxminddbtag.Parse(tag)
	if err != nil {
		if !utf8.ValidString(tag) {
			return invalidMaxMindDBTagError(field.Name)
		}
		return fmt.Errorf("invalid maxminddb struct tag on field %q: %w", field.Name, err)
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

	pointer := reflect.PointerTo(t)
	implements := pointer.Implements(cursorUnmarshalerType) || pointer.Implements(unmarshalerType)
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

//nolint:gocyclo // Field discovery keeps validation and precedence rules in one traversal.
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
			var tagOptions maxminddbtag.Options
			if validationErr == nil {
				validationErr = validateRawMaxMindDBTagValue(field, string(field.Tag))
			}
			if tag, ok := field.Tag.Lookup("maxminddb"); ok {
				if validationErr == nil {
					validationErr = validateTag(field, tag)
				}
				var err error
				tagOptions, err = maxminddbtag.Parse(tag)
				if validationErr == nil && err != nil {
					validationErr = fmt.Errorf(
						"invalid maxminddb struct tag on field %q: %w",
						field.Name,
						err,
					)
				}

				if tagOptions.Ignored {
					continue // Skip ignored fields
				}
				if tagOptions.HasName {
					fieldName = tagOptions.Name
					hasTag = true
				}
			}

			// Validate maxsize before embedded fields can be flattened away.
			if tagOptions.HasMaxSize && validationErr == nil {
				if _, supported := maxSizeKindsForType(field.Type); !supported {
					validationErr = fmt.Errorf(
						"invalid maxminddb struct tag on field %q: maxsize is only supported for maps, slices, strings, and bytes",
						field.Name,
					)
				}
			}

			// Handle embedded structs and embedded pointers to structs
			if field.Anonymous && handleEmbeddedField(
				field, hasTag, &queue, &seen, fieldIndex, entry.depth,
			) {
				continue
			}

			// Resolve dispatch strategy once per field. Custom unmarshaler
			// possibility takes precedence over fast-path eligibility so a named
			// primitive type with a custom pointer receiver takes the slow path.
			// The switch is exhaustive because dispatchPlain is the fallback.
			fieldType := field.Type
			unwrappedFieldType := unwrapPtrType(fieldType)
			var dispatch fieldDispatch
			var structFields *fieldsType
			maxSizeCustom := mayImplementUnmarshaler(unwrappedFieldType)
			maxSizeKinds, maxSizeSupported := maxSizeKindsForField(
				fieldType,
				maxSizeCustom,
			)
			switch {
			case tagOptions.HasMaxSize && maxSizeSupported:
				dispatch = dispatchMaxSize
			case maxSizeCustom ||
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
				index:         fieldIndex, // Will be reindexed later for optimization
				name:          fieldName,
				hasTag:        hasTag,
				depth:         entry.depth,
				fieldType:     fieldType,
				structFields:  structFields,
				dispatch:      dispatch,
				maxSize:       tagOptions.MaxSize,
				maxSizeKinds:  maxSizeKinds,
				maxSizeCustom: maxSizeCustom,
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
		namedFields:       namedFields,
		fingerprintFields: makeFingerprintFields(namedFields),
		validationErr:     validationErr,
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
	if t == sliceType {
		return true
	}

	switch t.Kind() {
	case reflect.String,
		reflect.Bool,
		reflect.Int32,
		reflect.Uint,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64:
		return true
	case reflect.Pointer:
		return isFastDecodeType(t.Elem())
	default:
		return false
	}
}

func typeMayImplementUnmarshaler(t reflect.Type) bool {
	unwrapped := unwrapPtrType(t)
	return unwrapped.Kind() == reflect.Interface || mayImplementUnmarshaler(unwrapped)
}

// unwrapPtrType strips all pointer indirection from t and returns the
// underlying element type. Used to find the addressable receiver type that
// mayImplementUnmarshaler should check for either custom interface, since
// decoding allocates and dereferences as many *T layers as the field declares.
func unwrapPtrType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func maxSizeKindsForType(t reflect.Type) (KindSet, bool) {
	t = unwrapPtrType(t)
	switch t.Kind() {
	case reflect.Map:
		return NewKindSet(KindMap), true
	case reflect.Slice:
		if t == sliceType {
			return NewKindSet(KindBytes, KindSlice), true
		}
		return NewKindSet(KindSlice), true
	case reflect.String:
		return NewKindSet(KindString), true
	default:
		return 0, false
	}
}

func maxSizeKindsForField(t reflect.Type, customUnmarshaler bool) (KindSet, bool) {
	kinds, supported := maxSizeKindsForType(t)
	if supported && customUnmarshaler {
		// A custom callback may accept any MMDB encoding regardless of its Go
		// type's underlying shape. Apply the schema limit to every size-bearing
		// kind before transferring control to it.
		return supportedMaxSizeKinds, true
	}
	return kinds, supported
}

func (d *ReflectionDecoder) decodeAny(
	offset uint,
	result addressableValue,
	depth int,
) error {
	if !result.IsNil() {
		existing := result.Elem()
		if existing.Kind() == reflect.Pointer && !existing.IsNil() {
			_, err := d.decodeValue(offset, result, depth)
			return err
		}
	}
	kind, size, dataOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return err
	}
	// The destination is an empty interface, and a retained pointer has
	// already been handled above. Avoid repeating destination and integer
	// width dispatch for directly encoded unsigned scalars.
	switch kind {
	case KindUint16:
		value, _, err := d.decodeUint16(size, dataOffset)
		if err != nil {
			return err
		}
		result.Set(reflect.ValueOf(uint64(value)))
		return nil
	case KindUint32:
		value, _, err := d.decodeUint32(size, dataOffset)
		if err != nil {
			return err
		}
		result.Set(reflect.ValueOf(uint64(value)))
		return nil
	case KindUint64:
		value, _, err := d.decodeUint64(size, dataOffset)
		if err != nil {
			return err
		}
		result.Set(reflect.ValueOf(value))
		return nil
	}
	_, err = d.decodeFromType(kind, size, dataOffset, result, depth+1)
	return err
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
// inner decode error. Callers surface failures by re-decoding from the same
// offset via the slow path, which re-encounters the underlying error and
// propagates it with proper context. The fast path itself never logs or wraps.
// String and []byte callers must have an active operation budget; standalone
// payload entry points use their unbudgeted fast paths instead.
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
	case reflect.Slice:
		if expectedType == sliceType && typeNum == KindBytes {
			if size > uint(d.payloadRemaining) {
				return 0, false
			}
			value, finalOffset, err := d.decodeBytes(size, newOffset)
			if err != nil {
				return 0, false
			}
			d.payloadRemaining -= uint32(size)
			result.SetBytes(value)
			return finalOffset, true
		}
	case reflect.String:
		if typeNum == KindString {
			if size > uint(d.payloadRemaining) {
				return 0, false
			}
			value, finalOffset, err := d.decodeString(size, newOffset)
			if err != nil {
				return 0, false
			}
			d.payloadRemaining -= uint32(size)
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
	case reflect.Int32:
		if typeNum == KindInt32 {
			value, finalOffset, err := d.decodeInt32(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetInt(int64(value))
			return finalOffset, true
		}
	case reflect.Float32:
		if typeNum == KindFloat32 {
			value, finalOffset, err := d.decodeFloat32(size, newOffset)
			if err != nil {
				return 0, false
			}
			result.SetFloat(float64(value))
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
		// Handle pointers to fast scalar types without leaving the typed path.
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

func (d *ReflectionDecoder) tryFastDecodeUnbudgetedString(
	offset uint,
	result addressableValue,
) bool {
	bufferLen := uint(len(d.buffer))
	if offset < bufferLen {
		ctrlByte := d.buffer[offset]
		size := uint(ctrlByte & 0x1f)
		dataOffset := offset + 1
		if Kind(ctrlByte>>5) == KindString && size < 29 &&
			hasBufferRange(bufferLen, dataOffset, size) {
			result.SetString(d.decodeCompactString(size, dataOffset))
			return true
		}
	}
	typeNum, size, newOffset, err := d.decodeCtrlData(offset)
	if err != nil {
		return false
	}
	if typeNum == KindString {
		value, _, err := d.decodeString(size, newOffset)
		if err != nil {
			return false
		}
		result.SetString(value)
		return true
	}
	return false
}

func (d *ReflectionDecoder) tryFastDecodeUnbudgetedBytes(
	offset uint,
	result addressableValue,
) bool {
	typeNum, size, newOffset, err := d.decodeCtrlData(offset)
	if err != nil || typeNum != KindBytes {
		return false
	}
	value, _, err := d.decodeBytes(size, newOffset)
	if err != nil {
		return false
	}
	result.SetBytes(value)
	return true
}
