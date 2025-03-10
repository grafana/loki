// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE-GO file.

// Modifications Copyright 2020 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

import (
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"
	// "golang.org/x/sys/unix"
	// "modernc.org/libc/errno"
	// "modernc.org/libc/fcntl"
)

// Random number state.
// We generate random temporary file names so that there's a good
// chance the file doesn't exist yet - keeps the number of tries in
// TempFile to a minimum.
var randState uint32
var randStateMu sync.Mutex

func reseed() uint32 {
	return uint32(time.Now().UnixNano() + int64(os.Getpid()))
}

func nextRandom(x uintptr) {
	randStateMu.Lock()
	r := randState
	if r == 0 {
		r = reseed()
	}
	r = r*1664525 + 1013904223 // constants from Numerical Recipes
	randState = r
	randStateMu.Unlock()
	copy((*RawMem)(unsafe.Pointer(x))[:6:6], fmt.Sprintf("%06d", int(1e9+r%1e9)%1e6))
}

func tempFile(s, x uintptr, flags int32) (fd int, err error) {
	panic(todo(""))
	// const maxTry = 10000
	// nconflict := 0
	// flags |= int32(os.O_RDWR | os.O_CREATE | os.O_EXCL | unix.O_LARGEFILE)
	// for i := 0; i < maxTry; i++ {
	// 	nextRandom(x)
	// 	fdcwd := fcntl.AT_FDCWD
	// 	n, _, err := unix.Syscall6(unix.SYS_OPENAT, uintptr(fdcwd), s, uintptr(flags), 0600, 0, 0)
	// 	if err == 0 {
	// 		return int(n), nil
	// 	}

	// 	if err != errno.EEXIST {
	// 		return -1, err
	// 	}

	// 	if nconflict++; nconflict > 10 {
	// 		randStateMu.Lock()
	// 		randState = reseed()
	// 		nconflict = 0
	// 		randStateMu.Unlock()
	// 	}
	// }
	// return -1, unix.Errno(errno.EEXIST)
}
