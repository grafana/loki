// Copyright (c) 2025 Minio Inc. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

// Package crc64nvme implements the 64-bit cyclic redundancy check with NVME polynomial.
package crc64nvme

import (
	"encoding/binary"
	"errors"
	"hash"
	"sync"
	"unsafe"
)

const (
	// The size of a CRC-64 checksum in bytes.
	Size = 8

	// The NVME polynoimial (reversed, as used by Go)
	NVME = 0x9a6c9329ac4bc9b5
)

var (
	// precalculated table.
	nvmeTable = makeTable(NVME)
)

// table is a 256-word table representing the polynomial for efficient processing.
type table [256]uint64

var (
	slicing8TablesBuildOnce sync.Once
	slicing8TableNVME       *[8]table
)

func buildSlicing8TablesOnce() {
	slicing8TablesBuildOnce.Do(buildSlicing8Tables)
}

func buildSlicing8Tables() {
	slicing8TableNVME = makeSlicingBy8Table(makeTable(NVME))
}

func makeTable(poly uint64) *table {
	t := new(table)
	for i := 0; i < 256; i++ {
		crc := uint64(i)
		for j := 0; j < 8; j++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ poly
			} else {
				crc >>= 1
			}
		}
		t[i] = crc
	}
	return t
}

func makeSlicingBy8Table(t *table) *[8]table {
	var helperTable [8]table
	helperTable[0] = *t
	for i := 0; i < 256; i++ {
		crc := t[i]
		for j := 1; j < 8; j++ {
			crc = t[crc&0xff] ^ (crc >> 8)
			helperTable[j][i] = crc
		}
	}
	return &helperTable
}

// digest represents the partial evaluation of a checksum.
type digest struct {
	crc uint64
}

// New creates a new hash.Hash64 computing the CRC-64 checksum using the
// NVME polynomial. Its Sum method will lay the
// value out in big-endian byte order. The returned Hash64 also
// implements [encoding.BinaryMarshaler] and [encoding.BinaryUnmarshaler] to
// marshal and unmarshal the internal state of the hash.
func New() hash.Hash64 { return &digest{0} }

func (d *digest) Size() int { return Size }

func (d *digest) BlockSize() int { return 1 }

func (d *digest) Reset() { d.crc = 0 }

const (
	magic         = "crc\x02"
	marshaledSize = len(magic) + 8 + 8
)

func (d *digest) MarshalBinary() ([]byte, error) {
	b := make([]byte, 0, marshaledSize)
	b = append(b, magic...)
	b = binary.BigEndian.AppendUint64(b, tableSum)
	b = binary.BigEndian.AppendUint64(b, d.crc)
	return b, nil
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic) || string(b[:len(magic)]) != magic {
		return errors.New("hash/crc64: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("hash/crc64: invalid hash state size")
	}
	if tableSum != binary.BigEndian.Uint64(b[4:]) {
		return errors.New("hash/crc64: tables do not match")
	}
	d.crc = binary.BigEndian.Uint64(b[12:])
	return nil
}

func update(crc uint64, p []byte) uint64 {
	if hasAsm && len(p) > 127 {
		ptr := unsafe.Pointer(&p[0])
		if align := (uintptr(ptr)+15)&^0xf - uintptr(ptr); align > 0 {
			// Align to 16-byte boundary.
			crc = update(crc, p[:align])
			p = p[align:]
		}
		runs := len(p) / 128
		crc = updateAsm(crc, p[:128*runs])
		return update(crc, p[128*runs:])
	}

	buildSlicing8TablesOnce()
	crc = ^crc
	// table comparison is somewhat expensive, so avoid it for small sizes
	for len(p) >= 64 {
		var helperTable = slicing8TableNVME
		// Update using slicing-by-8
		for len(p) > 8 {
			crc ^= binary.LittleEndian.Uint64(p)
			crc = helperTable[7][crc&0xff] ^
				helperTable[6][(crc>>8)&0xff] ^
				helperTable[5][(crc>>16)&0xff] ^
				helperTable[4][(crc>>24)&0xff] ^
				helperTable[3][(crc>>32)&0xff] ^
				helperTable[2][(crc>>40)&0xff] ^
				helperTable[1][(crc>>48)&0xff] ^
				helperTable[0][crc>>56]
			p = p[8:]
		}
	}
	// For reminders or small sizes
	for _, v := range p {
		crc = nvmeTable[byte(crc)^v] ^ (crc >> 8)
	}
	return ^crc
}

// Update returns the result of adding the bytes in p to the crc.
func Update(crc uint64, p []byte) uint64 {
	return update(crc, p)
}

func (d *digest) Write(p []byte) (n int, err error) {
	d.crc = update(d.crc, p)
	return len(p), nil
}

func (d *digest) Sum64() uint64 { return d.crc }

func (d *digest) Sum(in []byte) []byte {
	s := d.Sum64()
	return append(in, byte(s>>56), byte(s>>48), byte(s>>40), byte(s>>32), byte(s>>24), byte(s>>16), byte(s>>8), byte(s))
}

// Checksum returns the CRC-64 checksum of data
// using the NVME polynomial.
func Checksum(data []byte) uint64 { return update(0, data) }

// ISO tablesum of NVME poly
const tableSum = 0x8ddd9ee4402c7163
