// Package encoding provides utilities for encoding and decoding data objects.
package encoding

import "errors"

var (
	magic = []byte("THOR")
)

const (
	fileFormatVersion    = 0x1
	streamsFormatVersion = 0x1
	logsFormatVersion    = 0x1
)

var (
	ErrElementExist = errors.New("open element already exists")
	ErrClosed       = errors.New("element is closed")

	// errElementNoExist is used when a child element tries to notify its parent
	// of it closing but the parent doesn't have a child open. This would
	// indicate a bug in the encoder so it's not exposed to callers.
	errElementNoExist = errors.New("open element does not exist")
)
