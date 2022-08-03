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

// util.go - define the utilities for api package of BOS service

package api

import (
	"bytes"
	net_http "net/http"
	"strings"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
)

const (
	METADATA_DIRECTIVE_COPY    = "copy"
	METADATA_DIRECTIVE_REPLACE = "replace"

	STORAGE_CLASS_STANDARD    = "STANDARD"
	STORAGE_CLASS_STANDARD_IA = "STANDARD_IA"
	STORAGE_CLASS_COLD        = "COLD"
	STORAGE_CLASS_ARCHIVE     = "ARCHIVE"

	FETCH_MODE_SYNC  = "sync"
	FETCH_MODE_ASYNC = "async"

	CANNED_ACL_PRIVATE           = "private"
	CANNED_ACL_PUBLIC_READ       = "public-read"
	CANNED_ACL_PUBLIC_READ_WRITE = "public-read-write"

	RAW_CONTENT_TYPE = "application/octet-stream"

	THRESHOLD_100_CONTINUE = 1 << 20 // add 100 continue header if body size bigger than 1MB

	STATUS_ENABLED  = "enabled"
	STATUS_DISABLED = "disabled"

	ENCRYPTION_AES256 = "AES256"

	RESTORE_TIER_STANDARD  = "Standard"  //标准取回对象
	RESTORE_TIER_EXPEDITED = "Expedited" //快速取回对象

	FORBID_OVERWRITE_FALSE = "false"
	FORBID_OVERWRITE_TRUE  = "true"
)

var DEFAULT_CNAME_LIKE_LIST = []string{
	".cdn.bcebos.com",
}

var VALID_STORAGE_CLASS_TYPE = map[string]int{
	STORAGE_CLASS_STANDARD:    0,
	STORAGE_CLASS_STANDARD_IA: 1,
	STORAGE_CLASS_COLD:        2,
	STORAGE_CLASS_ARCHIVE:     3,
}

var VALID_RESTORE_TIER = map[string]int{
	RESTORE_TIER_STANDARD:  1,
	RESTORE_TIER_EXPEDITED: 1,
}

var VALID_FORBID_OVERWRITE = map[string]int{
	FORBID_OVERWRITE_FALSE: 1,
	FORBID_OVERWRITE_TRUE:  1,
}

var (
	GET_OBJECT_ALLOWED_RESPONSE_HEADERS = map[string]struct{}{
		"ContentDisposition": {},
		"ContentType":        {},
		"ContentLanguage":    {},
		"Expires":            {},
		"CacheControl":       {},
		"ContentEncoding":    {},
	}
)

func getBucketUri(bucketName string) string {
	return bce.URI_PREFIX + bucketName
}

func getObjectUri(bucketName, objectName string) string {
	return bce.URI_PREFIX + bucketName + "/" + objectName
}

func getCnameUri(uri string) string {
	if len(uri) <= 0 {
		return uri
	}
	slash_index := strings.Index(uri[1:], "/")
	if slash_index == -1 {
		return bce.URI_PREFIX
	} else {
		return uri[slash_index+1:]
	}
}

func validMetadataDirective(val string) bool {
	if val == METADATA_DIRECTIVE_COPY || val == METADATA_DIRECTIVE_REPLACE {
		return true
	}
	return false
}

func validForbidOverwrite(val string) bool {
	if _, ok := VALID_FORBID_OVERWRITE[val]; ok {
		return true
	}
	return false
}

func validStorageClass(val string) bool {
	if _, ok := VALID_STORAGE_CLASS_TYPE[val]; ok {
		return true
	}
	return false
}

func validFetchMode(val string) bool {
	if val == FETCH_MODE_SYNC || val == FETCH_MODE_ASYNC {
		return true
	}
	return false
}

func validCannedAcl(val string) bool {
	if val == CANNED_ACL_PRIVATE ||
		val == CANNED_ACL_PUBLIC_READ ||
		val == CANNED_ACL_PUBLIC_READ_WRITE {
		return true
	}
	return false
}

func toHttpHeaderKey(key string) string {
	var result bytes.Buffer
	needToUpper := true
	for _, c := range []byte(key) {
		if needToUpper && (c >= 'a' && c <= 'z') {
			result.WriteByte(c - 32)
			needToUpper = false
		} else if c == '-' {
			result.WriteByte(c)
			needToUpper = true
		} else {
			result.WriteByte(c)
		}
	}
	return result.String()
}

func setOptionalNullHeaders(req *bce.BceRequest, args map[string]string) {
	for k, v := range args {
		if len(v) == 0 {
			continue
		}
		switch k {
		case http.CACHE_CONTROL:
			fallthrough
		case http.CONTENT_DISPOSITION:
			fallthrough
		case http.CONTENT_ENCODING:
			fallthrough
		case http.CONTENT_RANGE:
			fallthrough
		case http.CONTENT_MD5:
			fallthrough
		case http.CONTENT_TYPE:
			fallthrough
		case http.EXPIRES:
			fallthrough
		case http.LAST_MODIFIED:
			fallthrough
		case http.ETAG:
			fallthrough
		case http.BCE_OBJECT_TYPE:
			fallthrough
		case http.BCE_NEXT_APPEND_OFFSET:
			fallthrough
		case http.BCE_CONTENT_SHA256:
			fallthrough
		case http.BCE_CONTENT_CRC32:
			fallthrough
		case http.BCE_COPY_SOURCE_RANGE:
			fallthrough
		case http.BCE_COPY_SOURCE_IF_MATCH:
			fallthrough
		case http.BCE_COPY_SOURCE_IF_NONE_MATCH:
			fallthrough
		case http.BCE_COPY_SOURCE_IF_MODIFIED_SINCE:
			fallthrough
		case http.BCE_COPY_SOURCE_IF_UNMODIFIED_SINCE:
			req.SetHeader(k, v)
		}
	}
}

func setUserMetadata(req *bce.BceRequest, meta map[string]string) error {
	if meta == nil {
		return nil
	}
	for k, v := range meta {
		if len(k) == 0 {
			continue
		}
		if len(k)+len(v) > 32*1024 {
			return bce.NewBceClientError("MetadataTooLarge")
		}
		req.SetHeader(http.BCE_USER_METADATA_PREFIX+k, v)
	}
	return nil
}

func isCnameLikeHost(host string) bool {
	for _, suffix := range DEFAULT_CNAME_LIKE_LIST {
		if strings.HasSuffix(strings.ToLower(host), suffix) {
			return true
		}
	}
	return false
}

func SendRequest(cli bce.Client, req *bce.BceRequest, resp *bce.BceResponse) error {
	var (
		err        error
		need_retry bool
	)

	req.SetEndpoint(cli.GetBceClientConfig().Endpoint)
	origin_uri := req.Uri()
	// set uri for cname or cdn endpoint
	if cli.GetBceClientConfig().CnameEnabled || isCnameLikeHost(cli.GetBceClientConfig().Endpoint) {
		req.SetUri(getCnameUri(origin_uri))
	}

	if err = cli.SendRequest(req, resp); err != nil {
		if serviceErr, isServiceErr := err.(*bce.BceServiceError); isServiceErr {
			if serviceErr.StatusCode == net_http.StatusInternalServerError ||
				serviceErr.StatusCode == net_http.StatusBadGateway ||
				serviceErr.StatusCode == net_http.StatusServiceUnavailable ||
				(serviceErr.StatusCode == net_http.StatusBadRequest && serviceErr.Code == "Http400") {
				need_retry = true
			}
		}
		if _, isClientErr := err.(*bce.BceClientError); isClientErr {
			need_retry = true
		}
		// retry backup endpoint
		if need_retry && cli.GetBceClientConfig().BackupEndpoint != "" {
			req.SetEndpoint(cli.GetBceClientConfig().BackupEndpoint)
			if cli.GetBceClientConfig().CnameEnabled || isCnameLikeHost(cli.GetBceClientConfig().BackupEndpoint) {
				req.SetUri(getCnameUri(origin_uri))
			}
			if err = cli.SendRequest(req, resp); err != nil {
				return err
			}
		}
	}
	return err
}
