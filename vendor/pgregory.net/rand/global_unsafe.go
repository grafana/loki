// Copyright 2022 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

//go:build unsafe

package rand

import (
	_ "unsafe"
)

// if you *really* want to win the benchmarks game:

//go:linkname rand64 runtime.fastrand64
func rand64() uint64
