// Copyright 2019 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"os"
	"syscall"
)

func createTimeFromFileInfo(input os.FileInfo) syscall.Timespec {
	if statT, ok := input.Sys().(*syscall.Stat_t); ok {
		return statT.Ctimespec
	}
	return syscall.Timespec{}
}
