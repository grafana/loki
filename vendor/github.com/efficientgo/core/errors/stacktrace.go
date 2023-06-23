// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Initially copied from Thanos and contributed by https://github.com/bisakhmondal.
//
// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package errors

import (
	"fmt"
	"runtime"
	"strings"
)

// stacktrace holds a snapshot of program counters.
type stacktrace []uintptr

// newStackTrace captures a stack trace. It skips first 3 frames to record the
// snapshot of the stack trace at the origin of a particular error. It tries to
// record maximum 16 frames (if available).
func newStackTrace() stacktrace {
	const stackDepth = 16 // record maximum 16 frames (if available).

	pc := make([]uintptr, stackDepth)
	// using skip=3 for not to count the program counter address of
	// 1. the respective function from errors package (eg. errors.New)
	// 2. newStacktrace itself
	// 3. the function used in runtime.Callers
	n := runtime.Callers(3, pc)

	// this approach is taken to reduce long term memory footprint (obtained through escape analysis).
	// We are returning a new slice by re-slicing the pc with the required length and capacity (when the
	// no of returned callFrames is less that stackDepth). This uses less memory compared to pc[:n] as
	// the capacity of new slice is inherited from the parent slice if not specified.
	return pc[:n:n]
}

// String implements the fmt.Stringer interface to provide formatted text output.
func (s stacktrace) String() string {
	var buf strings.Builder

	// CallersFrames takes the slice of Program Counter addresses returned by Callers to
	// retrieve function/file/line information.
	cf := runtime.CallersFrames(s)
	for {
		// more indicates if the next call will be successful or not.
		frame, more := cf.Next()
		// used formatting scheme <`>`space><function name><tab><filepath><:><line><newline> for example:
		// > testing.tRunner	/home/go/go1.17.8/src/testing/testing.go:1259
		buf.WriteString(fmt.Sprintf("> %s\t%s:%d\n", frame.Func.Name(), frame.File, frame.Line))
		if !more {
			break
		}
	}
	return buf.String()
}
