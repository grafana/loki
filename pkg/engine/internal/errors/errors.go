package errors

import "errors"

var (
	ErrIndex          = errors.New("index error")
	ErrKey            = errors.New("key error")
	ErrType           = errors.New("type error")
	ErrNotImplemented = errors.New("not implemented")

	// ErrNotSupported indicates the new query engine cannot handle the query.
	// It is re-exported as engine.ErrNotSupported. The HTTP handler branches
	// on it via errors.Is to return HTTP 501.
	ErrNotSupported = errors.New("feature not supported in new query engine")
)
