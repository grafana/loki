// Copyright 2020 Brad Fitzpatrick. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package go4.org/unsafe/assume-no-moving-gc exists so you can depend
// on it from unsafe code that wants to declare that it assumes that
// the Go runtime does not using a moving garbage collector. Specifically,
// it asserts that the caller is playing stupid games with the addresses
// of heap-allocated values. It says nothing about values that Go's escape
// analysis keeps on the stack. Ensuring things aren't stack-allocated
// is the caller's responsibility.
//
// This package is then updated for new Go versions when that
// is still the case and explodes at runtime with a failure
// otherwise, unless an environment variable overrides it.
//
// To use:
//
//     import _ "go4.org/unsafe/assume-no-moving-gc"
//
// There is no API.
//
// It is intentional that this package will break code that's not updated
// regularly to double check its assumptions about the world and new Go
// versions. If you play stupid games with unsafe pointers, the stupid prize
// is this maintenance cost. (The alternative would be memory corruption if
// some unmaintained, unsafe library were built with a future version of Go
// that worked very differently than when the unsafe library was built.)
// Ideally you shouldn't write unsafe code, though.
//
// The GitHub repo is at https://github.com/go4org/unsafe-assume-no-moving-gc
package assume_no_moving_gc

const env = "ASSUME_NO_MOVING_GC_UNSAFE_RISK_IT_WITH"
