package util

import (
	"errors"
)

// ErrStopProcess is the error returned by a service as a hint to stop the server entirely.
var ErrStopProcess = errors.New("stop process")
