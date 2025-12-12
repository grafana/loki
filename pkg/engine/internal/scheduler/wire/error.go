package wire

import (
	"fmt"
)

// Error represents an error received in response to a message.
type Error struct {
	// Code is an HTTP status code representing the kind of error received.
	Code int32

	// Message is a human-readable description of the error.
	Message string
}

var _ error = (*Error)(nil)

// Errorf creates a new Error with the given code and formatted message.
func Errorf(code int32, format string, args ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Is returns true if the target is identical to the error, providing
// functionality for [errors.Is].
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok {
		return false
	}
	return other.Code == e.Code && other.Message == e.Message
}

// Error returns the message of the error.
func (e *Error) Error() string { return e.Message }
