// Copyright 2025 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

//go:noescape
func TLSAlloc(p0 *TLS, p1 int) uintptr

//go:noescape
func TLSFree(p0 *TLS, p1 int)

func tlsAlloc(tls *TLS, n int) uintptr {
	return tls.Alloc(n)
}

func tlsFree(tls *TLS, n int) {
	tls.Free(n)
}
