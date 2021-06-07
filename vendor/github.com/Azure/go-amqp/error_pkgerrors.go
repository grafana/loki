// +build pkgerrors

package amqp

import "github.com/pkg/errors"

// Error functions used only when built with "-tags pkgerrors".
var (
	errorNew    = errors.New
	errorErrorf = errors.Errorf
	errorWrapf  = errors.Wrapf
)
