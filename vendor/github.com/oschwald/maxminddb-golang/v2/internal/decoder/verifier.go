package decoder

import (
	"reflect"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

// VerifyMetadata validates the metadata section. The section starts with the
// metadata map. Values that the map points to can follow it, so the rest of
// the section must be a sequence of valid values. All values in the section
// are decoded under one budget, so pointers cannot multiply the work. Each
// pointer must point to the start of a field.
func VerifyMetadata(buffer []byte) error {
	d := NewWithoutStringCache(buffer)
	var metadata any
	rv := addressableValue{Value: reflect.ValueOf(&metadata).Elem()}
	bounded := newBudgetedDecoder(&d)
	offset, err := bounded.decodeValue(0, rv, 0)
	if err != nil {
		return wrapRootDecodeError(err, 0)
	}
	if err := validateUTF8(metadata); err != nil {
		return err
	}
	bufferLen := uint(len(buffer))
	for offset < bufferLen {
		var value any
		rv := addressableValue{Value: reflect.ValueOf(&value).Elem()}
		next, err := bounded.decodeValue(offset, rv, 0)
		if err == nil {
			err = validateUTF8(value)
		}
		if err != nil {
			return mmdberrors.NewInvalidDatabaseError(
				"invalid value after the metadata map (%v) at offset of %v",
				err,
				offset,
			)
		}
		offset = next
	}
	_, err = d.verifyFieldPointers(bufferLen, "metadata")
	return err
}

// VerifyDataSection verifies the data section against the provided
// offsets from the tree. Each top-level value must be a search-tree target.
// A target can also be the start of a field nested in a top-level value.
func (d *ReflectionDecoder) VerifyDataSection(offsets map[uint]bool) error {
	pointerCount := len(offsets)

	var offset uint
	bufferLen := uint(len(d.buffer))
	for offset < bufferLen {
		var data any
		rv := addressableValue{Value: reflect.ValueOf(&data).Elem()}
		bounded := newBudgetedDecoder(d)
		newOffset, err := bounded.decodeValue(offset, rv, 0)
		if err != nil {
			return newDecodingErrorAt(err, offset)
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
		if _, ok := offsets[offset]; !ok {
			return mmdberrors.NewInvalidDatabaseError(
				"found data (%v) at %v that the search tree does not point to",
				data,
				offset,
			)
		}
		offset = newOffset
	}

	if offset != bufferLen {
		return mmdberrors.NewInvalidDatabaseError(
			"unexpected data at the end of the data section (last offset: %v, end: %v)",
			offset,
			bufferLen,
		)
	}

	starts, err := d.verifyFieldPointers(bufferLen, "data section")
	if err != nil {
		return err
	}
	// Report the lowest bad target, so that the error is the same on each run.
	var missing int
	bad := bufferLen
	for target := range offsets {
		if target >= bufferLen {
			// Reader.Verify cannot reach this, because resolveDataPointer
			// rejects a target at or past the end of the data section. It
			// guards direct calls.
			missing++
		} else if !starts.has(target) && target < bad {
			bad = target
		}
	}
	if bad != bufferLen {
		record, err := d.recordStart(bad)
		if err != nil {
			return err
		}
		return mmdberrors.NewInvalidDatabaseError(
			"search tree points into the middle of a field in the data record at %v (offset %v)",
			record,
			bad,
		)
	}
	if missing != 0 {
		return mmdberrors.NewInvalidDatabaseError(
			"found %v pointers (of %v) in the search tree that we did not see in the data section",
			missing,
			pointerCount,
		)
	}
	return nil
}

// recordStart returns the start of the top-level value that contains offset.
// The data section must already be verified. It runs only to report an error.
func (d *DataDecoder) recordStart(offset uint) (uint, error) {
	var start uint
	for {
		next, err := d.nextValueOffset(start, 1)
		if err != nil {
			return 0, newDecodingErrorAt(err, start)
		}
		if next > offset {
			return start, nil
		}
		start = next
	}
}

// fieldStarts is a bitset with one bit for each byte offset.
type fieldStarts []uint64

func (f fieldStarts) set(offset uint) {
	f[offset/64] |= 1 << (offset % 64)
}

func (f fieldStarts) has(offset uint) bool {
	return f[offset/64]&(1<<(offset%64)) != 0
}

// verifyFieldPointers returns the start offset of each field in [0, end) and
// checks that each pointer there points to the start of a field. The fields
// must already be verified. section names the section in errors. The first
// pass marks the field starts and checks each backward pointer. The MaxMind
// writers point only to data that they already wrote, so the second pass,
// which checks forward pointers, runs only if the first pass finds one.
func (d *DataDecoder) verifyFieldPointers(end uint, section string) (fieldStarts, error) {
	starts := make(fieldStarts, end/64+1)
	var forward bool
	err := d.forEachField(end, func(offset uint, isPointer bool, target uint) error {
		starts.set(offset)
		if !isPointer {
			return nil
		}
		if target > offset {
			forward = true
			return nil
		}
		if !starts.has(target) {
			return newPointerTargetError(section, offset, target)
		}
		return nil
	})
	if err != nil || !forward {
		return starts, err
	}
	err = d.forEachField(end, func(offset uint, isPointer bool, target uint) error {
		if isPointer && target > offset && (target >= end || !starts.has(target)) {
			return newPointerTargetError(section, offset, target)
		}
		return nil
	})
	return starts, err
}

func newPointerTargetError(section string, offset, target uint) error {
	return mmdberrors.NewInvalidDatabaseError(
		"%s pointer at offset %v does not point to the start of a field (offset %v)",
		section,
		offset,
		target,
	)
}

// forEachField calls visit for each field in [0, end), in order. A map or
// array header comes directly before its first child, so the walk also visits
// nested fields. For a pointer, isPointer is true and target is the offset
// that the pointer points to. The fields must already be verified. The rules
// for the offset of the next field must match decodeCtrlData and
// nextValueOffset.
func (d *DataDecoder) forEachField(
	end uint,
	visit func(offset uint, isPointer bool, target uint) error,
) error {
	for offset := uint(0); offset < end; {
		kind, size, next, err := d.decodeCtrlData(offset)
		if err != nil {
			return newDecodingErrorAt(err, offset)
		}
		var target uint
		switch kind {
		case KindPointer:
			target, next, err = d.decodePointer(size, next)
			if err != nil {
				return newDecodingErrorAt(err, offset)
			}
		case KindMap, KindSlice, KindBool:
			// Children follow the header, and a bool has no payload.
		default:
			next += size
		}
		if err := visit(offset, kind == KindPointer, target); err != nil {
			return err
		}
		offset = next
	}
	return nil
}

func newDecodingErrorAt(err error, offset uint) error {
	return mmdberrors.NewInvalidDatabaseError(
		"received decoding error (%v) at offset of %v",
		err,
		offset,
	)
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
