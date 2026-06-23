package testutils

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"time"

	"github.com/oklog/ulid/v2"
)

// Filler walks a reflect.Value and assigns every settable field a distinct
// non-zero value, recursing into composite kinds. Interface fields and
// concrete types with semantic constraints (valid enum values, unexported
// fields, discriminated unions) are handled via per-type registries that the
// caller sets up before invoking Fill.
//
// Filler is the shared engine for tests that detect "added a struct field but
// forgot to copy it in marshal/unmarshal" silent bugs without requiring the
// test author to enumerate every field.
type Filler struct {
	counter int

	interfaceCtors  map[reflect.Type]func(*Filler) reflect.Value
	specificFillers map[reflect.Type]func(*Filler) reflect.Value
}

// NewFiller returns a Filler with built-in handlers for time.Time,
// time.Duration, and ulid.ULID (deterministic non-zero values, no monotonic
// clock, stable location). Callers add further overrides via
// RegisterInterface, RegisterSpecific, or RegisterFixed before calling Fill.
func NewFiller() *Filler {
	f := &Filler{
		interfaceCtors:  map[reflect.Type]func(*Filler) reflect.Value{},
		specificFillers: map[reflect.Type]func(*Filler) reflect.Value{},
	}
	f.RegisterSpecific(reflect.TypeOf(time.Time{}), func(f *Filler) reflect.Value {
		return reflect.ValueOf(time.Unix(int64(f.Next())*1000, 0).UTC())
	})
	f.RegisterSpecific(reflect.TypeOf(time.Duration(0)), func(f *Filler) reflect.Value {
		return reflect.ValueOf(time.Duration(f.Next()) * time.Millisecond)
	})
	f.RegisterSpecific(reflect.TypeOf(ulid.ULID{}), func(f *Filler) reflect.Value {
		var u ulid.ULID
		binary.BigEndian.PutUint64(u[:8], uint64(f.Next()))
		binary.BigEndian.PutUint64(u[8:], uint64(f.Next()))
		return reflect.ValueOf(u)
	})
	return f
}

// RegisterInterface adds (or replaces) a constructor for an interface type.
// The returned reflect.Value must implement iface and contain non-zero data.
func (f *Filler) RegisterInterface(iface reflect.Type, ctor func(*Filler) reflect.Value) {
	f.interfaceCtors[iface] = ctor
}

// RegisterSpecific adds (or replaces) a constructor for a concrete type that
// has semantic constraints (e.g. valid enum values, discriminated unions,
// unexported fields that reflection cannot set).
func (f *Filler) RegisterSpecific(t reflect.Type, ctor func(*Filler) reflect.Value) {
	f.specificFillers[t] = ctor
}

// RegisterFixed is a convenience for RegisterSpecific when the type should
// always be filled with the same value (typical for enum-like types where any
// valid non-zero value will do).
func (f *Filler) RegisterFixed(v any) {
	rv := reflect.ValueOf(v)
	f.specificFillers[rv.Type()] = func(*Filler) reflect.Value { return rv }
}

// Next returns the next counter value. Custom registered fillers can call
// this to keep their values distinct across invocations.
func (f *Filler) Next() int {
	f.counter++
	return f.counter
}

// Fill populates v with non-zero data, applying registered overrides where
// available and recursing into composite types.
func (f *Filler) Fill(v reflect.Value) {
	if !v.CanSet() {
		return
	}
	t := v.Type()
	if c, ok := f.specificFillers[t]; ok {
		v.Set(c(f))
		return
	}
	switch t.Kind() {
	case reflect.Interface:
		c, ok := f.interfaceCtors[t]
		if !ok {
			panic(fmt.Sprintf("Filler: no constructor registered for interface %s", t))
		}
		v.Set(c(f))
	case reflect.Pointer:
		ptr := reflect.New(t.Elem())
		f.Fill(ptr.Elem())
		v.Set(ptr)
	case reflect.Slice:
		const n = 2
		s := reflect.MakeSlice(t, n, n)
		for i := 0; i < n; i++ {
			f.Fill(s.Index(i))
		}
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(t)
		for i := 0; i < 2; i++ {
			k := reflect.New(t.Key()).Elem()
			f.Fill(k)
			val := reflect.New(t.Elem()).Elem()
			f.Fill(val)
			m.SetMapIndex(k, val)
		}
		v.Set(m)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			f.Fill(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.CanSet() {
				continue
			}
			f.Fill(field)
		}
	case reflect.String:
		v.SetString(fmt.Sprintf("s%d", f.Next()))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(f.Next()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(f.Next()))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(f.Next()))
	default:
		panic(fmt.Sprintf("Filler: unsupported kind %s for type %s", t.Kind(), t))
	}
}
