package encoder

import (
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// ZeroKind is what makes the value of a field zero for omitzero, as encoding/json decides it: by the IsZero
// method of the type if it has one, and else by reflect.Value.IsZero.
type ZeroKind uint8

const (
	ZeroNever ZeroKind = iota
	// ZeroBits8 to ZeroBits64 are a value of 1 to 8 bytes whose bits are all zero: a bool, an integer,
	// a pointer, a map...
	ZeroBits8
	ZeroBits16
	ZeroBits32
	ZeroBits64
	// ZeroFloat32 and ZeroFloat64 are a float which is 0 or -0: reflect.Value.IsZero compares a float by its
	// value.
	ZeroFloat32
	ZeroFloat64
	ZeroString       // a string of no byte
	ZeroNilSlice     // a nil slice
	ZeroNilInterface // a nil interface value
	ZeroReflect      // a struct, an array or a complex number, as reflect.Value.IsZero decides
	ZeroMethod       // a type which has IsZero, which decides it
)

type isZeroer interface {
	IsZero() bool
}

var isZeroerType = reflect.TypeOf((*isZeroer)(nil)).Elem()

// zeroKindOf returns what makes a value of the type zero.
func zeroKindOf(typ reflect.Type) ZeroKind {
	if typ.Implements(isZeroerType) || reflect.PointerTo(typ).Implements(isZeroerType) {
		return ZeroMethod
	}
	switch typ.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Ptr, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		// the value is zero if and only if its bits are.
		switch typ.Size() {
		case 1:
			return ZeroBits8
		case 2:
			return ZeroBits16
		case 4:
			return ZeroBits32
		case 8:
			return ZeroBits64
		}
	case reflect.Float32:
		return ZeroFloat32
	case reflect.Float64:
		return ZeroFloat64
	case reflect.String:
		return ZeroString
	case reflect.Slice:
		return ZeroNilSlice
	case reflect.Interface:
		return ZeroNilInterface
	}
	return ZeroReflect
}

// IsZeroField is whether the value at p, of the field of the opcode, is zero for omitzero.
func IsZeroField(code *Opcode, p unsafe.Pointer) bool {
	switch code.ZeroKind {
	case ZeroBits8:
		return *(*uint8)(p) == 0
	case ZeroBits16:
		return *(*uint16)(p) == 0
	case ZeroBits32:
		return *(*uint32)(p) == 0
	case ZeroBits64:
		return *(*uint64)(p) == 0
	case ZeroFloat32:
		return *(*float32)(p) == 0
	case ZeroFloat64:
		return *(*float64)(p) == 0
	case ZeroString:
		return len(*(*string)(p)) == 0
	case ZeroNilSlice:
		return (*runtime.SliceHeader)(p).Data == nil
	case ZeroNilInterface:
		return *(*unsafe.Pointer)(p) == nil
	case ZeroReflect:
		return reflect.NewAt(runtime.TypeOfPtr(code.Type), p).Elem().IsZero()
	case ZeroMethod:
		return isZeroByMethod(reflect.NewAt(runtime.TypeOfPtr(code.Type), p))
	}
	return false
}

// isZeroByMethod is whether the value which ptr points to is zero by its IsZero method, as encoding/json
// decides it: a nil pointer, a nil interface value or one which holds a nil pointer is zero without a call.
func isZeroByMethod(ptr reflect.Value) bool {
	v := ptr.Elem()
	typ := v.Type()
	switch {
	case typ.Kind() == reflect.Interface && typ.Implements(isZeroerType):
		return v.IsNil() || (v.Elem().Kind() == reflect.Ptr && v.Elem().IsNil()) || v.Interface().(isZeroer).IsZero()
	case typ.Kind() == reflect.Ptr && typ.Implements(isZeroerType):
		return v.IsNil() || v.Interface().(isZeroer).IsZero()
	case typ.Implements(isZeroerType):
		return v.Interface().(isZeroer).IsZero()
	}
	return ptr.Interface().(isZeroer).IsZero()
}
