package columnar

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/memory"
)

// Concat concatenates the input arrays into a single, combined array using the
// provided allocator. Concat returns an error if not all arrays are of the same
// kind.
func Concat(alloc *memory.Allocator, in []Array) (Array, error) {
	if len(in) == 0 {
		return nil, nil
	}

	kind := in[0].Kind()
	for _, arr := range in[1:] {
		if want, got := kind, arr.Kind(); want != got {
			return nil, fmt.Errorf("concat expected array of %s, but got %s", want, got)
		}
	}

	switch kind {
	case KindNull:
		return concatNull(alloc, in)
	case KindBool:
		return concatBool(alloc, in)
	case KindInt64:
		return concatNumber[int64](alloc, in)
	case KindUint64:
		return concatNumber[uint64](alloc, in)
	case KindUTF8:
		return concatUTF8(alloc, in)
	default:
		return nil, fmt.Errorf("unsupported array kind %s", kind)
	}
}

func concatNull(alloc *memory.Allocator, in []Array) (Array, error) {
	totalLen := getTotalLen(in)
	validity := memory.NewBitmap(alloc, totalLen)
	validity.AppendCount(false, totalLen)
	return NewNull(validity), nil
}

func getTotalLen(in []Array) int {
	var total int
	for _, arr := range in {
		total += arr.Len()
	}
	return total
}

func concatBool(alloc *memory.Allocator, in []Array) (Array, error) {
	totalLen := getTotalLen(in)

	var (
		validity = memory.NewBitmap(alloc, totalLen)
		values   = memory.NewBitmap(alloc, totalLen)
	)

	for _, arr := range in {
		if arr.Len() == 0 {
			continue
		}

		appendValidityBitmap(&validity, arr)
		values.AppendBitmap(arr.(*Bool).Values())
	}

	cleanValidity(&validity)
	return NewBool(values, validity), nil
}

// appendValidityBitmap appends the validity bitmap of srcArray into dst. If
// srcArray lacks a validity bitmap (because it has no nulls), srcArray.Len()
// true values are appended into dst.
func appendValidityBitmap(dst *memory.Bitmap, srcArray Array) {
	srcValidity := srcArray.Validity()
	if srcValidity.Len() == 0 {
		// The validity bitmap does not exist; append all true values up to the
		// size of the source array.
		dst.AppendCount(true, srcArray.Len())
	}
	dst.AppendBitmap(srcValidity)
}

func cleanValidity(validity *memory.Bitmap) {
	if validity.ClearCount() == 0 {
		// Drop the bitmap; it has no nulls.
		*validity = memory.Bitmap{}
	}
}

func concatNumber[T Numeric](alloc *memory.Allocator, in []Array) (Array, error) {
	totalLen := getTotalLen(in)

	var (
		validity = memory.NewBitmap(alloc, totalLen)
		values   = memory.NewBuffer[T](alloc, totalLen)
	)

	for _, arr := range in {
		if arr.Len() == 0 {
			continue
		}

		appendValidityBitmap(&validity, arr)
		values.Append(arr.(*Number[T]).Values()...)
	}

	cleanValidity(&validity)
	return NewNumber[T](values.Data(), validity), nil
}

func concatUTF8(alloc *memory.Allocator, in []Array) (Array, error) {
	totalLen := getTotalLen(in)

	var totalDataLen int
	for _, arr := range in {
		totalDataLen += len(arr.(*UTF8).Data())
	}

	var (
		validity   = memory.NewBitmap(alloc, totalLen)
		offsetsBuf = memory.NewBuffer[int32](alloc, totalLen+1) // +1 for first offset
		data       = memory.NewBuffer[byte](alloc, totalDataLen)
	)

	offsetsBuf.Resize(totalLen + 1)

	var lastOffset int32
	var numOffsets int

	offsets := offsetsBuf.Data()
	offsets[numOffsets] = 0
	numOffsets++

	for _, arr := range in {
		if arr.Len() == 0 {
			continue
		}

		appendValidityBitmap(&validity, arr)

		utf8Array := arr.(*UTF8)
		data.Append(utf8Array.Data()...)

		// Appending arrOffsets requires more caution, since all arrOffsets need to be
		// adjusted based on the most recent offset.
		arrOffsets := utf8Array.Offsets()

		// We don't want to append the first offset from an array; it's always
		// the last offset we already added. Appending the first offset would
		// cause an incorrect total offset count.
		//
		// We do need to at least temporarily store the first offset to
		// determine relative offsets for the concatenated offsets, though.
		arrStartOffset := arrOffsets[0]

		for _, off := range arrOffsets[1:] {
			// Determine relative offset to start, then shift by the
			// accumulative offset.
			actualOff := lastOffset + (off - arrStartOffset)

			// NOTE(rfratto): There's a 4% increase in read speed by manually
			// modifying the slice rather than appending via the buffer.
			//
			// TODO(rfratto): Consider updating memory.Buffer to make appends
			// faster to avoid needing to do this hack all over the hot path (like
			// dataset page decoders).
			offsets[numOffsets] = actualOff
			numOffsets++
		}

		lastOffset = offsets[numOffsets-1]
	}

	cleanValidity(&validity)
	return NewUTF8(data.Data(), offsets, validity), nil
}
