package errors

import "errors"

var (
	// ErrNonSingularValue and ErrUnsupportedMutation are the errors of a MarshalJSONTo or UnmarshalJSONFrom method
	// which the root package calls, which wrote or read other than one value, or which declined after it wrote or
	// read, as encoding/json/v2 reports them.
	ErrNonSingularValue    = errors.New("must read or write exactly one value")
	ErrUnsupportedMutation = errors.New("unsupported calls must not read or write any tokens")
)

// MethodCallResult returns the error of a call of MarshalJSONTo or UnmarshalJSONFrom with an encoder or a decoder of
// its own, which returned err, after which the stack of the encoder or the decoder is at the depth with n values at
// its top level, and whether the method declined: it returned errors.ErrUnsupported and wrote or read nothing.
func MethodCallResult(err error, depth int, n int64) (bool, error) {
	switch {
	case err == nil && (depth != 0 || n != 1):
		return false, ErrNonSingularValue
	case err != nil && errors.Is(err, errors.ErrUnsupported):
		if depth == 0 && n == 0 {
			return true, nil
		}
		return false, ErrUnsupportedMutation
	}
	return false, err
}
