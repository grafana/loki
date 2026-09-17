// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64)

package libc // import "modernc.org/libc"

// sysClockNanosleepTime64 is the clock_nanosleep(2) syscall taking a 64-bit
// struct timespec. These targets have no such separate syscall - their
// struct timespec is already 64-bit - so this is a number no caller can pass.
const sysClockNanosleepTime64 = -1
