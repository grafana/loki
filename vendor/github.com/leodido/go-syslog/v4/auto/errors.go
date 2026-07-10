package auto

import "fmt"

// ParseError is returned when auto-detection fails to parse a message.
// It carries the raw input bytes so callers can log or route unparsed
// messages instead of silently dropping them.
type ParseError struct {
	// Err is the underlying parse error from the last parser attempted.
	Err error
	// RawMessage is the original input bytes that could not be parsed.
	RawMessage []byte
}

// Error returns the error string.
func (e *ParseError) Error() string {
	return fmt.Sprintf("auto-detect parse failed: %v", e.Err)
}

// Unwrap returns the underlying parse error.
func (e *ParseError) Unwrap() error {
	return e.Err
}
