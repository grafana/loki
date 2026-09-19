// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm)

package libc // import "modernc.org/libc"

import (
	"golang.org/x/sys/unix"
	"runtime"
)

func ___syscall_cp(tls *TLS, n, a, b, c, d, e, f long) long {
	switch n {
	case SYS_nanosleep:
		// int nanosleep(const struct timespec *req, struct timespec *rem)
		return sleepSyscall(tls, n, [6]long{a, b, c, d, e, f}, 0, 1, 0)
	case SYS_clock_nanosleep, sysClockNanosleepTime64:
		// int clock_nanosleep(clockid_t clk, int flags,
		//	const struct timespec *req, struct timespec *rem)
		return sleepSyscall(tls, n, [6]long{a, b, c, d, e, f}, 2, 3, b)
	}

	r1, _, err := (unix.Syscall6(uintptr(n), uintptr(a), uintptr(b), uintptr(c), uintptr(d), uintptr(e), uintptr(f)))
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

// sleepScratchSize is the size of the buffer sleepSyscall gives the kernel to
// report the time remaining in. It must be at least the size of the widest
// struct timespec the sleep syscalls use, which is the 64-bit one - two 64-bit
// words - also on 32-bit targets, where it is what the time64 variants take.
const sleepScratchSize = 16

// sleepSyscall issues one of the sleep syscalls and restarts it when a signal
// interrupts it.
//
// A program translated to Go by ccgo shares its process with the Go runtime,
// which signals threads with SIGURG to preempt goroutines asynchronously. The
// translated C code never installed that handler and cannot reasonably cope
// with the EINTR it causes: musl reports the interruption faithfully, but
// callers commonly discard the return value - SQLite's unixSleep() does - so
// the sleep silently comes up short and timing-dependent code misbehaves, e.g.
// a busy handler that gives up before its timeout. Restarting the sleep for the
// time remaining hides the runtime's own signals from the C program, which is
// also how the hand-written darwin, freebsd, netbsd and openbsd ports behave:
// their Xnanosleep sleeps in Go and cannot be interrupted at all.
//
// iReq and iRem are the indices in args of the req and rem arguments and flags
// is the clock_nanosleep(2) flags argument, zero for nanosleep(2).
func sleepSyscall(tls *TLS, n long, args [6]long, iReq, iRem int, flags long) long {
	if args[iRem] == 0 && flags&TIMER_ABSTIME == 0 && tls != nil {
		// The caller does not want the remaining time reported, but restarting
		// an interrupted sleep needs it. The kernel writes it in the same
		// layout it reads the request in, so it can be passed back as the next
		// request unchanged.
		p := tls.Alloc(sleepScratchSize)
		defer tls.Free(sleepScratchSize)

		args[iRem] = long(p)
	}
	for {
		r1, _, err := unix.Syscall6(uintptr(n), uintptr(args[0]), uintptr(args[1]), uintptr(args[2]), uintptr(args[3]), uintptr(args[4]), uintptr(args[5]))
		switch {
		case err == 0:
			return long(r1)
		case err != unix.EINTR:
			return long(-err)
		case flags&TIMER_ABSTIME != 0:
			// Waiting for an absolute deadline: the original request is still
			// the right one and the kernel reports no remaining time.
			continue
		case args[iRem] == 0:
			// Nowhere for the kernel to report the remaining time, so the sleep
			// cannot be restarted without overshooting. Report the
			// interruption.
			return long(-err)
		}
		args[iReq] = args[iRem]
	}
}

func X__syscall0(tls *TLS, n long) long {
	switch n {
	case __NR_sched_yield:
		runtime.Gosched()
		return 0
	default:
		r1, _, err := unix.Syscall(uintptr(n), 0, 0, 0)
		if err != 0 {
			return long(-err)
		}

		return long(r1)
	}
}

func X__syscall1(tls *TLS, n, a1 long) long {
	r1, _, err := unix.Syscall(uintptr(n), uintptr(a1), 0, 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall2(tls *TLS, n, a1, a2 long) long {
	r1, _, err := unix.Syscall(uintptr(n), uintptr(a1), uintptr(a2), 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall3(tls *TLS, n, a1, a2, a3 long) long {
	r1, _, err := unix.Syscall(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3))
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall4(tls *TLS, n, a1, a2, a3, a4 long) long {
	r1, _, err := unix.Syscall6(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(a4), 0, 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall5(tls *TLS, n, a1, a2, a3, a4, a5 long) long {
	r1, _, err := unix.Syscall6(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(a4), uintptr(a5), 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall6(tls *TLS, n, a1, a2, a3, a4, a5, a6 long) long {
	r1, _, err := unix.Syscall6(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(a4), uintptr(a5), uintptr(a6))
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}
