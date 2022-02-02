// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

//nolint
const (
	SASVersion = "2019-12-12"

	headerAuthorization           = "Authorization"
	headerXmsDate                 = "x-ms-date"
	headerContentLength           = "Content-Length"
	headerContentEncoding         = "Content-Encoding"
	headerContentLanguage         = "Content-Language"
	headerContentType             = "Content-Type"
	headerContentMD5              = "Content-MD5"
	headerIfModifiedSince         = "If-Modified-Since"
	headerIfMatch                 = "If-Match"
	headerIfNoneMatch             = "If-None-Match"
	headerIfUnmodifiedSince       = "If-Unmodified-Since"
	headerRange                   = "Range"
	headerDate                    = "Date"
	headerXmsVersion              = "x-ms-version"
	headerAcceptCharset           = "Accept-Charset"
	headerDataServiceVersion      = "DataServiceVersion"
	headerMaxDataServiceVersion   = "MaxDataServiceVersion"
	headerContentTransferEncoding = "Content-Transfer-Encoding"

	etagOData = "odata.etag"
	rfc3339   = "2006-01-02T15:04:05.9999999Z"
	timestamp = "Timestamp"
	etag      = "ETag"

	tokenScope = "https://storage.azure.com/.default"
)
