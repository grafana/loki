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

//go:build go1.18 && amd64 && !noasm && !appengine

package kernels

import (
	"encoding/binary"
	"math/bits"
	"unsafe"

	"golang.org/x/sys/cpu"
)

var filterUint32Tables = makeFilterUint32Tables()

func makeFilterUint32Tables() (tables [352]byte) {
	for mask := 0; mask < 16; mask++ {
		pos := 0
		for lane := 0; lane < 4; lane++ {
			if mask&(1<<uint(lane)) == 0 {
				continue
			}
			for byteInLane := 0; byteInLane < 4; byteInLane++ {
				tables[mask*16+pos] = byte(lane*4 + byteInLane)
				pos++
			}
		}
		for ; pos < 16; pos++ {
			tables[mask*16+pos] = 0x80
		}
	}

	for count := 1; count <= 4; count++ {
		for lane := 0; lane < count; lane++ {
			binary.LittleEndian.PutUint32(tables[256+count*16+lane*4:], ^uint32(0))
		}
	}
	for mask := 0; mask < 16; mask++ {
		tables[336+mask] = byte(bits.OnesCount8(uint8(mask)))
	}
	return tables
}

//go:noescape
func _filter_uint32_avx2(values, filter, output, tables unsafe.Pointer, length int64)

func filterUint32Avx2(values []uint32, output []uint32, filterData []byte, filterOffset, length int64) bool {
	if !cpu.X86.HasAVX2 || length < 64 || length%8 != 0 || filterOffset%8 != 0 {
		return false
	}

	numBytes := length / 8
	filterByteOffset := filterOffset / 8
	if filterByteOffset < 0 || filterByteOffset+numBytes > int64(len(filterData)) {
		return false
	}

	mixedBytes := 0
	const sampleBytes = 64
	for i := int64(0); i < numBytes && i < sampleBytes; i++ {
		mask := filterData[filterByteOffset+i]
		if mask != 0 && mask != 0xff {
			mixedBytes++
			if mixedBytes == 4 {
				break
			}
		}
	}
	if mixedBytes < 4 {
		return false
	}

	if len(output) == 0 {
		return false
	}
	_filter_uint32_avx2(
		unsafe.Pointer(&values[0]),
		unsafe.Pointer(&filterData[filterByteOffset]),
		unsafe.Pointer(&output[0]),
		unsafe.Pointer(&filterUint32Tables[0]),
		length,
	)
	return true
}
