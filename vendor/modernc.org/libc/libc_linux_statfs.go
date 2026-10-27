// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm || mips64le)

package libc // import "modernc.org/libc"

// int statfs(const char *path, struct statfs *buf);
// Wrapper for ___statfs from ccgo-transpiled musl.
func Xstatfs(tls *TLS, path uintptr, buf uintptr) int32 {
	return ___statfs(tls, path, buf)
}
