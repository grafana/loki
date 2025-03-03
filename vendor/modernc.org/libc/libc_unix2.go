// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build illumos

package libc // import "modernc.org/libc"

import (
	"modernc.org/libc/sys/types"
)

// ssize_t recvmsg(int sockfd, struct msghdr *msg, int flags);
func Xrecvmsg(t *TLS, sockfd int32, msg uintptr, flags int32) types.Ssize_t {
	if __ccgo_strace {
		trc("t=%v sockfd=%v msg=%v flags=%v, (%v:)", t, sockfd, msg, flags, origin(2))
	}
	panic(todo(""))
	//	n, _, err := unix.Syscall(unix.SYS_RECVMSG, uintptr(sockfd), msg, uintptr(flags))
	//	if err != 0 {
	//		t.setErrno(err)
	//		return -1
	//	}
	//
	// return types.Ssize_t(n)
}
