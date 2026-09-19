// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

import (
	mbits "math/bits"
)

func X__builtin_ctzll(tls *TLS, x uint64) int32 {
	return int32(mbits.TrailingZeros64(x))
}
