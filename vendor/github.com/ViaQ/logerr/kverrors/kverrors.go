package kverrors

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ViaQ/logerr/internal/kv"
)

// Keys used to log specific builtin fields
const (
	MessageKey string = "msg"
	CauseKey   string = "cause"
)

// New creates a new KVError with keys and values
func New(msg string, keysAndValues ...interface{}) error {
	keysAndValues = append([]interface{}{MessageKey, msg}, keysAndValues...)
	return &KVError{kv: kv.ToMap(keysAndValues...)}
}

// NewCtx creates a new error with Context
func NewCtx(msg string, ctx Context, keysAndValues ...interface{}) error {
	return New(msg, append(keysAndValues, ctx...)...)
}

// Wrap wraps an error as a new error with keys and values
func Wrap(err error, msg string, keysAndValues ...interface{}) error {
	if err == nil {
		return nil
	}
	e := New(msg, append(keysAndValues, []interface{}{CauseKey, err}...)...)
	return e
}

// KVError is an error that contains structured keys and values
type KVError struct {
	kv map[string]interface{}
}

// KVs returns the key/value pairs associated with this error if it is a *KVError
func KVs(err error) map[string]interface{} {
	var kve *KVError
	if errors.As(err, &kve) {
		return kve.kv
	}
	return nil
}

// KVSlice returns the key/value pairs associated with this error
// as a slice
func KVSlice(err error) []interface{} {
	var kve *KVError
	if !errors.As(err, &kve) {
		return nil
	}
	s := make([]interface{}, 0, len(kve.kv)*2)
	for k, v := range kve.kv {
		s = append(s, k, v)
	}
	return s
}

// Unwrap returns the error that caused this error. This is required
// to work with the standard library errors.Unwrap
func (e *KVError) Unwrap() error {
	if cause, ok := e.kv[CauseKey]; ok {
		e, _ := cause.(error)
		// if ok is false then e will be empty anyway so no need to check if ok
		return e
	}
	return nil
}

// Error returns the string formatted error message. This is required
// to function as a standard library error
func (e *KVError) Error() string {
	base := e.Unwrap()
	if base != nil {
		return fmt.Sprintf("%s: %s", Message(e), base.Error())
	}
	return Message(e)
}

// Message returns the raw error message
func Message(err error) string {
	var kve *KVError
	if !errors.As(err, &kve) {
		return err.Error()
	}
	msg := kve.kv[MessageKey]
	return fmt.Sprint(msg)
}

// Add adds key/value pairs to an error and returns the error
// WARNING: The original error is modified with this operation
func Add(err error, keyValuePairs ...interface{}) error {
	var kve *KVError
	if !errors.As(err, &kve) {
		return New(err.Error(), keyValuePairs...)
	}
	for k, v := range kv.ToMap(keyValuePairs...) {
		kve.kv[k] = v
	}
	return kve
}

// MarshalJSON implements json.Marshaler
func (e *KVError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.kv)
}

// AddCtx appends Context to the error
func AddCtx(err error, ctx Context) error {
	return Add(err, ctx...)
}

// Unwrap provides compatibility with the standard errors package
func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// NewContext creates key/value pairs to be used with errors later in
// the callstack. This provides the ability to create contextual
// information that will be used with any returned error
//
// Example:
//   errCtx := kverrors.Context("cluster", clusterName,
//       "namespace", namespace)
//
//   ...
//
//   if err != nil {
//       return kverrors.Wrap(err, "failed to get namespace").Ctx(errCtx)
//   }
//
//   ...
//
//   if err != nil {
//       return kverrors.Wrap(err, "failed to update cluster").Ctx(errCtx)
//   }
func NewContext(keysAndValues ...interface{}) Context {
	return keysAndValues
}

// Context is keyValuePairs wrapped to use later. Usage of this
// directly is not necessary
// See Context for more information
type Context []interface{}

// New creates a new KVError with this context
func (c Context) New(msg string, keysAndValues ...interface{}) error {
	return New(msg, append(keysAndValues, c...)...)
}

// Wrap wraps an error with this context
func (c Context) Wrap(err error, msg string, keysAndValues ...interface{}) error {
	return Wrap(err, msg, append(keysAndValues, c...)...)
}

// Root unwraps the error until it reaches the root error
func Root(err error) error {
	root := err
	for next := Unwrap(root); next != nil; next = Unwrap(root) {
		root = next
	}
	return root
}
