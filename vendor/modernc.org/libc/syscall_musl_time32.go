// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (386 || arm)

package libc // import "modernc.org/libc"

// sysClockNanosleepTime64 is the clock_nanosleep(2) syscall taking a 64-bit
// struct timespec, which musl uses on targets whose time_t is wider than long.
const sysClockNanosleepTime64 = SYS_clock_nanosleep_time64
