package decoder

import (
	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// errorContext provides zero-allocation error context tracking for Decoder.
// This is only used when an error occurs, ensuring no performance impact
// on the happy path.
type errorContext struct {
	path *mmdberrors.PathBuilder // Only allocated when needed
}

// BuildPath implements mmdberrors.ErrorContextTracker.
// This is only called when an error occurs, so allocation is acceptable.
func (e *errorContext) BuildPath() string {
	if e.path == nil {
		return "" // No path tracking enabled
	}
	return e.path.Build()
}

// wrapError wraps an error with context information when an error occurs.
// Zero allocation on happy path - only allocates when error != nil.
func (d *Decoder) wrapError(err error) error {
	if err == nil {
		return nil
	}
	// Only wrap with context when an error actually occurs
	return mmdberrors.WrapWithContext(err, d.offset, nil)
}

// wrapErrorAtOffset wraps an error with context at a specific offset.
// Used when the error occurs at a different offset than the decoder's current position.
func (*Decoder) wrapErrorAtOffset(err error, offset uint) error {
	if err == nil {
		return nil
	}
	return mmdberrors.WrapWithContext(err, offset, nil)
}

// Example of how to integrate into existing decoder methods:
// Instead of:
//   return mmdberrors.NewInvalidDatabaseError("message")
// Use:
//   return d.wrapError(mmdberrors.NewInvalidDatabaseError("message"))
//
// This adds zero overhead when no error occurs, but provides rich context
// when errors do happen.
