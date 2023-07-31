/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2023 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"hash/crc32"
	"io"
	"math/bits"
)

// ChecksumType contains information about the checksum type.
type ChecksumType uint32

const (

	// ChecksumSHA256 indicates a SHA256 checksum.
	ChecksumSHA256 ChecksumType = 1 << iota
	// ChecksumSHA1 indicates a SHA-1 checksum.
	ChecksumSHA1
	// ChecksumCRC32 indicates a CRC32 checksum with IEEE table.
	ChecksumCRC32
	// ChecksumCRC32C indicates a CRC32 checksum with Castagnoli table.
	ChecksumCRC32C

	// Keep after all valid checksums
	checksumLast

	// checksumMask is a mask for valid checksum types.
	checksumMask = checksumLast - 1

	// ChecksumNone indicates no checksum.
	ChecksumNone ChecksumType = 0

	amzChecksumAlgo   = "x-amz-checksum-algorithm"
	amzChecksumCRC32  = "x-amz-checksum-crc32"
	amzChecksumCRC32C = "x-amz-checksum-crc32c"
	amzChecksumSHA1   = "x-amz-checksum-sha1"
	amzChecksumSHA256 = "x-amz-checksum-sha256"
)

// Is returns if c is all of t.
func (c ChecksumType) Is(t ChecksumType) bool {
	return c&t == t
}

// Key returns the header key.
// returns empty string if invalid or none.
func (c ChecksumType) Key() string {
	switch c & checksumMask {
	case ChecksumCRC32:
		return amzChecksumCRC32
	case ChecksumCRC32C:
		return amzChecksumCRC32C
	case ChecksumSHA1:
		return amzChecksumSHA1
	case ChecksumSHA256:
		return amzChecksumSHA256
	}
	return ""
}

// RawByteLen returns the size of the un-encoded checksum.
func (c ChecksumType) RawByteLen() int {
	switch c & checksumMask {
	case ChecksumCRC32, ChecksumCRC32C:
		return 4
	case ChecksumSHA1:
		return sha1.Size
	case ChecksumSHA256:
		return sha256.Size
	}
	return 0
}

// Hasher returns a hasher corresponding to the checksum type.
// Returns nil if no checksum.
func (c ChecksumType) Hasher() hash.Hash {
	switch c & checksumMask {
	case ChecksumCRC32:
		return crc32.NewIEEE()
	case ChecksumCRC32C:
		return crc32.New(crc32.MakeTable(crc32.Castagnoli))
	case ChecksumSHA1:
		return sha1.New()
	case ChecksumSHA256:
		return sha256.New()
	}
	return nil
}

// IsSet returns whether the type is valid and known.
func (c ChecksumType) IsSet() bool {
	return bits.OnesCount32(uint32(c)) == 1
}

// String returns the type as a string.
// CRC32, CRC32C, SHA1, and SHA256 for valid values.
// Empty string for unset and "<invalid>" if not valid.
func (c ChecksumType) String() string {
	switch c & checksumMask {
	case ChecksumCRC32:
		return "CRC32"
	case ChecksumCRC32C:
		return "CRC32C"
	case ChecksumSHA1:
		return "SHA1"
	case ChecksumSHA256:
		return "SHA256"
	case ChecksumNone:
		return ""
	}
	return "<invalid>"
}

// ChecksumReader reads all of r and returns a checksum of type c.
// Returns any error that may have occurred while reading.
func (c ChecksumType) ChecksumReader(r io.Reader) (Checksum, error) {
	h := c.Hasher()
	if h == nil {
		return Checksum{}, nil
	}
	_, err := io.Copy(h, r)
	if err != nil {
		return Checksum{}, err
	}
	return NewChecksum(c, h.Sum(nil)), nil
}

// ChecksumBytes returns a checksum of the content b with type c.
func (c ChecksumType) ChecksumBytes(b []byte) Checksum {
	h := c.Hasher()
	if h == nil {
		return Checksum{}
	}
	n, err := h.Write(b)
	if err != nil || n != len(b) {
		// Shouldn't happen with these checksummers.
		return Checksum{}
	}
	return NewChecksum(c, h.Sum(nil))
}

// Checksum is a type and encoded value.
type Checksum struct {
	Type ChecksumType
	r    []byte
}

// NewChecksum sets the checksum to the value of b,
// which is the raw hash output.
// If the length of c does not match t.RawByteLen,
// a checksum with ChecksumNone is returned.
func NewChecksum(t ChecksumType, b []byte) Checksum {
	if t.IsSet() && len(b) == t.RawByteLen() {
		return Checksum{Type: t, r: b}
	}
	return Checksum{}
}

// NewChecksumString sets the checksum to the value of s,
// which is the base 64 encoded raw hash output.
// If the length of c does not match t.RawByteLen, it is not added.
func NewChecksumString(t ChecksumType, s string) Checksum {
	b, _ := base64.StdEncoding.DecodeString(s)
	if t.IsSet() && len(b) == t.RawByteLen() {
		return Checksum{Type: t, r: b}
	}
	return Checksum{}
}

// IsSet returns whether the checksum is valid and known.
func (c Checksum) IsSet() bool {
	return c.Type.IsSet() && len(c.r) == c.Type.RawByteLen()
}

// Encoded returns the encoded value.
// Returns the empty string if not set or valid.
func (c Checksum) Encoded() string {
	if !c.IsSet() {
		return ""
	}
	return base64.StdEncoding.EncodeToString(c.r)
}

// Raw returns the raw checksum value if set.
func (c Checksum) Raw() []byte {
	if !c.IsSet() {
		return nil
	}
	return c.r
}
