// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 || arm64 || ppc64le || s390x || riscv64 || loong64

package libc // import "modernc.org/libc"

// bytes.IndexByte has vectorized (or at least word-at-a-time)
// assembly on these arches, where the chunked IndexByte scan beats
// the word-at-a-time scan from 16 bytes up; see BenchmarkStrlen.
const strlenUseIndexByte = true
