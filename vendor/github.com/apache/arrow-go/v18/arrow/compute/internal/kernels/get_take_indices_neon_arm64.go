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
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

//go:build go1.18 && arm64 && !noasm && !appengine

package kernels

import (
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"golang.org/x/sys/cpu"
)

var takeIndicesUint32NeonPositions = makeTakeIndicesUint32NeonPositions()

var takeIndicesUint32NeonCounts = [16]uint8{
	0, 1, 1, 2, 1, 2, 2, 3,
	1, 2, 2, 3, 2, 3, 3, 4,
}

func makeTakeIndicesUint32NeonPositions() (positions [16][4]uint32) {
	for mask := 0; mask < len(positions); mask++ {
		n := 0
		for bit := 0; bit < 4; bit++ {
			if mask&(1<<uint(bit)) != 0 {
				positions[mask][n] = uint32(bit)
				n++
			}
		}
	}
	return
}

//go:noescape
func _getTakeIndicesUint32NEON(filter, output, positions, counts unsafe.Pointer, nbytes, tailMask int64)

func getTakeIndicesUint32NEON(mem memory.Allocator, filter *exec.ArraySpan) (arrow.ArrayData, bool) {
	if !cpu.ARM64.HasASIMD || filter.MayHaveNulls() || filter.Offset%8 != 0 || filter.Len < 64 {
		return nil, false
	}

	filterData := filter.Buffers[1].Buf
	byteOffset := filter.Offset / 8
	nbytes := (filter.Len + 7) / 8
	if byteOffset < 0 || byteOffset+nbytes > int64(len(filterData)) {
		return nil, false
	}

	// VisitSetBitRuns is especially effective for long runs, so only use the
	// compactor when a short sample shows enough fragmented bytes to amortize
	// its setup cost.
	const (
		sampleBytes = 256
		minMixed    = 4
	)
	mixed := 0
	for i := int64(0); i < nbytes && i < sampleBytes; i++ {
		mask := filterData[byteOffset+i]
		if mask != 0 && mask != 0xff {
			mixed++
		}
	}
	if mixed < minMixed {
		return nil, false
	}

	length := int64(bitutil.CountSetBits(filterData, int(filter.Offset), int(filter.Len)))
	if length == 0 {
		return array.NewData(arrow.PrimitiveTypes.Uint32, 0, []*memory.Buffer{nil, memory.NewBufferBytes(nil)}, nil, 0, 0), true
	}

	outputBuf := memory.NewBufferWithAllocator(mem.Allocate(int(length*4)), mem)
	defer outputBuf.Release()
	output := arrow.GetData[uint32](outputBuf.Bytes())
	tailMask := int64(0xff)
	if tailBits := filter.Len & 7; tailBits != 0 {
		tailMask = int64((uint64(1) << uint(tailBits)) - 1)
	}
	_getTakeIndicesUint32NEON(
		unsafe.Pointer(unsafe.SliceData(filterData[byteOffset:])),
		unsafe.Pointer(unsafe.SliceData(output)),
		unsafe.Pointer(unsafe.SliceData(takeIndicesUint32NeonPositions[:])),
		unsafe.Pointer(unsafe.SliceData(takeIndicesUint32NeonCounts[:])),
		nbytes,
		tailMask,
	)
	return array.NewData(arrow.PrimitiveTypes.Uint32, int(length), []*memory.Buffer{nil, outputBuf}, nil, 0, 0), true
}
