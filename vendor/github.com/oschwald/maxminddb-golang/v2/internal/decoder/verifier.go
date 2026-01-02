package decoder

import (
	"reflect"

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
