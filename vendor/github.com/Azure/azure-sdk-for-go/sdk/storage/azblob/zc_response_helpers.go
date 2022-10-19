//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

// GetHTTPHeaders returns the user-modifiable properties for this blob.
func (bgpr BlobGetPropertiesResponse) GetHTTPHeaders() BlobHTTPHeaders {
	return BlobHTTPHeaders{
		BlobContentType:        bgpr.ContentType,
		BlobContentEncoding:    bgpr.ContentEncoding,
		BlobContentLanguage:    bgpr.ContentLanguage,
		BlobContentDisposition: bgpr.ContentDisposition,
		BlobCacheControl:       bgpr.CacheControl,
		BlobContentMD5:         bgpr.ContentMD5,
	}
}

///////////////////////////////////////////////////////////////////////////////

// GetHTTPHeaders returns the user-modifiable properties for this blob.
func (r BlobDownloadResponse) GetHTTPHeaders() BlobHTTPHeaders {
	return BlobHTTPHeaders{
		BlobContentType:        r.ContentType,
		BlobContentEncoding:    r.ContentEncoding,
		BlobContentLanguage:    r.ContentLanguage,
		BlobContentDisposition: r.ContentDisposition,
		BlobCacheControl:       r.CacheControl,
		BlobContentMD5:         r.ContentMD5,
	}
}

///////////////////////////////////////////////////////////////////////////////
