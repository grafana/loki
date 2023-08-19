//go:build windows
// +build windows

package watch

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func IsDeletePending(f *os.File) (bool, error) {
	if f == nil {
		return false, nil
	}

	fi, err := getFileStandardInfo(f)
	if err != nil {
		return false, err
	}

	return fi.DeletePending, nil
}

// From: https://github.com/microsoft/go-winio/blob/main/fileinfo.go
// FileStandardInfo contains extended information for the file.
// FILE_STANDARD_INFO in WinBase.h
// https://docs.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_standard_info
type fileStandardInfo struct {
	AllocationSize, EndOfFile int64
	NumberOfLinks             uint32
	DeletePending, Directory  bool
}

// GetFileStandardInfo retrieves ended information for the file.
func getFileStandardInfo(f *os.File) (*fileStandardInfo, error) {
	si := &fileStandardInfo{}
	if err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()),
		windows.FileStandardInfo,
		(*byte)(unsafe.Pointer(si)),
		uint32(unsafe.Sizeof(*si))); err != nil {
		return nil, &os.PathError{Op: "GetFileInformationByHandleEx", Path: f.Name(), Err: err}
	}
	runtime.KeepAlive(f)
	return si, nil
}
