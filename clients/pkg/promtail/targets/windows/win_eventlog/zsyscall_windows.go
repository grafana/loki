// The MIT License (MIT)

// Copyright (c) 2015-2020 InfluxData Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//go:build windows
// +build windows

//revive:disable-next-line:var-naming
// Package win_eventlog Input plugin to collect Windows Event Log messages
package win_eventlog

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var _ unsafe.Pointer

// EvtHandle uintptr
type EvtHandle uintptr

// Do the interface allocations only once for common
// Errno values.
const (
	//revive:disable-next-line:var-naming
	errnoERROR_IO_PENDING = 997
)

var (
	//revive:disable-next-line:var-naming
	errERROR_IO_PENDING error = syscall.Errno(errnoERROR_IO_PENDING)
)

// EvtFormatMessageFlag defines the values that specify the message string from
// the event to format.
type EvtFormatMessageFlag uint32

// EVT_FORMAT_MESSAGE_FLAGS enumeration
// https://msdn.microsoft.com/en-us/library/windows/desktop/aa385525(v=vs.85).aspx
const (
	//revive:disable:var-naming
	// Format the event's message string.
	EvtFormatMessageEvent EvtFormatMessageFlag = iota + 1
	// Format the message string of the level specified in the event.
	EvtFormatMessageLevel
	// Format the message string of the task specified in the event.
	EvtFormatMessageTask
	// Format the message string of the task specified in the event.
	EvtFormatMessageOpcode
	// Format the message string of the keywords specified in the event. If the
	// event specifies multiple keywords, the formatted string is a list of
	// null-terminated strings. Increment through the strings until your pointer
	// points past the end of the used buffer.
	EvtFormatMessageKeyword
	//revive:enable:var-naming
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case errnoERROR_IO_PENDING:
		return errERROR_IO_PENDING
	}

	return e
}

var (
	modwevtapi = windows.NewLazySystemDLL("wevtapi.dll")

	procEvtSubscribe             = modwevtapi.NewProc("EvtSubscribe")
	procEvtRender                = modwevtapi.NewProc("EvtRender")
	procEvtClose                 = modwevtapi.NewProc("EvtClose")
	procEvtNext                  = modwevtapi.NewProc("EvtNext")
	procEvtFormatMessage         = modwevtapi.NewProc("EvtFormatMessage")
	procEvtOpenPublisherMetadata = modwevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtCreateBookmark        = modwevtapi.NewProc("EvtCreateBookmark")
	procEvtUpdateBookmark        = modwevtapi.NewProc("EvtUpdateBookmark")
)

func _EvtSubscribe(session EvtHandle, signalEvent uintptr, channelPath *uint16, query *uint16, bookmark EvtHandle, context uintptr, callback syscall.Handle, flags EvtSubscribeFlag) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall9(procEvtSubscribe.Addr(), 8, uintptr(session), uintptr(signalEvent), uintptr(unsafe.Pointer(channelPath)), uintptr(unsafe.Pointer(query)), uintptr(bookmark), uintptr(context), uintptr(callback), uintptr(flags), 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtRender(context EvtHandle, fragment EvtHandle, flags EvtRenderFlag, bufferSize uint32, buffer *byte, bufferUsed *uint32, propertyCount *uint32) (err error) {
	r1, _, e1 := syscall.Syscall9(procEvtRender.Addr(), 7, uintptr(context), uintptr(fragment), uintptr(flags), uintptr(bufferSize), uintptr(unsafe.Pointer(buffer)), uintptr(unsafe.Pointer(bufferUsed)), uintptr(unsafe.Pointer(propertyCount)), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtClose(object EvtHandle) (err error) {
	r1, _, e1 := syscall.Syscall(procEvtClose.Addr(), 1, uintptr(object), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtNext(resultSet EvtHandle, eventArraySize uint32, eventArray *EvtHandle, timeout uint32, flags uint32, numReturned *uint32) (err error) {
	r1, _, e1 := syscall.Syscall6(procEvtNext.Addr(), 6, uintptr(resultSet), uintptr(eventArraySize), uintptr(unsafe.Pointer(eventArray)), uintptr(timeout), uintptr(flags), uintptr(unsafe.Pointer(numReturned)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtFormatMessage(publisherMetadata EvtHandle, event EvtHandle, messageID uint32, valueCount uint32, values uintptr, flags EvtFormatMessageFlag, bufferSize uint32, buffer *byte, bufferUsed *uint32) (err error) {
	r1, _, e1 := syscall.Syscall9(procEvtFormatMessage.Addr(), 9, uintptr(publisherMetadata), uintptr(event), uintptr(messageID), uintptr(valueCount), uintptr(values), uintptr(flags), uintptr(bufferSize), uintptr(unsafe.Pointer(buffer)), uintptr(unsafe.Pointer(bufferUsed)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtOpenPublisherMetadata(session EvtHandle, publisherIdentity *uint16, logFilePath *uint16, locale uint32, flags uint32) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall6(procEvtOpenPublisherMetadata.Addr(), 5, uintptr(session), uintptr(unsafe.Pointer(publisherIdentity)), uintptr(unsafe.Pointer(logFilePath)), uintptr(locale), uintptr(flags), 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

// CreateBookmark create a new bookmark from a string, if the string is empty a new bookrmak is created.
// Use update the bookmark with an event to save the position, or render to get the bookmark data to store.
func CreateBookmark(bookmark string) (EvtHandle, error) {
	bookmarkPtr, err := stringPointer(bookmark)
	if err != nil {
		return 0, err
	}
	r0, _, e1 := syscall.Syscall(procEvtCreateBookmark.Addr(), 1, bookmarkPtr, 0, 0)
	handle := EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			return 0, errnoErr(e1)
		}
		return 0, syscall.EINVAL
	}
	return handle, nil
}

func UpdateBookmark(bookmark, event EvtHandle, buf []byte) (string, error) {
	r1, _, e1 := syscall.Syscall(procEvtUpdateBookmark.Addr(), 2, uintptr(bookmark), uintptr(event), 0)
	if r1 == 0 {
		if e1 != 0 {
			return "", errnoErr(e1)
		} else {
			return "", syscall.EINVAL
		}
	}
	var bufferUsed, propertyCount uint32
	err := _EvtRender(0, bookmark, EvtRenderBookmark, uint32(len(buf)), &buf[0], &bufferUsed, &propertyCount)
	if err != nil {
		return "", err
	}

	bookMarkData, err := DecodeUTF16(buf[:bufferUsed])
	if err != nil {
		return "", err
	}
	return string(bookMarkData), nil
}

func stringPointer(s string) (uintptr, error) {
	if s == "" {
		return 0, nil
	}
	// last character is always nil and causes issue for decoding.
	ptr, err := syscall.UTF16PtrFromString(s[:len(s)-1])
	if err != nil {
		return 0, err
	}
	return uintptr(unsafe.Pointer(ptr)), nil

}
