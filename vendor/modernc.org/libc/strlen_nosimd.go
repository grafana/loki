// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !(amd64 || arm64 || ppc64le || s390x || riscv64 || loong64)

package libc // import "modernc.org/libc"

// On the remaining arches (386, arm, ...) bytes.IndexByte is a plain
// byte loop, and the chunked IndexByte scan loses to the
// word-at-a-time scan at every size (measured ~2x slower than even a
// byte loop on 386), so strlen uses strlenWords there.
const strlenUseIndexByte = false
