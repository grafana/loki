//go:build rdma

/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2024-2026 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * SPDX-License-Identifier: Apache-2.0
 */

package minio

// #cgo CFLAGS: -DMINIO_CPP_RDMA
// #include <stdlib.h>
// #include <sys/types.h>
// #include <miniocpp/c_api.h>
//
// // minioRDMAReadGo is defined by cgo from the //export in rdma_callback.go;
// // the file carrying //export may not hold a definition like the one below,
// // which is why the trampoline lives here. It has external linkage on
// // purpose: Go stores its address, and a static function is not resolvable
// // from where the linker needs it.
// extern ssize_t minioRDMAReadGo(void *userdata, char *buf, size_t size);
// ssize_t minioRDMAReadTrampoline(void *userdata, char *buf, size_t size) {
//     return minioRDMAReadGo(userdata, buf, size);
// }
import "C"

import (
	"context"
	"fmt"
	"io"
	"runtime/cgo"
	"unsafe"
)

// putObjectRDMAStream uploads `reader` over RDMA without staging the whole
// object in pinned memory.
//
// libminiocpp drives it: one page-aligned part buffer, registered once for the
// whole upload, then read-part / RDMA UploadPart / repeat, with the per-part
// CRC64NVME the server requires on that path. Pinned memory is therefore one
// part, not one object, and an object past the 4 GiB an RDMA descriptor can
// address uploads fine.
//
// This is a multipart upload, so the ETag carries the usual `-N` suffix rather
// than being an MD5 of the object.
func (c *Client) putObjectRDMAStream(ctx context.Context, bucketName, objectName string,
	reader io.Reader, size int64, _ PutObjectOptions,
) (UploadInfo, error) {
	if reader == nil {
		return UploadInfo{}, fmt.Errorf("RDMA put: a streaming upload needs a reader")
	}
	if size < 0 {
		return UploadInfo{}, fmt.Errorf(
			"RDMA put: a streaming RDMA upload needs a known object size, got %d", size)
	}

	h, err := c.rdma()
	if err != nil {
		return UploadInfo{}, err
	}

	src := &rdmaStreamSource{ctx: ctx, reader: reader}
	handle := cgo.NewHandle(src)
	defer handle.Delete()

	// Hand the handle over in C memory rather than as a Go pointer: a
	// cgo.Handle is a uintptr, and converting one straight to unsafe.Pointer
	// is the misuse go vet rejects.
	ud := C.malloc(C.size_t(unsafe.Sizeof(cgo.Handle(0))))
	if ud == nil {
		return UploadInfo{}, fmt.Errorf("RDMA put: cannot allocate callback context")
	}
	defer C.free(ud)
	*(*cgo.Handle)(ud) = handle

	bucketC := C.CString(bucketName)
	defer C.free(unsafe.Pointer(bucketC))
	objectC := C.CString(objectName)
	defer C.free(unsafe.Pointer(objectC))

	var etagBuf, checksumBuf [64]C.char
	// buf is NULL on purpose: libminiocpp allocates and registers the part
	// buffer itself. The caller's RDMABuffer is what selects this path, not
	// the staging area -- passing it here would ask for a single-shot
	// transfer of one buffer instead of a stream.
	n := C.miniocpp_put_object(h.cptr, bucketC, objectC,
		nil, C.size_t(size),
		C.miniocpp_read_cb(C.minioRDMAReadTrampoline), ud,
		&etagBuf[0], &checksumBuf[0])
	if n < 0 {
		// A read error on the caller's stream reaches C only as "failed", so
		// report the reason we kept rather than libminiocpp's generic message.
		if src.err != nil {
			return UploadInfo{}, fmt.Errorf("RDMA put: reading source: %w", src.err)
		}
		return UploadInfo{}, fmt.Errorf("RDMA put: %s", lastRDMAError())
	}

	return UploadInfo{
		Bucket:            bucketName,
		Key:               objectName,
		Size:              int64(n),
		ETag:              C.GoString(&etagBuf[0]),
		ChecksumCRC64NVME: C.GoString(&checksumBuf[0]),
	}, nil
}
