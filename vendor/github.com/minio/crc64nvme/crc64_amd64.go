// Copyright (c) 2025 Minio Inc. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

//go:build !noasm && !appengine && !gccgo

package crc64nvme

import (
	"github.com/klauspost/cpuid/v2"
)

var hasAsm = cpuid.CPU.Supports(cpuid.SSE2, cpuid.CLMUL, cpuid.SSE4)

func updateAsm(crc uint64, p []byte) (checksum uint64)
