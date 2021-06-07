// +build !pkgerrors

package amqp

import (
	"errors"
	"fmt"
)

// Default stdlib-based error functions.
var (
	errorNew    = errors.New
	errorErrorf = fmt.Errorf
	errorWrapf  = func(err error, _ string, _ ...interface{}) error { return err }
)
