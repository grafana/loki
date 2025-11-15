/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package gocql

import (
	"hash/crc32"
)

var (
	// Initial CRC32 bytes: 0xFA, 0x2D, 0x55, 0xCA
	initialCRC32Bytes = []byte{0xfa, 0x2d, 0x55, 0xca}
)

// Crc32 calculates the CRC32 checksum of the given byte slice.
func Crc32(b []byte) uint32 {
	crc := crc32.NewIEEE()
	crc.Write(initialCRC32Bytes) // Include initial CRC32 bytes
	crc.Write(b)
	return crc.Sum32()
}

const (
	crc24Init = 0x875060  // Initial value for CRC24 calculation
	crc24Poly = 0x1974F0B // Polynomial for CRC24 calculation
)

// Crc24 calculates the CRC24 checksum using the Koopman polynomial.
func Crc24(buf []byte) uint32 {
	crc := crc24Init
	for _, b := range buf {
		crc ^= int(b) << 16

		for i := 0; i < 8; i++ {
			crc <<= 1
			if crc&0x1000000 != 0 {
				crc ^= crc24Poly
			}
		}
	}

	return uint32(crc)
}
