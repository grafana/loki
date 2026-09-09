package decoder

import (
	"reflect"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// VerifyDataSection verifies the data section against the provided
// offsets from the tree.
func (d *ReflectionDecoder) VerifyDataSection(offsets map[uint]bool) error {
	pointerCount := len(offsets)

	var offset uint
	bufferLen := uint(len(d.buffer))
	for offset < bufferLen {
		var data any
		rv := reflect.ValueOf(&data)
		newOffset, err := d.decode(offset, rv, 0)
		if err != nil {
			return mmdberrors.NewInvalidDatabaseError(
				"received decoding error (%v) at offset of %v",
				err,
				offset,
			)
		}
		if err := validateUTF8(data); err != nil {
			return mmdberrors.NewInvalidDatabaseError(
				"received validation error (%v) at offset of %v",
				err,
				offset,
			)
		}
		if newOffset <= offset {
			return mmdberrors.NewInvalidDatabaseError(
				"data section offset unexpectedly went from %v to %v",
				offset,
				newOffset,
			)
		}

		pointer := offset

		if _, ok := offsets[pointer]; !ok {
			return mmdberrors.NewInvalidDatabaseError(
				"found data (%v) at %v that the search tree does not point to",
				data,
				pointer,
			)
		}
		delete(offsets, pointer)

		offset = newOffset
	}

	if offset != bufferLen {
		return mmdberrors.NewInvalidDatabaseError(
			"unexpected data at the end of the data section (last offset: %v, end: %v)",
			offset,
			bufferLen,
		)
	}

	if len(offsets) != 0 {
		return mmdberrors.NewInvalidDatabaseError(
			"found %v pointers (of %v) in the search tree that we did not see in the data section",
			len(offsets),
			pointerCount,
		)
	}
	return nil
}

func validateUTF8(data any) error {
	switch value := data.(type) {
	case string:
		if !utf8.ValidString(value) {
			return mmdberrors.NewInvalidDatabaseError("invalid UTF-8 string")
		}
	case map[string]any:
		for key, item := range value {
			if !utf8.ValidString(key) {
				return mmdberrors.NewInvalidDatabaseError("invalid UTF-8 map key")
			}
			if err := validateUTF8(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range value {
			if err := validateUTF8(item); err != nil {
				return err
			}
		}
	default:
		// Non-string scalar values require no UTF-8 validation.
	}
	return nil
}
