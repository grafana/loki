package defaults

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"
)

var (
	errInvalidType = errors.New("not a struct pointer")
)

const (
	fieldName = "default"
)

// Set initializes members in a struct referenced by a pointer.
// Maps and slices are initialized by `make` and other primitive types are set with default values.
// `ptr` should be a struct pointer
func Set(ptr interface{}) error {
	if reflect.TypeOf(ptr).Kind() != reflect.Ptr {
		return errInvalidType
	}

	v := reflect.ValueOf(ptr).Elem()
	t := v.Type()

	if t.Kind() != reflect.Struct {
		return errInvalidType
	}

	for i := 0; i < t.NumField(); i++ {
		if defaultVal := t.Field(i).Tag.Get(fieldName); defaultVal != "-" {
			if err := setField(v.Field(i), defaultVal); err != nil {
				return err
			}
		}
	}
	callSetter(ptr)
	return nil
}

// MustSet function is a wrapper of Set function
// It will call Set and panic if err not equals nil.
func MustSet(ptr interface{}) {
	if err := Set(ptr); err != nil {
		panic(err)
	}
}

func setField(field reflect.Value, defaultVal string) error {
	if !field.CanSet() {
		return nil
	}

	if !shouldInitializeField(field, defaultVal) {
		return nil
	}

	if isInitialValue(field) {
		switch field.Kind() {
		case reflect.Bool:
			if val, err := strconv.ParseBool(defaultVal); err == nil {
				field.Set(reflect.ValueOf(val).Convert(field.Type()))
			}
		case reflect.Int:
			if val, err := strconv.ParseInt(defaultVal, 0, strconv.IntSize); err == nil {
				field.Set(reflect.ValueOf(int(val)).Convert(field.Type()))
			}
		case reflect.Int8:
			if val, err := strconv.ParseInt(defaultVal, 0, 8); err == nil {
				field.Set(reflect.ValueOf(int8(val)).Convert(field.Type()))
			}
		case reflect.Int16:
			if val, err := strconv.ParseInt(defaultVal, 0, 16); err == nil {
				field.Set(reflect.ValueOf(int16(val)).Convert(field.Type()))
			}
		case reflect.Int32:
			if val, err := strconv.ParseInt(defaultVal, 0, 32); err == nil {
				field.Set(reflect.ValueOf(int32(val)).Convert(field.Type()))
			}
		case reflect.Int64:
			if val, err := time.ParseDuration(defaultVal); err == nil {
				field.Set(reflect.ValueOf(val).Convert(field.Type()))
			} else if val, err := strconv.ParseInt(defaultVal, 0, 64); err == nil {
				field.Set(reflect.ValueOf(val).Convert(field.Type()))
			}
		case reflect.Uint:
			if val, err := strconv.ParseUint(defaultVal, 0, strconv.IntSize); err == nil {
				field.Set(reflect.ValueOf(uint(val)).Convert(field.Type()))
			}
		case reflect.Uint8:
			if val, err := strconv.ParseUint(defaultVal, 0, 8); err == nil {
				field.Set(reflect.ValueOf(uint8(val)).Convert(field.Type()))
			}
		case reflect.Uint16:
			if val, err := strconv.ParseUint(defaultVal, 0, 16); err == nil {
				field.Set(reflect.ValueOf(uint16(val)).Convert(field.Type()))
			}
		case reflect.Uint32:
			if val, err := strconv.ParseUint(defaultVal, 0, 32); err == nil {
				field.Set(reflect.ValueOf(uint32(val)).Convert(field.Type()))
			}
		case reflect.Uint64:
			if val, err := strconv.ParseUint(defaultVal, 0, 64); err == nil {
				field.Set(reflect.ValueOf(val).Convert(field.Type()))
			}
		case reflect.Uintptr:
			if val, err := strconv.ParseUint(defaultVal, 0, strconv.IntSize); err == nil {
				field.Set(reflect.ValueOf(uintptr(val)).Convert(field.Type()))
			}
		case reflect.Float32:
			if val, err := strconv.ParseFloat(defaultVal, 32); err == nil {
				field.Set(reflect.ValueOf(float32(val)).Convert(field.Type()))
			}
		case reflect.Float64:
			if val, err := strconv.ParseFloat(defaultVal, 64); err == nil {
				field.Set(reflect.ValueOf(val).Convert(field.Type()))
			}
		case reflect.String:
			field.Set(reflect.ValueOf(defaultVal).Convert(field.Type()))

		case reflect.Slice:
			ref := reflect.New(field.Type())
			ref.Elem().Set(reflect.MakeSlice(field.Type(), 0, 0))
			if defaultVal != "" && defaultVal != "[]" {
				if err := json.Unmarshal([]byte(defaultVal), ref.Interface()); err != nil {
					return err
				}
			}
			field.Set(ref.Elem().Convert(field.Type()))
		case reflect.Map:
			ref := reflect.New(field.Type())
			ref.Elem().Set(reflect.MakeMap(field.Type()))
			if defaultVal != "" && defaultVal != "{}" {
				if err := json.Unmarshal([]byte(defaultVal), ref.Interface()); err != nil {
					return err
				}
			}
			field.Set(ref.Elem().Convert(field.Type()))
		case reflect.Struct:
			if defaultVal != "" && defaultVal != "{}" {
				if err := json.Unmarshal([]byte(defaultVal), field.Addr().Interface()); err != nil {
					return err
				}
			}
		case reflect.Ptr:
			field.Set(reflect.New(field.Type().Elem()))
		}
	}

	switch field.Kind() {
	case reflect.Ptr:
		setField(field.Elem(), defaultVal)
		callSetter(field.Interface())
	case reflect.Struct:
		if err := Set(field.Addr().Interface()); err != nil {
			return err
		}
	case reflect.Slice:
		for j := 0; j < field.Len(); j++ {
			if err := setField(field.Index(j), defaultVal); err != nil {
				return err
			}
		}
	}

	return nil
}

func isInitialValue(field reflect.Value) bool {
	return reflect.DeepEqual(reflect.Zero(field.Type()).Interface(), field.Interface())
}

func shouldInitializeField(field reflect.Value, tag string) bool {
	switch field.Kind() {
	case reflect.Struct:
		return true
	case reflect.Ptr:
		if !field.IsNil() && field.Elem().Kind() == reflect.Struct {
			return true
		}
	case reflect.Slice:
		return field.Len() > 0 || tag != ""
	}

	return tag != ""
}

// CanUpdate returns true when the given value is an initial value of its type
func CanUpdate(v interface{}) bool {
	return isInitialValue(reflect.ValueOf(v))
}
