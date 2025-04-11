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
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"io"
	"math/bits"
	"net/http"
	"sort"

	"github.com/minio/crc64nvme"
)

// ChecksumMode contains information about the checksum mode on the object
type ChecksumMode uint32

const (
	// ChecksumFullObjectMode Full object checksum `csumCombine(csum1, csum2...)...), csumN...)`
	ChecksumFullObjectMode ChecksumMode = 1 << iota

	// ChecksumCompositeMode Composite checksum `csum([csum1 + csum2 ... + csumN])`
	ChecksumCompositeMode

	// Keep after all valid checksums
	checksumLastMode

	// checksumModeMask is a mask for valid checksum mode types.
	checksumModeMask = checksumLastMode - 1
)

// Is returns if c is all of t.
func (c ChecksumMode) Is(t ChecksumMode) bool {
	return c&t == t
}

// Key returns the header key.
func (c ChecksumMode) Key() string {
	return amzChecksumMode
}

func (c ChecksumMode) String() string {
	switch c & checksumModeMask {
	case ChecksumFullObjectMode:
		return "FULL_OBJECT"
	case ChecksumCompositeMode:
		return "COMPOSITE"
	}
	return ""
}

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
	// ChecksumCRC64NVME indicates CRC64 with 0xad93d23594c93659 polynomial.
	ChecksumCRC64NVME

	// Keep after all valid checksums
	checksumLast

	// ChecksumFullObject is a modifier that can be used on CRC32 and CRC32C
	// to indicate full object checksums.
	ChecksumFullObject

	// checksumMask is a mask for valid checksum types.
	checksumMask = checksumLast - 1

	// ChecksumNone indicates no checksum.
	ChecksumNone ChecksumType = 0

	// ChecksumFullObjectCRC32 indicates full object CRC32
	ChecksumFullObjectCRC32 = ChecksumCRC32 | ChecksumFullObject

	// ChecksumFullObjectCRC32C indicates full object CRC32C
	ChecksumFullObjectCRC32C = ChecksumCRC32C | ChecksumFullObject

	amzChecksumAlgo      = "x-amz-checksum-algorithm"
	amzChecksumCRC32     = "x-amz-checksum-crc32"
	amzChecksumCRC32C    = "x-amz-checksum-crc32c"
	amzChecksumSHA1      = "x-amz-checksum-sha1"
	amzChecksumSHA256    = "x-amz-checksum-sha256"
	amzChecksumCRC64NVME = "x-amz-checksum-crc64nvme"
	amzChecksumMode      = "x-amz-checksum-type"
)

// Base returns the base type, without modifiers.
func (c ChecksumType) Base() ChecksumType {
	return c & checksumMask
}

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
	case ChecksumCRC64NVME:
		return amzChecksumCRC64NVME
	}
	return ""
}

// CanComposite will return if the checksum type can be used for composite multipart upload on AWS.
func (c ChecksumType) CanComposite() bool {
	switch c & checksumMask {
	case ChecksumSHA256, ChecksumSHA1, ChecksumCRC32, ChecksumCRC32C:
		return true
	}
	return false
}

// CanMergeCRC will return if the checksum type can be used for multipart upload on AWS.
func (c ChecksumType) CanMergeCRC() bool {
	switch c & checksumMask {
	case ChecksumCRC32, ChecksumCRC32C, ChecksumCRC64NVME:
		return true
	}
	return false
}

// FullObjectRequested will return if the checksum type indicates full object checksum was requested.
func (c ChecksumType) FullObjectRequested() bool {
	switch c & (ChecksumFullObject | checksumMask) {
	case ChecksumFullObjectCRC32C, ChecksumFullObjectCRC32, ChecksumCRC64NVME:
		return true
	}
	return false
}

// KeyCapitalized returns the capitalized key as used in HTTP headers.
func (c ChecksumType) KeyCapitalized() string {
	return http.CanonicalHeaderKey(c.Key())
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
	case ChecksumCRC64NVME:
		return crc64.Size
	}
	return 0
}

const crc64NVMEPolynomial = 0xad93d23594c93659

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
	case ChecksumCRC64NVME:
		return crc64nvme.New()
	}
	return nil
}

// IsSet returns whether the type is valid and known.
func (c ChecksumType) IsSet() bool {
	return bits.OnesCount32(uint32(c&checksumMask)) == 1
}

// SetDefault will set the checksum if not already set.
func (c *ChecksumType) SetDefault(t ChecksumType) {
	if !c.IsSet() {
		*c = t
	}
}

// EncodeToString the encoded hash value of the content provided in b.
func (c ChecksumType) EncodeToString(b []byte) string {
	if !c.IsSet() {
		return ""
	}
	h := c.Hasher()
	h.Write(b)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
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
	case ChecksumCRC64NVME:
		return "CRC64NVME"
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

// CompositeChecksum returns the composite checksum of all provided parts.
func (c ChecksumType) CompositeChecksum(p []ObjectPart) (*Checksum, error) {
	if !c.CanComposite() {
		return nil, errors.New("cannot do composite checksum")
	}
	sort.Slice(p, func(i, j int) bool {
		return p[i].PartNumber < p[j].PartNumber
	})
	c = c.Base()
	crcBytes := make([]byte, 0, len(p)*c.RawByteLen())
	for _, part := range p {
		pCrc, err := part.ChecksumRaw(c)
		if err != nil {
			return nil, err
		}
		crcBytes = append(crcBytes, pCrc...)
	}
	h := c.Hasher()
	h.Write(crcBytes)
	return &Checksum{Type: c, r: h.Sum(nil)}, nil
}

// FullObjectChecksum will return the full object checksum from provided parts.
func (c ChecksumType) FullObjectChecksum(p []ObjectPart) (*Checksum, error) {
	if !c.CanMergeCRC() {
		return nil, errors.New("cannot merge this checksum type")
	}
	c = c.Base()
	sort.Slice(p, func(i, j int) bool {
		return p[i].PartNumber < p[j].PartNumber
	})

	switch len(p) {
	case 0:
		return nil, errors.New("no parts given")
	case 1:
		check, err := p[0].ChecksumRaw(c)
		if err != nil {
			return nil, err
		}
		return &Checksum{
			Type: c,
			r:    check,
		}, nil
	}
	var merged uint32
	var merged64 uint64
	first, err := p[0].ChecksumRaw(c)
	if err != nil {
		return nil, err
	}
	sz := p[0].Size
	switch c {
	case ChecksumCRC32, ChecksumCRC32C:
		merged = binary.BigEndian.Uint32(first)
	case ChecksumCRC64NVME:
		merged64 = binary.BigEndian.Uint64(first)
	}

	poly32 := uint32(crc32.IEEE)
	if c.Is(ChecksumCRC32C) {
		poly32 = crc32.Castagnoli
	}
	for _, part := range p[1:] {
		if part.Size == 0 {
			continue
		}
		sz += part.Size
		pCrc, err := part.ChecksumRaw(c)
		if err != nil {
			return nil, err
		}
		switch c {
		case ChecksumCRC32, ChecksumCRC32C:
			merged = crc32Combine(poly32, merged, binary.BigEndian.Uint32(pCrc), part.Size)
		case ChecksumCRC64NVME:
			merged64 = crc64Combine(bits.Reverse64(crc64NVMEPolynomial), merged64, binary.BigEndian.Uint64(pCrc), part.Size)
		}
	}
	var tmp [8]byte
	switch c {
	case ChecksumCRC32, ChecksumCRC32C:
		binary.BigEndian.PutUint32(tmp[:], merged)
		return &Checksum{
			Type: c,
			r:    tmp[:4],
		}, nil
	case ChecksumCRC64NVME:
		binary.BigEndian.PutUint64(tmp[:], merged64)
		return &Checksum{
			Type: c,
			r:    tmp[:8],
		}, nil
	default:
		return nil, errors.New("unknown checksum type")
	}
}

func addAutoChecksumHeaders(opts *PutObjectOptions) {
	if opts.UserMetadata == nil {
		opts.UserMetadata = make(map[string]string, 1)
	}
	opts.UserMetadata["X-Amz-Checksum-Algorithm"] = opts.AutoChecksum.String()
	if opts.AutoChecksum.FullObjectRequested() {
		opts.UserMetadata[amzChecksumMode] = ChecksumFullObjectMode.String()
	}
}

func applyAutoChecksum(opts *PutObjectOptions, allParts []ObjectPart) {
	if !opts.AutoChecksum.IsSet() {
		return
	}
	if opts.AutoChecksum.CanComposite() && !opts.AutoChecksum.Is(ChecksumFullObject) {
		// Add composite hash of hashes.
		crc, err := opts.AutoChecksum.CompositeChecksum(allParts)
		if err == nil {
			opts.UserMetadata = map[string]string{opts.AutoChecksum.Key(): crc.Encoded()}
		}
	} else if opts.AutoChecksum.CanMergeCRC() {
		crc, err := opts.AutoChecksum.FullObjectChecksum(allParts)
		if err == nil {
			opts.UserMetadata = map[string]string{
				opts.AutoChecksum.KeyCapitalized(): crc.Encoded(),
				amzChecksumMode:                    ChecksumFullObjectMode.String(),
			}
		}
	}
}
