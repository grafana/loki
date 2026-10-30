//go:build !rdma

/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2024-2026 MinIO, Inc.
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package minio

import (
	"context"
	"errors"
	"unsafe"
)

// ErrRDMANotCompiled is returned when RDMA dispatch is requested but the
// binary was built without -tags=rdma.
var ErrRDMANotCompiled = errors.New("RDMA support not compiled in (build with -tags=rdma)")

// rdmaClientHandle is the no-tag placeholder type so Client (in api.go)
// has a stable shape regardless of build tags. The rdma.go variant
// defines the real struct.
type rdmaClientHandle struct{} //nolint:unused

func (c *Client) putObjectRDMA(_ context.Context, _, _ string, _ PutObjectOptions) (UploadInfo, error) {
	return UploadInfo{}, ErrRDMANotCompiled
}

func (c *Client) getObjectRDMA(_ context.Context, _, _ string, _ GetObjectOptions) (int64, error) {
	return 0, ErrRDMANotCompiled
}

// AlignedBuffer is unavailable without -tags=rdma; returns nil.
func AlignedBuffer(_ int) unsafe.Pointer { return nil }

// FreeAlignedBuffer is a no-op without -tags=rdma.
func FreeAlignedBuffer(_ unsafe.Pointer) {}

// IsRDMAAvailable always returns false without -tags=rdma.
func IsRDMAAvailable() bool { return false }
