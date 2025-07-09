// Copyright 2021 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package checksum

import (
	"crypto/md5"
	"encoding/base64"
	"hash"
	"hash/crc32"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

func crc32cChecksum(content []byte) []byte {
	checksummer := crc32.New(crc32cTable)
	checksummer.Write(content)
	return checksummer.Sum(make([]byte, 0, 4))
}

func EncodedChecksum(checksum []byte) string {
	return base64.StdEncoding.EncodeToString(checksum)
}

func EncodedCrc32cChecksum(content []byte) string {
	return EncodedChecksum(crc32cChecksum(content))
}

func MD5Hash(b []byte) []byte {
	h := md5.New()
	h.Write(b)
	return h.Sum(nil)
}

func EncodedHash(hash []byte) string {
	return base64.StdEncoding.EncodeToString(hash)
}

func EncodedMd5Hash(content []byte) string {
	return EncodedHash(MD5Hash(content))
}

type StreamingHasher struct {
	crc32 hash.Hash32
	md5   hash.Hash
}

func NewStreamingHasher() *StreamingHasher {
	return &StreamingHasher{
		crc32: crc32.New(crc32cTable),
		md5:   md5.New(),
	}
}

func (s *StreamingHasher) Write(p []byte) (n int, err error) {
	n, err = s.crc32.Write(p)
	if err != nil {
		return n, err
	}
	return s.md5.Write(p)
}

func (s *StreamingHasher) EncodedCrc32cChecksum() string {
	return EncodedChecksum(s.crc32.Sum(nil))
}

func (s *StreamingHasher) EncodedMd5Hash() string {
	return EncodedHash(s.md5.Sum(nil))
}
