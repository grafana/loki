// Copyright 2019 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite3

import (
	"syscall"
	"unsafe"

	"modernc.org/libc"
)

// Format and write a message to the log if logging is enabled.
func X__ccgo_sqlite3_log(t *libc.TLS, iErrCode int32, zFormat uintptr, va uintptr) { /* sqlite3.c:29405:17: */
	libc.X__ccgo_sqlite3_log(t, iErrCode, zFormat, va)
}

// https://gitlab.com/cznic/sqlite/-/issues/199
//
// The transpiled getpagesize entry of _aSyscall does not report the kernel page
// size on linux/arm64, so SQLite misaligns the mmap of its WAL shm regions on a
// 64 KB page kernel and reports a disk I/O error. Substitute a Go implementation
// at run time; sqlite.go calls this from its init.
func PatchIssue199() {
	p := unsafe.Pointer(&_aSyscall)
	*(*uintptr)(unsafe.Add(p, 608)) = __ccgo_fp(_unixGetpagesizeIssue199)
}

func _unixGetpagesizeIssue199(tls *libc.TLS) (r int32) {
	return int32(syscall.Getpagesize())
}
