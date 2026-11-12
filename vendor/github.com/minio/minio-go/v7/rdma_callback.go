//go:build rdma

/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2024-2026 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * SPDX-License-Identifier: Apache-2.0
 */

package minio

// A file carrying //export may hold declarations in its cgo preamble but no
// definitions, so the trampoline that reaches this lives in rdma_stream.go.
// The blank line below matters: only the comment block touching `import "C"`
// is the preamble, and this prose is not C.

// #include <sys/types.h>
import "C"

import (
	"context"
	"io"
	"runtime/cgo"
	"unsafe"
)

// maxConsecutiveEmptyReads bounds how long the callback waits on a reader
// returning (0, nil). bufio uses the same limit for the same reason: tolerate
// a stall, refuse to spin forever.
const maxConsecutiveEmptyReads = 100

// rdmaStreamSource is what the C read callback pulls from.
//
// The read error is kept here rather than returned through C: the callback can
// only say "failed", and losing the reason turns an ordinary error on the
// caller's stream into an opaque upload failure.
type rdmaStreamSource struct {
	// ctx is observed per callback. libminiocpp's upload is synchronous with
	// no cancellation hook, so this is the only place cancellation can be
	// seen; it stops the upload at the next part boundary, not instantly.
	ctx    context.Context
	reader io.Reader
	err    error
}

//export minioRDMAReadGo
func minioRDMAReadGo(userdata unsafe.Pointer, buf *C.char, size C.size_t) C.ssize_t {
	if userdata == nil {
		return -1
	}
	src, ok := (*(*cgo.Handle)(userdata)).Value().(*rdmaStreamSource)
	if !ok {
		return -1
	}
	if size == 0 {
		return 0
	}
	if err := src.ctx.Err(); err != nil {
		src.err = err
		return -1
	}

	// Alias the C buffer rather than copying: Read then writes straight into
	// the part buffer libminiocpp registered for RDMA.
	dst := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(size))

	// An io.Reader may return fewer bytes than asked for without being at EOF,
	// and libminiocpp reads a short return as end-of-part. Filling only
	// partially mid-stream would upload a short part and truncate the object
	// with no error anywhere, so keep pulling until the buffer is full or the
	// stream genuinely ends.
	total := 0
	empty := 0
	for total < len(dst) {
		n, err := src.reader.Read(dst[total:])
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			src.err = err
			return -1
		}
		if n > 0 {
			empty = 0
			continue
		}
		// A (0, nil) read is legal and does not mean EOF. Returning here would
		// hand back a short part, which libminiocpp reads as end-of-object --
		// the upload then succeeds with the rest of the data missing, which is
		// the one outcome this loop exists to prevent. Fail instead, once the
		// reader has clearly stalled rather than merely paused.
		empty++
		if empty >= maxConsecutiveEmptyReads {
			src.err = io.ErrNoProgress
			return -1
		}
	}
	return C.ssize_t(total)
}
