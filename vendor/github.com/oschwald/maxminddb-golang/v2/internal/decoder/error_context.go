package decoder

import "github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"

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
