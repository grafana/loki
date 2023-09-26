// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import "fmt"

// ErrInvalidUpdate is returned whenever Update is called against an instance
// but an invalid field is changed between configs. If ErrInvalidUpdate is
// returned, the instance must be fully stopped and replaced with a new one
// with the new config.
type ErrInvalidUpdate struct {
	Inner error
}

// Error implements the error interface.
func (e ErrInvalidUpdate) Error() string { return e.Inner.Error() }

// Is returns true if err is an ErrInvalidUpdate.
func (e ErrInvalidUpdate) Is(err error) bool {
	switch err.(type) {
	case ErrInvalidUpdate, *ErrInvalidUpdate:
		return true
	default:
		return false
	}
}

// As will set the err object to ErrInvalidUpdate provided err
// is a pointer to ErrInvalidUpdate.
func (e ErrInvalidUpdate) As(err interface{}) bool {
	switch v := err.(type) {
	case *ErrInvalidUpdate:
		*v = e
	default:
		return false
	}
	return true
}

// errImmutableField is the error describing a field that cannot be changed. It
// is wrapped inside of a ErrInvalidUpdate.
type errImmutableField struct{ Field string }

func (e errImmutableField) Error() string {
	return fmt.Sprintf("%s cannot be changed dynamically", e.Field)
}
