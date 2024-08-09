//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package internal

import (
	"errors"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
)

var errMissingImport = errors.New("import github.com/Azure/azure-sdk-for-go/sdk/azidentity/cache to enable persistent caching")

// NewCache constructs a persistent token cache when "o" isn't nil. Applications that intend to
// use a persistent cache must first import the cache module, which will replace this function
// with a platform-specific implementation.
var NewCache = func(o *TokenCachePersistenceOptions, enableCAE bool) (cache.ExportReplace, error) {
	if o == nil {
		return nil, nil
	}
	return nil, errMissingImport
}

// CacheFilePath returns the path to the cache file for the given name.
// Defining it in this package makes it available to azidentity tests.
var CacheFilePath = func(name string) (string, error) {
	return "", errMissingImport
}
