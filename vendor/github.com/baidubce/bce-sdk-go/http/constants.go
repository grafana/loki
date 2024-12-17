/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// constants.go - defines constants of the BCE http package including headers and methods

package http

// Constants of the supported HTTP methods for BCE
const (
	GET     = "GET"
	PUT     = "PUT"
	POST    = "POST"
	DELETE  = "DELETE"
	HEAD    = "HEAD"
	OPTIONS = "OPTIONS"
	PATCH   = "PATCH"
)

// Constants of the HTTP headers for BCE
const (
	// Standard HTTP Headers
	AUTHORIZATION       = "Authorization"
	CACHE_CONTROL       = "Cache-Control"
	CONTENT_DISPOSITION = "Content-Disposition"
	CONTENT_ENCODING    = "Content-Encoding"
	CONTENT_LANGUAGE    = "Content-Language"
	CONTENT_LENGTH      = "Content-Length"
	CONTENT_MD5         = "Content-Md5"
	CONTENT_RANGE       = "Content-Range"
	CONTENT_TYPE        = "Content-Type"
	DATE                = "Date"
	ETAG                = "Etag"
	EXPIRES             = "Expires"
	HOST                = "Host"
	LAST_MODIFIED       = "Last-Modified"
	LOCATION            = "Location"
	RANGE               = "Range"
	SERVER              = "Server"
	TRANSFER_ENCODING   = "Transfer-Encoding"
	USER_AGENT          = "User-Agent"

	// BCE Common HTTP Headers
	BCE_PREFIX               = "x-bce-"
	BCE_ACL                  = "x-bce-acl"
	BCE_GRANT_READ           = "x-bce-grant-read"
	BCE_GRANT_FULL_CONTROL   = "x-bce-grant-full-control"
	BCE_CONTENT_SHA256       = "x-bce-content-sha256"
	BCE_CONTENT_CRC32        = "x-bce-content-crc32"
	BCE_REQUEST_ID           = "x-bce-request-id"
	BCE_USER_METADATA_PREFIX = "x-bce-meta-"
	BCE_SECURITY_TOKEN       = "x-bce-security-token"
	BCE_DATE                 = "x-bce-date"
	BCE_TAG                  = "x-bce-tag-list"

	// BOS HTTP Headers
	BCE_COPY_METADATA_DIRECTIVE         = "x-bce-metadata-directive"
	BCE_COPY_TAGGING_DIRECTIVE          = "x-bce-tagging-directive"
	BCE_COPY_SOURCE                     = "x-bce-copy-source"
	BCE_COPY_SOURCE_IF_MATCH            = "x-bce-copy-source-if-match"
	BCE_COPY_SOURCE_IF_MODIFIED_SINCE   = "x-bce-copy-source-if-modified-since"
	BCE_COPY_SOURCE_IF_NONE_MATCH       = "x-bce-copy-source-if-none-match"
	BCE_COPY_SOURCE_IF_UNMODIFIED_SINCE = "x-bce-copy-source-if-unmodified-since"
	BCE_COPY_SOURCE_RANGE               = "x-bce-copy-source-range"
	BCE_DEBUG_ID                        = "x-bce-debug-id"
	BCE_OBJECT_TYPE                     = "x-bce-object-type"
	BCE_NEXT_APPEND_OFFSET              = "x-bce-next-append-offset"
	BCE_STORAGE_CLASS                   = "x-bce-storage-class"
	BCE_PROCESS                         = "x-bce-process"
	BCE_RESTORE_TIER                    = "x-bce-restore-tier"
	BCE_RESTORE_DAYS                    = "x-bce-restore-days"
	BCE_RESTORE                         = "x-bce-restore"
	BCE_FORBID_OVERWRITE                = "x-bce-forbid-overwrite"
	BCE_SYMLINK_TARGET                  = "x-bce-symlink-target"
	BCE_SYMLINK_BUCKET                  = "x-bce-symlink-bucket"
	BCE_TRAFFIC_LIMIT                   = "x-bce-traffic-limit"
	BCE_BUCKET_TYPE                     = "x-bce-bucket-type"
	BCE_OBJECT_TAGGING                  = "x-bce-tagging"
	BCE_FETCH_CALLBACK_ADDRESS          = "x-bce-callback-address"
)
