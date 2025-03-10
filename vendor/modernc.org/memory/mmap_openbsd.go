// Copyright 2011 Evan Shaw. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE-MMAP-GO file.

// Modifications (c) 2024 The Memory Authors.
// Copyright 2024 The Memory Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build openbsd && (386 || amd64 || arm64)

package memory

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// track what can be unmapped
var allocmap map[uintptr][]byte
var m sync.Mutex

const pageSizeLog = 20

var (
	osPageMask = osPageSize - 1
	osPageSize = os.Getpagesize()
)

func init() {
	allocmap = make(map[uintptr][]byte)
}

func unmap(addr uintptr, size int) error {
	if trace {
		fmt.Fprintf(os.Stderr, "unmap %#x\n", addr)
	}

	a, ok := allocmap[addr]
	if !ok {
		if trace {
			fmt.Fprintf(os.Stderr, "unmap %#x: not found\n", addr)
		}
		// panic("unmap called on unknown mapping")
		return nil
	}

	if err := unix.Munmap(a); err != nil {
		if trace {
			fmt.Fprintf(os.Stderr, "unmap: %s\n", err.Error())
		}
		// panic(err.Error())
		return err
	}

	m.Lock()
	delete(allocmap, addr)
	m.Unlock()

	return nil
}

func mmap(size int) (uintptr, int, error) {
	roundsize := roundup(size, osPageSize) + pageSize

	b, err := unix.Mmap(-1, 0, roundsize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return 0, 0, err
	}

	p := uintptr(unsafe.Pointer(&b[0]))

	if trace {
		fmt.Fprintf(os.Stderr, "mmap actual @%#x size: %#x\n", p, roundsize)
	}

	// waste all the space until the next page
	r := (p + uintptr(pageSize)) &^ uintptr(pageMask)
	nsize := (roundsize) - int((r - p))
	if nsize < size {
		panic("didn't allocate enough to meet initial request!")
	}

	if trace {
		fmt.Fprintf(os.Stderr, "mmap page-rounded @%#x size: %#x\n", r, nsize)
	}

	m.Lock()
	allocmap[r] = b
	m.Unlock()

	return r, nsize, nil
}
