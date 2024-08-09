//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package internal

// TokenCachePersistenceOptions contains options for persistent token caching
type TokenCachePersistenceOptions struct {
	// AllowUnencryptedStorage controls whether the cache should fall back to storing its data in plain text
	// when encryption isn't possible. Setting this true doesn't disable encryption. The cache always attempts
	// encryption before falling back to plaintext storage.
	AllowUnencryptedStorage bool

	// Name identifies the cache. Set this to isolate data from other applications.
	Name string
}
