package decoder

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonnum"
)

// AssignValue sets src, cast to the type which dst points to, to the value dst points to. dst must be a pointer which
// is not nil. The value is not changed when src cannot be cast.
func AssignValue(src, dst reflect.Value) error {
	if dst.Kind() != reflect.Ptr || dst.IsNil() {
		var typ reflect.Type
		if dst.IsValid() {
			typ = dst.Type()
		}
		return &errors.InvalidUnmarshalError{Type: typ}
	}
	return assignValue(dst.Elem(), src, &visits{})
}

// pathDepthBeforeCycles is how many objects and arrays ( maps, slices, arrays and structs ), or pointers in a row, a
// cast or a recursive descent goes into before it records them to find a cycle: 1000, the startDetectingCyclesAfter
// of encoding/json, so that a value which is not that deep costs no record. It only delays the error of a cycle.
const pathDepthBeforeCycles = 1000

// pathMaxDepth is how many objects and arrays ( maps, slices, arrays and structs ) a cast or a recursive descent goes
// into at most, past which it returns an error instead of exhausting the stack: the levels of a value which
// encoding/json decodes and encodes, maxDecodeNestingDepth, in the slice of such values which Path.Unmarshal casts.
// The pointers and interfaces which lead from one to the next are followed in a loop, not deeper, and a struct
// embedded in another by its value with no name in its tag, which Marshal writes in the object of the other, is gone
// through as a part of it ( it is as deep as its type at most ).
const pathMaxDepth = maxDecodeNestingDepth + 1

// visit is a value which a cast or a recursive descent is inside of, by its address and type.
type visit struct {
	ptr unsafe.Pointer
	typ reflect.Type
	len int
}

// visits are the objects and arrays which a cast or a recursive descent is inside of, so that it stops with an error
// at a value which holds itself, which has no end, or which is nested deeper than pathMaxDepth. A value which holds
// itself does so through a map, a slice, or a pointer to an array or a struct, which is in a cycle with it.
type visits struct {
	depth int
	seen  map[visit]struct{}
}

// containerVisit returns the visit of v, an object or an array, or false if v cannot be in a cycle: a nil or empty map
// or slice, or an array or struct which has no address, as no pointer leads to it.
func containerVisit(v reflect.Value) (visit, bool) {
	switch v.Kind() {
	case reflect.Map:
		return visit{ptr: v.UnsafePointer(), typ: v.Type()}, !v.IsNil()
	case reflect.Slice:
		return visit{ptr: v.UnsafePointer(), typ: v.Type(), len: v.Len()}, v.Len() != 0
	}
	if !v.CanAddr() {
		return visit{}, false
	}
	return visit{ptr: v.Addr().UnsafePointer(), typ: v.Type()}, true
}

// enter records that a cast or a recursive descent goes into v, an object or an array, and returns an error if it is
// inside of v already, as v holds itself, or past pathMaxDepth. leave records that it leaves v again.
func (s *visits) enter(v reflect.Value) error {
	s.depth++
	if s.depth <= pathDepthBeforeCycles {
		return nil
	}
	if s.depth > pathMaxDepth {
		return &errors.UnsupportedValueError{Value: v, Str: "exceeded max depth"}
	}
	key, ok := containerVisit(v)
	if !ok {
		return nil
	}
	if _, ok := s.seen[key]; ok {
		if v.Kind() == reflect.Array || v.Kind() == reflect.Struct {
			// the cycle goes through a pointer to it, which Marshal names too
			return errCycle(v.Addr())
		}
		return errCycle(v)
	}
	if s.seen == nil {
		s.seen = map[visit]struct{}{}
	}
	s.seen[key] = struct{}{}
	return nil
}

func (s *visits) leave(v reflect.Value) {
	if s.depth > pathDepthBeforeCycles {
		if key, ok := containerVisit(v); ok {
			delete(s.seen, key)
		}
	}
	s.depth--
}

// pointerVisits are the pointers which a value leads through, one after the other, to the value it holds, so that a
// pointer which leads back to itself, with no object or array between ( type P *P, or an interface which holds a
// pointer to itself ), stops with an error.
type pointerVisits struct {
	count int
	seen  map[visit]struct{}
}

// follow records that v, a pointer which is not nil, is followed, and returns an error if it was before.
func (s *pointerVisits) follow(v reflect.Value) error {
	s.count++
	if s.count <= pathDepthBeforeCycles {
		return nil
	}
	key := visit{ptr: v.UnsafePointer(), typ: v.Type()}
	if _, ok := s.seen[key]; ok {
		return errCycle(v)
	}
	if s.seen == nil {
		s.seen = map[visit]struct{}{}
	}
	s.seen[key] = struct{}{}
	return nil
}

func errCycle(v reflect.Value) error {
	return &errors.UnsupportedValueError{Value: v, Str: fmt.Sprintf("encountered a cycle via %s", v.Type())}
}

// assignValue sets v, cast to the type of dst, to dst, which can be set. A null v, the zero Value or a nil pointer or
// interface, as JSON null is decoded to, is cast to the zero value. dst is not changed when v cannot be cast: a
// value which holds others is made whole before it is set.
func assignValue(dst, v reflect.Value, seen *visits) error {
	var pointers pointerVisits
	for {
		switch v.Kind() {
		case reflect.Invalid:
			dst.SetZero()
			return nil
		case reflect.Ptr, reflect.Interface:
			if v.IsNil() {
				dst.SetZero()
				return nil
			}
		}
		if isScalarKind(dst.Kind()) {
			// a value of another kind is not assignable to a scalar: a scalar is cast to it, and assignInside reaches
			// one in the rest
			if isScalarKind(v.Kind()) {
				return assignScalar(dst, v)
			}
		} else if t := dst.Type(); t.Kind() != reflect.Map && v.Type().AssignableTo(t) {
			// a map is copied by assignMap
			dst.Set(v)
			return nil
		}
		if v.Kind() == reflect.Interface {
			v = v.Elem()
			continue
		}
		if dst.Kind() == reflect.Ptr {
			return assignPointer(dst, v, seen)
		}
		if v.Kind() != reflect.Ptr {
			break
		}
		if err := pointers.follow(v); err != nil {
			return err
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		if err := seen.enter(v); err != nil {
			return err
		}
		err := assignInside(dst, v, seen)
		seen.leave(v)
		return err
	}
	return assignInside(dst, v, seen)
}

// assignPointer is assignValue to dst, a pointer, of v, which is not null, nor an interface, nor assignable to dst:
// what dst points to is allocated, and v is cast to it.
func assignPointer(dst, v reflect.Value, seen *visits) error {
	t := dst.Type()
	if !pointerTypesTake(t, v.Type()) {
		return fmt.Errorf("failed to cast to %s from %s", t, v.Type())
	}
	ptr := reflect.New(t.Elem())
	if err := assignValue(ptr.Elem(), v, seen); err != nil {
		return err
	}
	dst.Set(ptr)
	return nil
}

// pointerTypesTake reports whether a value of the type vt is cast to the pointer type t: whether, of the types which t
// leads to, each one pointing to the next, one takes vt as it is, or is not a pointer, before they come back to one
// of them. A pointer type which leads back to itself ( type P *P ) takes nothing else but nil.
func pointerTypesTake(t, vt reflect.Type) bool {
	var buf [4]reflect.Type
	types := buf[:0]
	for t.Kind() == reflect.Ptr {
		if slices.Contains(types, t) {
			return false
		}
		types = append(types, t)
		t = t.Elem()
		if t.Kind() != reflect.Map && vt.AssignableTo(t) {
			return true
		}
	}
	return true
}

// assignInside is assignValue of a v which is not null, nor a pointer or an interface, nor assigned as it is, to a
// dst which is not a pointer.
func assignInside(dst, v reflect.Value, seen *visits) error {
	if isScalarKind(dst.Kind()) {
		if v.Kind() == reflect.Array || v.Kind() == reflect.Slice {
			// the first element is cast
			if v.Len() == 0 {
				return fmt.Errorf("failed to cast to %s from empty %s", dst.Type(), v.Kind())
			}
			return assignValue(dst, v.Index(0), seen)
		}
		return assignScalar(dst, v)
	}
	switch dst.Kind() {
	case reflect.Array:
		return assignArray(dst, v, seen)
	case reflect.Slice:
		return assignSlice(dst, v, seen)
	case reflect.Map:
		return assignMap(dst, v, seen)
	case reflect.Struct:
		return assignStruct(dst, v, seen)
	}
	return fmt.Errorf("failed to cast to %s from %s", dst.Type(), v.Type())
}

// isScalarKind reports whether a value of the kind is a number, a string or a bool, which assignScalar casts to.
func isScalarKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.String, reflect.Bool:
		return true
	}
	return false
}

// assignScalar sets v, which is not an array or a slice, cast to the kind of dst, a number, a string or a bool.
func assignScalar(dst, v reflect.Value) error {
	switch dst.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := castInt(v)
		if err != nil {
			return err
		}
		dst.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u, err := castUint(v)
		if err != nil {
			return err
		}
		dst.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := castFloat(v)
		if err != nil {
			return err
		}
		dst.SetFloat(f)
	case reflect.String:
		s, err := castString(v)
		if err != nil {
			return err
		}
		if dst.Type() == jsonNumberType && !jsonnum.IsValid([]byte(s)) {
			// a json.Number is a number of JSON
			return fmt.Errorf("failed to cast to json.Number from %q", s)
		}
		dst.SetString(s)
	default:
		b, err := castBool(v)
		if err != nil {
			return err
		}
		dst.SetBool(b)
	}
	return nil
}

func castInt(v reflect.Value) (int64, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int64(v.Uint()), nil
	case reflect.String:
		return strconv.ParseInt(v.String(), 10, 64)
	case reflect.Bool:
		if v.Bool() {
			return 1, nil
		}
		return 0, nil
	case reflect.Float32, reflect.Float64:
		return int64(v.Float()), nil
	}
	return 0, fmt.Errorf("failed to cast to int64 from %s", v.Kind())
}

func castUint(v reflect.Value) (uint64, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint(), nil
	case reflect.String:
		return strconv.ParseUint(v.String(), 10, 64)
	case reflect.Bool:
		if v.Bool() {
			return 1, nil
		}
		return 0, nil
	case reflect.Float32, reflect.Float64:
		return uint64(v.Float()), nil
	}
	return 0, fmt.Errorf("failed to cast to uint64 from %s", v.Kind())
}

func castString(v reflect.Value) (string, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprint(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return fmt.Sprint(v.Uint()), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		if v.Bool() {
			return "true", nil
		}
		return "false", nil
	case reflect.Float32, reflect.Float64:
		return fmt.Sprint(v.Float()), nil
	}
	return "", fmt.Errorf("failed to cast to string from %s", v.Kind())
}

func castBool(v reflect.Value) (bool, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v.Int() {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
		return false, fmt.Errorf("failed to cast to bool from %d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch v.Uint() {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
		return false, fmt.Errorf("failed to cast to bool from %d", v.Uint())
	case reflect.String:
		return strconv.ParseBool(v.String())
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Float32, reflect.Float64:
		switch v.Float() {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
		return false, fmt.Errorf("failed to cast to bool from %f", v.Float())
	}
	return false, fmt.Errorf("failed to cast to bool from %s", v.Kind())
}

func castFloat(v reflect.Value) (float64, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(v.Uint()), nil
	case reflect.String:
		return strconv.ParseFloat(v.String(), 64)
	case reflect.Bool:
		if v.Bool() {
			return 1, nil
		}
		return 0, nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	}
	return 0, fmt.Errorf("failed to cast to float64 from %s", v.Kind())
}

func assignArray(dst, v reflect.Value, seen *visits) error {
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return fmt.Errorf("failed to cast to array from %s", v.Kind())
	}
	if dst.Len() != v.Len() {
		return fmt.Errorf("failed to cast [%d]array from slice of %d length", dst.Len(), v.Len())
	}
	ret := reflect.New(dst.Type()).Elem()
	for i := 0; i < v.Len(); i++ {
		if err := assignValue(ret.Index(i), v.Index(i), seen); err != nil {
			return err
		}
	}
	dst.Set(ret)
	return nil
}

func assignSlice(dst, v reflect.Value, seen *visits) error {
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return fmt.Errorf("failed to cast to slice from %s", v.Kind())
	}
	ret := reflect.MakeSlice(dst.Type(), v.Len(), v.Len())
	for i := 0; i < v.Len(); i++ {
		if err := assignValue(ret.Index(i), v.Index(i), seen); err != nil {
			return err
		}
	}
	dst.Set(ret)
	return nil
}

func assignMap(dst, v reflect.Value, seen *visits) error {
	switch v.Kind() {
	case reflect.Map:
		t := dst.Type()
		ret := reflect.MakeMap(t)
		// SetMapIndex copies the key and the value, and assignValue sets all of them, so they are reused
		key := reflect.New(t.Key()).Elem()
		value := reflect.New(t.Elem()).Elem()
		iter := v.MapRange()
		for iter.Next() {
			if err := assignValue(key, iter.Key(), seen); err != nil {
				return err
			}
			if err := assignValue(value, iter.Value(), seen); err != nil {
				return err
			}
			ret.SetMapIndex(key, value)
		}
		dst.Set(ret)
		return nil
	case reflect.Slice:
		if v.Len() > 0 {
			return assignValue(dst, v.Index(0), seen)
		}
		return fmt.Errorf("failed to cast to map from empty slice")
	}
	return fmt.Errorf("failed to cast to map from %s", v.Kind())
}

func assignStruct(dst, v reflect.Value, seen *visits) error {
	switch v.Kind() {
	case reflect.Map:
		ret := reflect.New(dst.Type()).Elem()
		name := reflect.New(stringType).Elem()
		iter := v.MapRange()
		for iter.Next() {
			if err := assignValue(name, iter.Key(), seen); err != nil {
				return err
			}
			if err := setStructField(ret, name.String(), iter.Value(), seen); err != nil {
				return err
			}
		}
		dst.Set(ret)
		return nil
	case reflect.Struct:
		ret := reflect.New(dst.Type()).Elem()
		typ := v.Type()
		for i := 0; i < typ.NumField(); i++ {
			if !typ.Field(i).IsExported() {
				// the value of an unexported field cannot be read
				continue
			}
			if err := setStructField(ret, typ.Field(i).Name, v.Field(i), seen); err != nil {
				return err
			}
		}
		dst.Set(ret)
		return nil
	case reflect.Slice:
		if v.Len() > 0 {
			return assignValue(dst, v.Index(0), seen)
		}
		return fmt.Errorf("failed to cast to struct from empty slice")
	}
	return fmt.Errorf("failed to cast to struct from %s", v.Kind())
}

// setStructField sets v, cast, to the field of the struct ret which has the name, if ret has one which can be set:
// an exported field, which is not promoted through a pointer to an unexported struct type. A struct which is embedded
// by a pointer of an exported type on the way to the field is allocated.
func setStructField(ret reflect.Value, name string, v reflect.Value, seen *visits) error {
	field, ok := ret.Type().FieldByName(name)
	if !ok || !field.IsExported() {
		return nil
	}
	dst := ret
	for i, x := range field.Index {
		if i > 0 && dst.Kind() == reflect.Ptr {
			if dst.IsNil() {
				if !dst.CanSet() {
					return nil
				}
				dst.Set(reflect.New(dst.Type().Elem()))
			}
			dst = dst.Elem()
		}
		dst = dst.Field(x)
	}
	if !dst.CanSet() {
		return nil
	}
	return assignValue(dst, v, seen)
}
