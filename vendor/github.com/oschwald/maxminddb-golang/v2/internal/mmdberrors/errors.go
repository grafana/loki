// Package mmdberrors is an internal package for the errors used in
// this module.
package mmdberrors

import (
	"fmt"
	"reflect"
)

// InvalidDatabaseError is returned when the database contains invalid data
// and cannot be parsed.
type InvalidDatabaseError struct {
	message string
}

// NewOffsetError creates an [InvalidDatabaseError] indicating that an offset
// pointed beyond the end of the database.
func NewOffsetError() InvalidDatabaseError {
	return InvalidDatabaseError{"unexpected end of database"}
}

// NewInvalidDatabaseError creates an [InvalidDatabaseError] using the
// provided format and format arguments.
func NewInvalidDatabaseError(format string, args ...any) InvalidDatabaseError {
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

// NewUnmarshalTypeStrError creates an [UnmarshalTypeError] when the string
// value cannot be assigned to a value of rType.
func NewUnmarshalTypeStrError(value string, rType reflect.Type) UnmarshalTypeError {
	return UnmarshalTypeError{
		Type:  rType,
		Value: value,
	}
}

// NewUnmarshalTypeError creates an [UnmarshalTypeError] when the value
// cannot be assigned to a value of rType.
func NewUnmarshalTypeError(value any, rType reflect.Type) UnmarshalTypeError {
	return NewUnmarshalTypeStrError(fmt.Sprintf("%v (%T)", value, value), rType)
}

func (e UnmarshalTypeError) Error() string {
	return fmt.Sprintf("maxminddb: cannot unmarshal %s into type %s", e.Value, e.Type)
}
