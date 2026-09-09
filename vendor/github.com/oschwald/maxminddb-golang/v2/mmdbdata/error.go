package mmdbdata

import (
	"reflect"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// NewUnmarshalTypeError reports that value cannot be represented by T. It is
// intended for generated and handwritten decoders that perform conversions.
func NewUnmarshalTypeError[T any](value any) error {
	return mmdberrors.NewUnmarshalTypeError(value, reflect.TypeFor[T]())
}

// NormalizeUnmarshalError converts a direct cursor kind mismatch to the same
// public UnmarshalTypeError category used by reflection decoding into T. It
// preserves direct offset context and leaves nested error chains unchanged.
func NormalizeUnmarshalError[T any](err error) error {
	switch direct := err.(type) { //nolint:errorlint // Only direct mismatches are normalized.
	case UnexpectedKindError:
		return NewUnmarshalTypeError[T](direct.Actual)
	case mmdberrors.ContextualError:
		mismatch, ok := direct.Err.(UnexpectedKindError) //nolint:errorlint // Preserve nested chains.
		if !ok {
			return err
		}
		direct.Err = NewUnmarshalTypeError[T](mismatch.Actual)
		return direct
	default:
		return err
	}
}
