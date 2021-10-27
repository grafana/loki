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
)

// Event log error codes.
// https://msdn.microsoft.com/en-us/library/windows/desktop/ms681382(v=vs.85).aspx
const (
	//revive:disable:var-naming
	ERROR_INSUFFICIENT_BUFFER syscall.Errno = 122
	ERROR_NO_MORE_ITEMS       syscall.Errno = 259
	ERROR_INVALID_OPERATION   syscall.Errno = 4317
	//revive:enable:var-naming
)

// EvtSubscribeFlag defines the possible values that specify when to start subscribing to events.
type EvtSubscribeFlag uint32

// EVT_SUBSCRIBE_FLAGS enumeration
// https://msdn.microsoft.com/en-us/library/windows/desktop/aa385588(v=vs.85).aspx
const (
	EvtSubscribeToFutureEvents     EvtSubscribeFlag = 1
	EvtSubscribeStartAfterBookmark EvtSubscribeFlag = 3
)

// EvtRenderFlag uint32
type EvtRenderFlag uint32

// EVT_RENDER_FLAGS enumeration
// https://msdn.microsoft.com/en-us/library/windows/desktop/aa385563(v=vs.85).aspx
const (
	//revive:disable:var-naming
	// Render the event as an XML string. For details on the contents of the
	// XML string, see the Event schema.
	EvtRenderEventXml EvtRenderFlag = 1
	//revive:enable:var-naming
	EvtRenderBookmark EvtRenderFlag = 2
)
