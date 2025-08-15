// Copyright 2025 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

func TLSAlloc(p0 *TLS, p1 int) uintptr
func TLSFree(p0 *TLS, p1 int)

func tlsAlloc(tls *TLS, n int) uintptr {
	return tls.Alloc(n)
}

func tlsFre(tls *TLS, n int) {
	tls.Free(n)
}
