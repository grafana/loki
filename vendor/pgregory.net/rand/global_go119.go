// Copyright 2022 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

//go:build go1.19 && !unsafe

package rand

import "hash/maphash"

func rand64() uint64 {
	return maphash.Bytes(maphash.MakeSeed(), nil)
}
