// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package logerrcapture

// Close a `io.Closer` interface or execute any function that returns error safely while logging error.
// It's often forgotten but it's a caller responsibility to close all implementations of `Closer`,
// such as *os.File or io.ReaderCloser. Commonly we would use:
//
// 	defer closer.Close()
//
// This is wrong. Close() usually return important error (e.g for os.File the actual file flush might happen and fail on `Close` method).
// It's very important to *always* check error. `logerrcapture` provides utility functions to capture error and log it via provided
// logger, while still allowing to put them in a convenient `defer` statement:
//
// 	func <...>(...) (err error) {
//  	...
//  	defer logerrcapture.Do(logger, closer.Close, "log format message")
//
// 		...
// 	}
//
// If Close returns error, `logerrcapture.Do` will capture it, add to input error if not nil and return by argument.
//
// The logerrcapture.ExhaustClose function provide the same functionality but takes an io.ReadCloser and exhausts the whole
// reader before closing. This is useful when trying to use http keep-alive connections because for the same connection
// to be re-used the whole response body needs to be exhausted.
//
// Recommended: Check https://pkg.go.dev/github.com/efficientgo/tools/pkg/errcapture if you want to return error instead of just logging (causing
// hard error).
