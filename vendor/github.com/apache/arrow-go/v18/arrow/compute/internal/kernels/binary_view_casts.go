// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build go1.18

package kernels

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
)

func validateUtf8View(input *exec.ArraySpan) error {
	views := arrow.ViewHeaderTraits.CastFromBytes(input.Buffers[1].Buf)
	dataBuffers := make([][]byte, 0, len(input.Buffers)-2)
	for i := 2; i < len(input.Buffers); i++ {
		dataBuffers = append(dataBuffers, input.Buffers[i].Buf)
	}
	return validateUTF8Sequence(input.Buffers[0].Buf, input.Offset, input.Len,
		func(pos int64) []byte {
			h := &views[input.Offset+pos]
			if h.IsInline() {
				return h.InlineBytes()
			}
			off := h.BufferOffset()
			return dataBuffers[h.BufferIndex()][off : off+int32(h.Len())]
		})
}

func unsafeStringBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

type binaryAppender struct {
	bldr        array.Builder
	appendBytes func([]byte)
	reserveData func(int)
}

func newBinaryAppender(bldr array.Builder) (binaryAppender, error) {
	switch b := bldr.(type) {
	case *array.BinaryBuilder:
		return binaryAppender{bldr: b, appendBytes: b.Append, reserveData: b.ReserveData}, nil
	case *array.StringBuilder:
		return binaryAppender{bldr: b, appendBytes: b.BinaryBuilder.Append, reserveData: b.ReserveData}, nil
	case *array.LargeStringBuilder:
		return binaryAppender{bldr: b, appendBytes: b.BinaryBuilder.Append, reserveData: b.ReserveData}, nil
	case *array.BinaryViewBuilder:
		return binaryAppender{bldr: b, appendBytes: b.Append, reserveData: b.ReserveData}, nil
	case *array.StringViewBuilder:
		return binaryAppender{bldr: b, appendBytes: b.BinaryViewBuilder.Append, reserveData: b.ReserveData}, nil
	default:
		return binaryAppender{}, fmt.Errorf("%w: unsupported builder type %T for binary-like output",
			arrow.ErrNotImplemented, bldr)
	}
}

// binaryLikeValueAccessor returns a per-index byte slice accessor for any
// binary-like arrow array. Shared by CastBinaryToBinaryView and
// CastBinaryViewToBinary so the input-type switch has one source of truth.
func binaryLikeValueAccessor(arr arrow.Array) (func(int) []byte, error) {
	switch a := arr.(type) {
	case *array.Binary:
		return a.Value, nil
	case *array.LargeBinary:
		return a.Value, nil
	case *array.String:
		return func(i int) []byte { return unsafeStringBytes(a.Value(i)) }, nil
	case *array.LargeString:
		return func(i int) []byte { return unsafeStringBytes(a.Value(i)) }, nil
	case *array.FixedSizeBinary:
		return a.Value, nil
	case *array.BinaryView:
		return a.Value, nil
	case *array.StringView:
		return func(i int) []byte { return unsafeStringBytes(a.Value(i)) }, nil
	default:
		return nil, fmt.Errorf("%w: unsupported binary-like type: %s",
			arrow.ErrNotImplemented, arr.DataType())
	}
}

// appendBinaryValues drives the null-preserving append loop shared by
// both directions of the binary<->view cast kernels.
func appendBinaryValues(arr arrow.Array, getVal func(int) []byte, ba binaryAppender) {
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			ba.bldr.AppendNull()
			continue
		}
		ba.appendBytes(getVal(i))
	}
}

// SumOutOfLineBytes returns the total non-inline payload (in bytes) that
// would be written to a view array's overflow buffer when iterating n
// positions with the given predicates, and arrow.ErrInvalid if the total
// exceeds the single-overflow-buffer limit (math.MaxInt32). Used both by
// the binary-to-view cast kernel here and by the cast meta function's
// view-coalescing helpers (where the closures translate dictionary or
// chunk positions into value-level lookups).
func SumOutOfLineBytes(n int, isNull func(int) bool, valueLen func(int) int) (int64, error) {
	var total int64
	for i := 0; i < n; i++ {
		if isNull(i) {
			continue
		}
		vlen := valueLen(i)
		if !arrow.IsViewInline(vlen) {
			total += int64(vlen)
			if total > math.MaxInt32 {
				return 0, fmt.Errorf("%w: view out-of-line payload (%d bytes) exceeds single-buffer limit (%d bytes)",
					arrow.ErrInvalid, total, math.MaxInt32)
			}
		}
	}
	return total, nil
}

// CastBinaryToBinaryView casts a Binary, LargeBinary, String, LargeString,
// or FixedSizeBinary array into a BinaryView or StringView array. When the
// source is a non-utf8 binary type and the destination is a utf8 view type,
// every non-null element is validated as UTF-8 unless
// CastOptions.AllowInvalidUtf8 is set.
func CastBinaryToBinaryView(ctx *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	opts := ctx.State.(CastState)
	input := &batch.Values[0].Array
	outputType := out.Type.(arrow.BinaryDataType)

	if shouldValidateUTF8(input.Type, outputType, opts.AllowInvalidUtf8) {
		switch input.Type.ID() {
		case arrow.BINARY:
			if err := validateUtf8[int32](input); err != nil {
				return err
			}
		case arrow.LARGE_BINARY:
			if err := validateUtf8[int64](input); err != nil {
				return err
			}
		case arrow.FIXED_SIZE_BINARY:
			if err := validateUtf8Fsb(input); err != nil {
				return err
			}
		}
	}

	rawBldr := array.NewBuilder(exec.GetAllocator(ctx.Ctx), out.Type)
	defer rawBldr.Release()
	rawBldr.Reserve(int(input.Len))

	ba, err := newBinaryAppender(rawBldr)
	if err != nil {
		return err
	}

	arr := input.MakeArray()
	defer arr.Release()

	getVal, err := binaryLikeValueAccessor(arr)
	if err != nil {
		return fmt.Errorf("%w: unsupported input type for cast to %s: %s",
			arrow.ErrNotImplemented, out.Type, input.Type)
	}

	// Pre-size the out-of-line data buffer to the total required capacity so
	// the builder allocates a single overflow block. ArraySpan only has three
	// buffer slots (bitmap + view headers + one data buffer); if we let the
	// builder spill into a second block, TakeOwnership would index past
	// Buffers[2] and panic.
	outOfLineTotal, err := SumOutOfLineBytes(arr.Len(), arr.IsNull,
		func(i int) int { return len(getVal(i)) })
	if err != nil {
		return fmt.Errorf("cast from %s to %s: %w", input.Type, out.Type, err)
	}
	if outOfLineTotal > 0 {
		ba.reserveData(int(outOfLineTotal))
	}

	appendBinaryValues(arr, getVal, ba)

	result := ba.bldr.NewArray()
	defer result.Release()
	out.TakeOwnership(result.Data())
	return nil
}

// CastBinaryViewToBinary casts a BinaryView or StringView array into a
// Binary, LargeBinary, String, or LargeString array, materializing the
// referenced byte ranges into a single contiguous data buffer. UTF-8
// validation is performed when casting from a non-utf8 view into a utf8
// destination unless CastOptions.AllowInvalidUtf8 is set.
func CastBinaryViewToBinary[OutOffsetT int32 | int64](ctx *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	opts := ctx.State.(CastState)
	input := &batch.Values[0].Array
	inputType := input.Type.(arrow.BinaryDataType)
	outputType := out.Type.(arrow.BinaryDataType)

	if shouldValidateUTF8(inputType, outputType, opts.AllowInvalidUtf8) {
		if err := validateUtf8View(input); err != nil {
			return err
		}
	}

	rawBldr := array.NewBuilder(exec.GetAllocator(ctx.Ctx), out.Type)
	defer rawBldr.Release()
	rawBldr.Reserve(int(input.Len))

	ba, err := newBinaryAppender(rawBldr)
	if err != nil {
		return err
	}

	arr := input.MakeArray()
	defer arr.Release()

	getVal, err := binaryLikeValueAccessor(arr)
	if err != nil {
		return fmt.Errorf("%w: unsupported input type for view-to-binary cast: %s",
			arrow.ErrNotImplemented, input.Type)
	}

	var totalBytes int64
	for i := 0; i < arr.Len(); i++ {
		if !arr.IsNull(i) {
			totalBytes += int64(len(getVal(i)))
		}
	}
	if totalBytes > int64(MaxOf[OutOffsetT]()) {
		return fmt.Errorf("%w: failed casting from %s to %s: input array too large",
			arrow.ErrInvalid, input.Type, out.Type)
	}

	appendBinaryValues(arr, getVal, ba)

	result := ba.bldr.NewArray()
	defer result.Release()
	out.TakeOwnership(result.Data())
	return nil
}

// CastBinaryViewToBinaryView handles casts between BinaryView and StringView
// (and same-type view casts). The cast is zero-copy; UTF-8 validation is
// performed when casting from a binary_view into a string_view unless
// CastOptions.AllowInvalidUtf8 is set.
func CastBinaryViewToBinaryView(ctx *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	opts := ctx.State.(CastState)
	input := &batch.Values[0].Array
	inputType := input.Type.(arrow.BinaryDataType)
	outputType := out.Type.(arrow.BinaryDataType)

	if shouldValidateUTF8(inputType, outputType, opts.AllowInvalidUtf8) {
		if err := validateUtf8View(input); err != nil {
			return err
		}
	}

	return ZeroCopyCastExec(ctx, batch, out)
}
