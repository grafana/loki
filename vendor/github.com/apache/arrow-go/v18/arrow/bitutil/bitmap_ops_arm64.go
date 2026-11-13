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

//go:build !noasm && !appengine
// +build !noasm,!appengine

package bitutil

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func _bitmap_aligned_and_neon(left, right, out unsafe.Pointer, length int64)

//go:noescape
func _bitmap_aligned_or_neon(left, right, out unsafe.Pointer, length int64)

//go:noescape
func _bitmap_aligned_and_not_neon(left, right, out unsafe.Pointer, length int64)

//go:noescape
func _bitmap_aligned_xor_neon(left, right, out unsafe.Pointer, length int64)

//go:noescape
func _bitmap_aligned_xnor_neon(left, right, out unsafe.Pointer, length int64)

func bitmapSlicesOverlap(left, right []byte) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}

	leftStart := uintptr(unsafe.Pointer(&left[0]))
	rightStart := uintptr(unsafe.Pointer(&right[0]))
	if leftStart < rightStart {
		return rightStart-leftStart < uintptr(len(left))
	}
	return leftStart-rightStart < uintptr(len(right))
}

func bitmapAlignedNEONHasOverlap(left, right, out []byte) bool {
	return bitmapSlicesOverlap(left, out) || bitmapSlicesOverlap(right, out)
}

func bitmapAlignedAndNEON(left, right, out []byte) {
	if bitmapAlignedNEONHasOverlap(left, right, out) {
		alignedBitAndGo(left, right, out)
		return
	}
	_bitmap_aligned_and_neon(unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), int64(len(out)))
}

func bitmapAlignedOrNEON(left, right, out []byte) {
	if bitmapAlignedNEONHasOverlap(left, right, out) {
		alignedBitOrGo(left, right, out)
		return
	}
	_bitmap_aligned_or_neon(unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), int64(len(out)))
}

func bitmapAlignedAndNotNEON(left, right, out []byte) {
	if bitmapAlignedNEONHasOverlap(left, right, out) {
		alignedBitAndNotGo(left, right, out)
		return
	}
	_bitmap_aligned_and_not_neon(unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), int64(len(out)))
}

func bitmapAlignedXorNEON(left, right, out []byte) {
	if bitmapAlignedNEONHasOverlap(left, right, out) {
		alignedBitXorGo(left, right, out)
		return
	}
	_bitmap_aligned_xor_neon(unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), int64(len(out)))
}

func bitmapAlignedXnorNEON(left, right, out []byte) {
	if bitmapAlignedNEONHasOverlap(left, right, out) {
		alignedBitXnorGo(left, right, out)
		return
	}
	_bitmap_aligned_xnor_neon(unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), int64(len(out)))
}

func init() {
	if cpu.ARM64.HasASIMD {
		bitAndOp.opAligned = bitmapAlignedAndNEON
		bitOrOp.opAligned = bitmapAlignedOrNEON
		bitAndNotOp.opAligned = bitmapAlignedAndNotNEON
		bitXorOp.opAligned = bitmapAlignedXorNEON
		bitXnorOp.opAligned = bitmapAlignedXnorNEON
	} else {
		bitAndOp.opAligned = alignedBitAndGo
		bitOrOp.opAligned = alignedBitOrGo
		bitAndNotOp.opAligned = alignedBitAndNotGo
		bitXorOp.opAligned = alignedBitXorGo
		bitXnorOp.opAligned = alignedBitXnorGo
	}
}
