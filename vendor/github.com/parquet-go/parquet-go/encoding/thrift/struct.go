package thrift

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Flags controls encoding/decoding behavior.
type Flags int16

// Flag constants for struct field handling.
const (
	Enum      Flags = 1 << 0
	Union     Flags = 1 << 1
	Required  Flags = 1 << 2
	Optional  Flags = 1 << 3
	Strict    Flags = 1 << 4
	WriteZero Flags = 1 << 5
	ValueType Flags = 1 << 6 // indicates field implements Value interface

	// Internal protocol feature flags
	featuresBitOffset  = 8
	useDeltaEncoding   = Flags(UseDeltaEncoding) << featuresBitOffset
	coalesceBoolFields = Flags(CoalesceBoolFields) << featuresBitOffset

	decodeFlags   Flags = Strict | protocolFlags
	protocolFlags Flags = useDeltaEncoding | coalesceBoolFields
)

// Have returns true if f has all the bits in x.
func (f Flags) Have(x Flags) bool {
	return (f & x) == x
}

// Only returns only the bits in f that are also in x.
func (f Flags) Only(x Flags) Flags {
	return f & x
}

// With returns f with the bits in x set.
func (f Flags) With(x Flags) Flags {
	return f | x
}

// Without returns f with the bits in x cleared.
func (f Flags) Without(x Flags) Flags {
	return f & ^x
}

type structField struct {
	typ   reflect.Type
	index []int
	id    int16
	flags Flags
}

func forEachStructField(t reflect.Type, index []int, do func(structField)) {
	for i, n := 0, t.NumField(); i < n; i++ {
		f := t.Field(i)

		if f.PkgPath != "" && !f.Anonymous { // unexported
			continue
		}

		fieldIndex := append(index, i)
		fieldIndex = fieldIndex[:len(fieldIndex):len(fieldIndex)]

		if f.Anonymous {
			fieldType := f.Type

			for fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}

			if fieldType.Kind() == reflect.Struct {
				forEachStructField(fieldType, fieldIndex, do)
				continue
			}
		}

		tag := f.Tag.Get("thrift")
		if tag == "" {
			continue
		}
		tags := strings.Split(tag, ",")
		flags := Flags(0)

		for _, opt := range tags[1:] {
			switch opt {
			case "enum":
				flags = flags.With(Enum)
			case "union":
				flags = flags.With(Union)
			case "required":
				flags = flags.With(Required)
			case "optional":
				flags = flags.With(Optional)
			case "writezero":
				flags = flags.With(WriteZero)
			default:
				panic(fmt.Errorf("thrift struct field contains an unknown tag option %q in `thrift:\"%s\"`", opt, tag))
			}
		}

		if flags.Have(Optional | Required) {
			panic(fmt.Errorf("thrift struct field cannot be both optional and required in `thrift:\"%s\"`", tag))
		}

		if flags.Have(Union) {
			if f.Type.Kind() != reflect.Interface {
				panic(fmt.Errorf("thrift union tag found on a field which is not an interface type `thrift:\"%s\"`", tag))
			}

			if tags[0] != "" {
				panic(fmt.Errorf("invalid thrift field id on union field `thrift:\"%s\"`", tag))
			}

			do(structField{
				typ:   f.Type,
				index: fieldIndex,
				flags: flags,
			})
		} else {
			if flags.Have(Enum) {
				switch f.Type.Kind() {
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				default:
					panic(fmt.Errorf("thrift enum tag found on a field which is not an integer type `thrift:\"%s\"`", tag))
				}
			}

			// Detect types implementing Value interface
			if f.Type.Implements(valueType) {
				flags = flags.With(ValueType)
			}

			if id, err := strconv.ParseInt(tags[0], 10, 16); err != nil {
				panic(fmt.Errorf("invalid thrift field id found in struct tag `thrift:\"%s\"`: %w", tag, err))
			} else if id <= 0 {
				panic(fmt.Errorf("invalid thrift field id found in struct tag `thrift:\"%s\"`: %d <= 0", tag, id))
			} else {
				do(structField{
					typ:   f.Type,
					index: fieldIndex,
					id:    int16(id),
					flags: flags,
				})
			}
		}
	}
}
