// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Structured exception handling (SEH) emulation for wal.c.
//
// MSVC builds of SQLite wrap the parts of wal.c that touch the memory-mapped
// -shm file in __try/__except blocks and turn an EXCEPTION_IN_PAGE_ERROR into
// SQLITE_IOERR_IN_PAGE (SQLITE_USE_SEH, see the "Structured Exception
// Handling" section of wal.c). Every other build, this one included until
// now, dies on such a fault: https://gitlab.com/cznic/sqlite/-/issues/221.
//
// Go cannot express __try, but the Go runtime converts the same hardware fault
// - EXCEPTION_IN_PAGE_ERROR on Windows, SIGBUS/SIGSEGV elsewhere - into a
// recoverable panic while debug.SetPanicOnFault is in effect for the
// goroutine. internal/sqlite_issue221.patch therefore enables SQLITE_USE_SEH
// under __CCGO__ and routes each SEH_TRY{...}SEH_EXCEPT(...) block of wal.c
// through modernc_seh_try(), implemented below: the protected statements
// become a C thunk called under the guard; a caught fault runs the block's
// cleanup (walHandleException) and the operation fails with
// SQLITE_IOERR_IN_PAGE, the connection staying usable, exactly as in the MSVC
// build. Without a fault nothing changes.
//
// This file is hand-written and lives next to the transpiled library in both
// modernc.org/libsqlite3 (package libsqlite3) and modernc.org/sqlite/lib
// (package sqlite3); keep the two copies identical but for the package clause.

package sqlite3

import (
	"runtime/debug"
	"sync"
	"sync/atomic"
	"unsafe"

	"modernc.org/libc"
)

// walIndexPgsz is WALINDEX_PGSZ, the size of one wal-index page, which is also
// the size of one xShmMap region.
const walIndexPgsz = 32768

// sehFaultAddr is the optional method of the runtime.Error the Go runtime
// panics with on a memory fault while debug.SetPanicOnFault is in effect.
type sehFaultAddr interface {
	Addr() uintptr
}

// sehInjected is the panic value raised by _modernc_seh_inject to simulate a
// fault at an SEH_INJECT_FAULT site of wal.c.
type sehInjected struct{}

// sehFaultSim is a countdown: SehInject(n) makes the n-th SEH_INJECT_FAULT
// site reached afterwards raise a simulated fault.
var sehFaultSim atomic.Int32

// SehInject arms the simulated fault injector: the n-th SEH_INJECT_FAULT site
// of wal.c reached after the call raises a fault that the SEH emulation
// handles like a real one. n <= 0 disarms the injector. It is the counterpart
// of sqlite3FaultSim(650) in SQLite's own test builds and exists for tests.
func SehInject(n int32) { sehFaultSim.Store(n) }

// SehPending returns how many SEH_INJECT_FAULT sites still have to be reached
// before the armed simulated fault fires, or 0 when it has fired or the
// injector is not armed. For tests.
func SehPending() int32 {
	if n := sehFaultSim.Load(); n > 0 {
		return n
	}
	return 0
}

// _modernc_seh_inject implements SEH_INJECT_FAULT of wal.c for the ccgo build.
func _modernc_seh_inject(tls *libc.TLS, pWal uintptr) {
	if sehFaultSim.Load() <= 0 {
		return
	}
	if sehFaultSim.Add(-1) == 0 {
		panic(sehInjected{})
	}
}

// _modernc_seh_try implements modernc_seh_try() of wal.c: it runs
// xBody(pWal, pArg) under the fault guard and returns its result. If a memory
// fault inside the wal-index mapping, or a simulated fault, is caught, it
// returns xOnFault(pWal) instead, or SQLITE_IOERR_IN_PAGE when xOnFault is
// zero. Any other panic propagates.
//
// The guard is per goroutine and SQLite runs on the caller's goroutine, so
// arming it around the body is all that is needed; the previous state is
// restored on the way out, faults included.
func _modernc_seh_try(tls *libc.TLS, pWal, xBody, pArg, xOnFault uintptr) (rc int32) {
	prev := debug.SetPanicOnFault(true)
	defer func() {
		debug.SetPanicOnFault(prev)
		e := recover()
		if e == nil {
			return
		}

		addr, ok := sehCaught(pWal, e)
		if !ok {
			panic(e)
		}

		sehLog(tls, addr)
		if xOnFault != 0 {
			rc = (*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{xOnFault})))(tls, pWal)
			return
		}

		rc = SQLITE_IOERR_IN_PAGE
	}()

	return (*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{xBody})))(tls, pWal, pArg)
}

// sehCaught reports whether the recovered panic value e is a fault the SEH
// emulation handles: a simulated one, or a memory fault whose address lies in
// one of the wal-index pages of pWal. It returns the faulting address, 0 for a
// simulated fault.
//
// Restricting the real faults to the wal-index pages is stricter than
// SQLite's own filter, which accepts any EXCEPTION_IN_PAGE_ERROR raised
// inside the block: a fault anywhere else is a bug and must keep crashing.
func sehCaught(pWal uintptr, e any) (addr uintptr, ok bool) {
	switch x := e.(type) {
	case sehInjected:
		return 0, true
	case sehFaultAddr:
		addr = x.Addr()
		return addr, sehInWalIndex(pWal, addr)
	}
	return 0, false
}

// sehInWalIndex reports whether addr lies within a mapped wal-index page of
// pWal, that is within pWal->apWiData[i], i < pWal->nWiData, each page being
// WALINDEX_PGSZ bytes.
func sehInWalIndex(pWal, addr uintptr) bool {
	if pWal == 0 {
		return false
	}

	w := (*TWal)(unsafe.Pointer(pWal))
	for i := 0; i < int(w.FnWiData); i++ {
		p := *(*uintptr)(unsafe.Pointer(w.FapWiData + uintptr(i)*unsafe.Sizeof(uintptr(0))))
		if p != 0 && addr >= p && addr-p < walIndexPgsz {
			return true
		}
	}
	return false
}

var sehLogFmt struct {
	once sync.Once
	z    uintptr
}

// sehLog reports a caught fault through sqlite3_log(), so that it shows up
// wherever the application routes SQLITE_CONFIG_LOG.
func sehLog(tls *libc.TLS, addr uintptr) {
	sehLogFmt.once.Do(func() {
		sehLogFmt.z, _ = libc.CString("SEH emulation: memory fault at %p in the wal-index mapping, returning SQLITE_IOERR_IN_PAGE")
	})
	if sehLogFmt.z == 0 {
		return
	}

	va := libc.NewVaList(addr)
	Xsqlite3_log(tls, SQLITE_IOERR_IN_PAGE, sehLogFmt.z, va)
	libc.Xfree(tls, va)
}
