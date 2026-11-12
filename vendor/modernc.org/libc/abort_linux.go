// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm)

package libc // import "modernc.org/libc"

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// kernelSigaction is the kernel's struct sigaction as rt_sigaction takes it
// (arch/*/include/uapi/asm/signal.h): sa_handler, sa_flags, sa_restorer and
// the kernel sigset_t, which is _NSIG/8 = 8 bytes on every port built here
// (mips, where it is 16, is not one of them). sa_flags is an unsigned long,
// hence uintptr.
type kernelSigaction struct {
	handler  uintptr
	flags    uintptr
	restorer uintptr
	mask     uint64
}

// resetSigDfl installs the kernel's default disposition for sig and unblocks
// it in the calling thread, replacing whatever handler the kernel has for it,
// the Go runtime's included. ___libc_sigaction cannot do that: it only records
// dispositions for delivery through os/signal (issue #53), and the runtime
// keeps its own handler installed even after signal.Reset. It is for the one
// place that must die by a signal rather than handle it, Xabort: with the
// runtime's SIGABRT handler in place the process prints a goroutine dump to
// stderr before it dies, which no C abort() does. A parent that examines the
// child's death, such as Tcl's exec ("child killed: SIGABRT" only when stderr
// stayed empty), tells the two apart.
func resetSigDfl(sig int32) {
	sa := kernelSigaction{handler: SIG_DFL}
	unix.RawSyscall6(unix.SYS_RT_SIGACTION, uintptr(sig), uintptr(unsafe.Pointer(&sa)), 0, unsafe.Sizeof(sa.mask), 0, 0)
	set := uint64(1) << (uint(sig) - 1)
	unix.RawSyscall6(unix.SYS_RT_SIGPROCMASK, SIG_UNBLOCK, uintptr(unsafe.Pointer(&set)), 0, unsafe.Sizeof(set), 0, 0)
}
