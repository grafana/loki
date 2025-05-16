// Package encoding provides utilities for encoding and decoding data objects.
package encoding

import "errors"

var (
	ErrElementExist = errors.New("open element already exists")
	ErrClosed       = errors.New("element is closed")
)
