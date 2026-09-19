//go:build windows && !appengine

package maxminddb

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mmap maps a file into memory and returns a byte slice.
func mmap(fd, length int) ([]byte, error) {
	// Create a file mapping
	handle, err := windows.CreateFileMapping(
		windows.Handle(fd),
		nil,
		windows.PAGE_READONLY,
		0,
		0,
		nil,
	)
	if err != nil {
		return nil, os.NewSyscallError("CreateFileMapping", err)
	}
	defer windows.CloseHandle(handle)

	// Map the file into memory
	addrUintptr, err := windows.MapViewOfFile(
		handle,
		windows.FILE_MAP_READ,
		0,
		0,
		0,
	)
	if err != nil {
		return nil, os.NewSyscallError("MapViewOfFile", err)
	}

	// When there's not enough address space for the whole file (e.g. large
	// files on 32-bit systems), MapViewOfFile may return a partial mapping.
	// Query the region size and fail on partial mappings.
	var info windows.MemoryBasicInformation
	if err := windows.VirtualQuery(addrUintptr, &info, unsafe.Sizeof(info)); err != nil {
		_ = windows.UnmapViewOfFile(addrUintptr)
		return nil, os.NewSyscallError("VirtualQuery", err)
	}
	if info.RegionSize < uintptr(length) {
		_ = windows.UnmapViewOfFile(addrUintptr)
		return nil, errors.New("file too large")
	}

	// Workaround for unsafeptr check in go vet, see
	// https://github.com/golang/go/issues/58625
	addr := *(*unsafe.Pointer)(unsafe.Pointer(&addrUintptr))
	return unsafe.Slice((*byte)(addr), length), nil
}

// munmap unmaps a memory-mapped file and releases associated resources.
func munmap(b []byte) error {
	// Convert slice to base address and length
	data := unsafe.SliceData(b)
	addr := uintptr(unsafe.Pointer(data))

	// Unmap the memory
	if err := windows.UnmapViewOfFile(addr); err != nil {
		return os.NewSyscallError("UnmapViewOfFile", err)
	}
	return nil
}
