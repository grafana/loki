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
	"encoding/json"
	"fmt"
	"net"
	net_http "net/http"
	"net/url"
	"strings"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
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

	TRAFFIC_LIMIT_MAX = 8 * (100 << 20) // 100M bit = 838860800
	TRAFFIC_LIMIT_MIN = 8 * (100 << 10) // 100K bit = 819200

	STATUS_ENABLED  = "enabled"
	STATUS_DISABLED = "disabled"

	ENCRYPTION_AES256 = "AES256"

	RESTORE_TIER_STANDARD  = "Standard"  //标准取回对象
	RESTORE_TIER_EXPEDITED = "Expedited" //快速取回对象

	FORBID_OVERWRITE_FALSE = "false"
	FORBID_OVERWRITE_TRUE  = "true"

	NAMESPACE_BUCKET   = "namespace"
	BOS_CONFIG_PREFIX  = "bos://"
	BOS_SHARE_ENDPOINT = "bos-share.baidubce.com"
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

func validObjectTagging(tagging string) (bool, string) {
	if len(tagging) > 4000 {
		return false, ""
	}
	encodeTagging := []string{}
	pair := strings.Split(tagging, "&")
	for _, p := range pair {
		kv := strings.Split(p, "=")
		if len(kv) != 2 {
			return false, ""
		}
		key := kv[0]
		value := kv[1]
		encodeKey := url.QueryEscape(key)
		encodeValue := url.QueryEscape(value)
		if len(encodeKey) > 128 || len(encodeValue) > 256 {
			return false, ""
		}
		encodeTagging = append(encodeTagging, encodeKey+"="+encodeValue)
	}
	return true, strings.Join(encodeTagging, "&")
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
	if isVirtualHost(host) {
		return true
	}
	return false
}

func SendRequest(cli bce.Client, req *bce.BceRequest, resp *bce.BceResponse, ctx *BosContext) error {
	var (
		err        error
		need_retry bool
	)
	setUriAndEndpoint(cli, req, ctx, cli.GetBceClientConfig().Endpoint)
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
			setUriAndEndpoint(cli, req, ctx, cli.GetBceClientConfig().BackupEndpoint)
			if err = cli.SendRequest(req, resp); err != nil {
				return err
			}
		}
	}
	return err
}

func SendRequestFromBytes(cli bce.Client, req *bce.BceRequest, resp *bce.BceResponse, ctx *BosContext, content []byte) error {
	var (
		err        error
		need_retry bool
	)
	setUriAndEndpoint(cli, req, ctx, cli.GetBceClientConfig().Endpoint)
	if err = cli.SendRequestFromBytes(req, resp, content); err != nil {
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
			setUriAndEndpoint(cli, req, ctx, cli.GetBceClientConfig().BackupEndpoint)
			if err = cli.SendRequestFromBytes(req, resp, content); err != nil {
				return err
			}
		}
	}
	return err
}

func isVirtualHost(host string) bool {
	domain := getDomainWithoutPort(host)
	arr := strings.Split(domain, ".")
	if len(arr) != 4 {
		return false
	}
	// bucket max length is 64
	if len(arr[0]) == 0 || len(arr[0]) > 64 {
		return false
	}
	if arr[2] != "bcebos" || arr[3] != "com" {
		return false
	}
	return true
}

func isIpHost(host string) bool {
	domain := getDomainWithoutPort(host)
	validIp := net.ParseIP(domain)
	return validIp != nil
}

func isBosHost(host string) bool {
	domain := getDomainWithoutPort(host)
	arr := strings.Split(domain, ".")
	if len(arr) != 3 {
		return false
	}
	if arr[1] != "bcebos" || arr[2] != "com" {
		return false
	}
	return true
}

func getDomainWithoutPort(host string) string {
	end := 0
	if end = strings.Index(host, ":"); end == -1 {
		end = len(host)
	}
	return host[:end]
}

func needCompatibleBucketAndEndpoint(bucket, endpoint string) bool {
	if bucket == "" {
		return false
	}
	if !isVirtualHost(endpoint) {
		return false
	}
	if strings.Split(endpoint, ".")[0] == bucket {
		return false
	}
	// bucket from sdk and from endpoint is different
	return true
}

// replace endpoint by bucket, only effective when two bucket are in same region, otherwise server return NoSuchBucket error
func replaceEndpointByBucket(bucket, endpoint string) string {
	arr := strings.Split(endpoint, ".")
	arr[0] = bucket
	return strings.Join(arr, ".")
}

func setUriAndEndpoint(cli bce.Client, req *bce.BceRequest, ctx *BosContext, endpoint string) {
	origin_uri := req.Uri()
	bucket := ctx.Bucket
	protocol := bce.DEFAULT_PROTOCOL
	// deal with protocal
	if strings.HasPrefix(endpoint, "https://") {
		protocol = bce.HTTPS_PROTOCAL
		endpoint = strings.TrimPrefix(endpoint, "https://")
	} else if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
	}
	// set uri, endpoint for cname, cdn, virtual host
	if cli.GetBceClientConfig().CnameEnabled || isCnameLikeHost(endpoint) {
		req.SetEndpoint(endpoint)
		// if virtual host endpoint and bucket is not empty, compatible bucket and endpoint
		if needCompatibleBucketAndEndpoint(bucket, endpoint) {
			req.SetEndpoint(replaceEndpointByBucket(bucket, endpoint))
		}
		req.SetUri(getCnameUri(origin_uri))
	} else if isIpHost(endpoint) {
		// set endpoint for ip host
		req.SetEndpoint(endpoint)
	} else if isBosHost(endpoint) {
		// endpoint is xx.bcebos.com, set endpoint depends on PathStyleEnable
		if bucket != "" && !ctx.PathStyleEnable {
			req.SetEndpoint(bucket + "." + endpoint)
			req.SetUri(getCnameUri(origin_uri))
		} else {
			req.SetEndpoint(endpoint)
		}
	} else {
		// user define custom endpoint
		req.SetEndpoint(endpoint)
	}
	req.SetProtocol(protocol)
}

func getDefaultContentType(object string) string {
	dot := strings.LastIndex(object, ".")
	if dot == -1 {
		return "application/octet-stream"
	}
	ext := object[dot:]
	mimeMap := util.GetMimeMap()
	if contentType, ok := mimeMap[ext]; ok {
		return contentType
	}
	return "application/octet-stream"

}

func ParseObjectTagResult(rawData []byte) (map[string]interface{}, error) {
	var data map[string]interface{}
	err := json.Unmarshal(rawData, &data)
	if err != nil {
		return nil, err
	}

	tagSet, ok := data["tagSet"].([]interface{})
	if !ok || len(tagSet) == 0 {
		return nil, fmt.Errorf("decode tagSet error")
	}

	tagInfoMap, ok := tagSet[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("decode tagInfo error")
	}

	tags, ok := tagInfoMap["tagInfo"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("decode tags error")
	}
	return tags, nil
}
