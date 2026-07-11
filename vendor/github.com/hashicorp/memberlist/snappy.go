// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package memberlist

import (
	"fmt"

	"github.com/golang/snappy"
)

// snappyCompress compresses src using snappy and returns a freshly-
// allocated slice owned by the caller.
func snappyCompress(src []byte) []byte {
	return snappy.Encode(nil, src)
}

// snappyDecompress returns a freshly allocated []byte holding the
// decompressed payload.
//
// The claimed decoded length is checked against maxDecompressBytes before
// allocation so a malformed peer cannot trigger an oversized make().
func snappyDecompress(src []byte) ([]byte, error) {
	n, err := snappy.DecodedLen(src)
	if err != nil {
		return nil, fmt.Errorf("snappy.DecodedLen: %w", err)
	}
	if n > maxDecompressBytes {
		return nil, fmt.Errorf("memberlist: snappy-decompressed payload would exceed %d bytes (claimed %d)", maxDecompressBytes, n)
	}
	return snappy.Decode(make([]byte, n), src)
}
