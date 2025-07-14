// Copyright (c) 2018 David Crawshaw <david@zentus.com>
// Copyright (c) 2021 Roxy Light <roxy@zombiezen.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
//
// SPDX-License-Identifier: ISC

package sqlite

import (
	"fmt"
	"sync"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/libc/sys/types"
	lib "modernc.org/sqlite/lib"
)

// See https://sqlite.org/unlock_notify.html for detailed explanation.

// unlockNote is a C-allocated struct used as a condition variable.
type unlockNote struct {
	mu    sync.Mutex
	wait  sync.Mutex // held while fired == false
	fired bool
}

func allocUnlockNote(tls *libc.TLS) (uintptr, error) {
	ptr := libc.Xcalloc(tls, 1, types.Size_t(unsafe.Sizeof(unlockNote{})))
	if ptr == 0 {
		return 0, fmt.Errorf("out of memory for unlockNote")
	}
	un := (*unlockNote)(unsafe.Pointer(ptr))
	un.wait.Lock()
	return ptr, nil
}

func fireUnlockNote(tls *libc.TLS, ptr uintptr) {
	un := (*unlockNote)(unsafe.Pointer(ptr))
	un.mu.Lock()
	if !un.fired {
		un.fired = true
		un.wait.Unlock()
	}
	un.mu.Unlock()
}

func unlockNotifyCallback(tls *libc.TLS, apArg uintptr, nArg int32) {
	for ; nArg > 0; nArg-- {
		fireUnlockNote(tls, *(*uintptr)(unsafe.Pointer(apArg)))
		// apArg is a C array of pointers.
		apArg += unsafe.Sizeof(uintptr(0))
	}
}

func waitForUnlockNotify(tls *libc.TLS, db uintptr, unPtr uintptr) ResultCode {
	un := (*unlockNote)(unsafe.Pointer(unPtr))
	if un.fired {
		un.wait.Lock()
	}
	un.fired = false

	cbPtr := cFuncPointer(unlockNotifyCallback)

	res := ResultCode(lib.Xsqlite3_unlock_notify(tls, db, cbPtr, unPtr))

	if res == ResultOK {
		un.mu.Lock()
		fired := un.fired
		un.mu.Unlock()
		if !fired {
			un.wait.Lock()
			un.wait.Unlock()
		}
	}
	return res
}
