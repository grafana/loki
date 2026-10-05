// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!linux || mips64le) && !darwin

package libc // import "modernc.org/libc"

// int openpty(int *amaster, int *aslave, char *name,
//
//	const struct termios *termp,
//	const struct winsize *winp);
//
// The darwin implementation is in libc_darwin.go.
func Xopenpty(t *TLS, amaster, aslave, name, termp, winp uintptr) int32 {
	if __ccgo_strace {
		trc("t=%v amaster=%v aslave=%v name=%v termp=%v winp=%v, (%v:)", t, amaster, aslave, name, termp, winp, origin(2))
	}
	panic(todo(""))
}
