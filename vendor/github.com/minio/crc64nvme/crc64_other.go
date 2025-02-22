// Copyright (c) 2025 Minio Inc. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

//go:build (!amd64 || noasm || appengine || gccgo) && (!arm64 || noasm || appengine || gccgo)

package crc64nvme

var hasAsm = false
