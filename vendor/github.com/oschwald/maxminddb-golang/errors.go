package maxminddb

import (
	"fmt"
	"reflect"
)

// InvalidDatabaseError is returned when the database contains invalid data
// and cannot be parsed.
type InvalidDatabaseError struct {
	message string
}

func newOffsetError() InvalidDatabaseError {
	return InvalidDatabaseError{"unexpected end of database"}
}

func newInvalidDatabaseError(format string, args ...any) InvalidDatabaseError {
	return InvalidDatabaseError{fmt.Sprintf(format, args...)}
}

func (e InvalidDatabaseError) Error() string {
	return e.message
}

// UnmarshalTypeError is returned when the value in the database cannot be
// assigned to the specified data type.
type UnmarshalTypeError struct {
	Type  reflect.Type
	Value string
}

func newUnmarshalTypeError(value any, rType reflect.Type) UnmarshalTypeError {
	return UnmarshalTypeError{
		Value: fmt.Sprintf("%v", value),
		Type:  rType,
	}
}

func (e UnmarshalTypeError) Error() string {
	return fmt.Sprintf("maxminddb: cannot unmarshal %s into type %s", e.Value, e.Type.String())
}
