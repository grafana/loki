/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2024-2026 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * SPDX-License-Identifier: Apache-2.0
 */

package minio

import "net/http"

// rdmaSkipCertVerify reports whether the RDMA control plane should skip server
// certificate verification.
//
// RDMA transfers are driven by libminiocpp, which issues its own S3 requests
// instead of going through this package's http.Client. A TLSClientConfig set
// here therefore governs the HTTP path alone: the C++ client verifies against
// the OpenSSL trust store and cannot see it. Left unbridged, RDMA fails against
// exactly the self-signed and private-CA endpoints where the caller's plain S3
// path succeeds, and no flag rescues it.
//
// Derived from the transport rather than configured separately, so a caller's
// existing --insecure keeps working with no change at the call site. Only
// *http.Transport can be inspected; a caller that wraps its transport in
// another RoundTripper still gets verification. A private CA is named by
// SSL_CERT_FILE in the environment, which libminiocpp reads per request.
func rdmaSkipCertVerify(hc *http.Client, secure bool) bool {
	if !secure || hc == nil {
		return false
	}
	tr, ok := hc.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		return false
	}
	return tr.TLSClientConfig.InsecureSkipVerify
}
