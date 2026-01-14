package errors

import "errors"

var (
	ErrIndex          = errors.New("index error")
	ErrKey            = errors.New("key error")
	ErrType           = errors.New("type error")
	ErrNotImplemented = errors.New("not implemented")
)
