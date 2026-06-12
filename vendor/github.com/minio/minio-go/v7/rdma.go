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
// #cgo CXXFLAGS: --std=c++17 -DMINIO_CPP_RDMA
// #cgo LDFLAGS: -lminiocpp
// #include <stdlib.h>
// #include <miniocpp/c_api.h>
import "C"

import (
	"context"
	"errors"
	"fmt"
	"unsafe"
)

var ErrRDMANotConnected = errors.New("RDMA infrastructure not connected")

type rdmaClientHandle struct {
	cptr *C.miniocpp_client
}

func newRDMAClient(c *Client) (*rdmaClientHandle, error) {
	creds, err := c.credsProvider.Get()
	if err != nil {
		return nil, fmt.Errorf("RDMA: credentials: %w", err)
	}

	endpoint := C.CString(c.endpointURL.Host)
	defer C.free(unsafe.Pointer(endpoint))
	region := C.CString(c.region)
	defer C.free(unsafe.Pointer(region))
	accessKey := C.CString(creds.AccessKeyID)
	defer C.free(unsafe.Pointer(accessKey))
	secretKey := C.CString(creds.SecretAccessKey)
	defer C.free(unsafe.Pointer(secretKey))
	sessionToken := C.CString(creds.SessionToken)
	defer C.free(unsafe.Pointer(sessionToken))

	var useHTTPS C.int
	if c.secure {
		useHTTPS = 1
	}

	cptr := C.miniocpp_client_new(endpoint, region, accessKey, secretKey,
		sessionToken, useHTTPS)
	if cptr == nil {
		return nil, fmt.Errorf("RDMA: %s", lastRDMAError())
	}
	return &rdmaClientHandle{cptr: cptr}, nil
}

func (c *Client) putObjectRDMA(_ context.Context, bucketName, objectName string,
	opts PutObjectOptions,
) (UploadInfo, error) {
	h, err := c.rdma()
	if err != nil {
		return UploadInfo{}, err
	}

	bucketC := C.CString(bucketName)
	defer C.free(unsafe.Pointer(bucketC))
	objectC := C.CString(objectName)
	defer C.free(unsafe.Pointer(objectC))

	var etagBuf, checksumBuf [64]C.char
	n := C.miniocpp_put_object(h.cptr, bucketC, objectC,
		opts.RDMABuffer, C.size_t(opts.RDMABufferSize),
		nil, nil, &etagBuf[0], &checksumBuf[0])
	if n < 0 {
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

func (c *Client) getObjectRDMA(_ context.Context, bucketName, objectName string,
	opts GetObjectOptions,
) (int64, error) {
	h, err := c.rdma()
	if err != nil {
		return 0, err
	}

	bucketC := C.CString(bucketName)
	defer C.free(unsafe.Pointer(bucketC))
	objectC := C.CString(objectName)
	defer C.free(unsafe.Pointer(objectC))

	n := C.miniocpp_get_object(h.cptr, bucketC, objectC,
		opts.RDMABuffer, C.size_t(opts.RDMABufferSize), nil, nil)
	if n < 0 {
		return 0, fmt.Errorf("RDMA get: %s", lastRDMAError())
	}
	return int64(n), nil
}

func (c *Client) rdma() (*rdmaClientHandle, error) {
	c.rdmaOnce.Do(func() {
		c.rdmaHandle, c.rdmaInitErr = newRDMAClient(c)
	})
	return c.rdmaHandle, c.rdmaInitErr
}

func lastRDMAError() string {
	msg := C.miniocpp_last_error()
	if msg == nil {
		return "unknown error"
	}
	return C.GoString(msg)
}

// AlignedBuffer allocates a page-aligned buffer of n bytes for use as
// PutObjectOptions.RDMABuffer / GetObjectOptions.RDMABuffer. Release with
// FreeAlignedBuffer. Returns nil on allocation failure.
func AlignedBuffer(n int) unsafe.Pointer {
	return C.miniocpp_alloc_aligned(C.size_t(n))
}

// FreeAlignedBuffer releases a buffer from AlignedBuffer.
func FreeAlignedBuffer(p unsafe.Pointer) { C.miniocpp_free_aligned(p) }

// IsRDMAAvailable reports whether cuObj is connected to a cuObjServer.
func IsRDMAAvailable() bool { return C.miniocpp_rdma_available() != 0 }
