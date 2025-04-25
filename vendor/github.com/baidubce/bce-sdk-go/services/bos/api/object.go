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

// object.go - the object APIs definition supported by the BOS service

package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strconv"
	"strings"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
)

// PutObject - put the object from the string or the stream
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
//     - body: the input content of the object
//     - args: the optional arguments of this api
// RETURNS:
//     - string: the etag of the object
//     - error: nil if ok otherwise the specific error
func PutObject(cli bce.Client, bucket, object string, body *bce.Body,
	args *PutObjectArgs, ctx *BosContext) (string, *PutObjectResult, error) {
	req := &bce.BceRequest{}
	NeedReturnCallback := false
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	ctx.Bucket = bucket
	if body == nil {
		return "", nil, bce.NewBceClientError("PutObject body should not be emtpy")
	}
	if body.Size() >= THRESHOLD_100_CONTINUE {
		req.SetHeader("Expect", "100-continue")
	}
	req.SetBody(body)

	// Optional arguments settings
	if args != nil {
		setOptionalNullHeaders(req, map[string]string{
			http.CACHE_CONTROL:       args.CacheControl,
			http.CONTENT_DISPOSITION: args.ContentDisposition,
			http.CONTENT_TYPE:        args.ContentType,
			http.EXPIRES:             args.Expires,
			http.BCE_CONTENT_SHA256:  args.ContentSha256,
			http.BCE_CONTENT_CRC32:   args.ContentCrc32,
		})
		if args.ContentLength > 0 {
			// User specified Content-Length can be smaller than the body size, so the body should
			// be reset. The `net/http.Client' does not support the Content-Length bigger than the
			// body size.
			if args.ContentLength > body.Size() {
				return "", nil, bce.NewBceClientError(fmt.Sprintf("ContentLength %d is bigger than body size %d", args.ContentLength, body.Size()))
			}
			body, err := bce.NewBodyFromSizedReader(body.Stream(), args.ContentLength)
			if err != nil {
				return "", nil, bce.NewBceClientError(err.Error())
			}
			req.SetHeader(http.CONTENT_LENGTH, fmt.Sprintf("%d", args.ContentLength))
			req.SetBody(body) // re-assign body
		}

		//set traffic-limit
		if args.TrafficLimit > 0 {
			if args.TrafficLimit > TRAFFIC_LIMIT_MAX || args.TrafficLimit < TRAFFIC_LIMIT_MIN {
				return "", nil, bce.NewBceClientError(fmt.Sprintf("TrafficLimit must between %d ~ %d, current value:%d", TRAFFIC_LIMIT_MIN, TRAFFIC_LIMIT_MAX, args.TrafficLimit))
			}
			req.SetHeader(http.BCE_TRAFFIC_LIMIT, fmt.Sprintf("%d", args.TrafficLimit))
		}

		// Reset the contentMD5 if set by user
		if len(args.ContentMD5) != 0 {
			req.SetHeader(http.CONTENT_MD5, args.ContentMD5)
		}

		if validStorageClass(args.StorageClass) {
			req.SetHeader(http.BCE_STORAGE_CLASS, args.StorageClass)
		} else {
			if len(args.StorageClass) != 0 {
				return "", nil, bce.NewBceClientError("invalid storage class value: " +
					args.StorageClass)
			}
		}

		if err := setUserMetadata(req, args.UserMeta); err != nil {
			return "", nil, err
		}

		if len(args.Process) != 0 {
			req.SetHeader(http.BCE_PROCESS, args.Process)
			if strings.HasPrefix(args.Process, "callback") {
				NeedReturnCallback = true
			}
		}
		if len(args.CannedAcl) != 0 {
			if validCannedAcl(args.CannedAcl) {
				req.SetHeader(http.BCE_ACL, args.CannedAcl)
			}
		}
		if len(args.ObjectTagging) != 0 {
			if ok, encodeTagging := validObjectTagging(args.ObjectTagging); ok {
				req.SetHeader(http.BCE_OBJECT_TAGGING, encodeTagging)
			}
		}
	}
	// add content-type if not assigned by user
	if req.Header(http.CONTENT_TYPE) == "" {
		req.SetHeader(http.CONTENT_TYPE, getDefaultContentType(object))
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return "", nil, err
	}
	if resp.IsFail() {
		return "", nil, resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	if NeedReturnCallback {
		jsonBody := &PutObjectResult{}
		if err := resp.ParseJsonBody(jsonBody); err != nil {
			return "", nil, err
		}
		return strings.Trim(resp.Header(http.ETAG), "\""), jsonBody, nil
	}
	return strings.Trim(resp.Header(http.ETAG), "\""), nil, nil
}

// CopyObject - copy one object to a new object with new bucket and/or name. It can alse set the
// metadata of the object with the same source and target.
//
// PARAMS:
//     - cli: the client object which can perform sending request
//     - bucket: the bucket name of the target object
//     - object: the name of the target object
//     - source: the source object uri
//     - *CopyObjectArgs: the optional input args for copying object
// RETURNS:
//     - *CopyObjectResult: the result object which contains etag and lastmodified
//     - error: nil if ok otherwise the specific error
func CopyObject(cli bce.Client, bucket, object, source string,
	args *CopyObjectArgs, ctx *BosContext) (*CopyObjectResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	ctx.Bucket = bucket
	if len(source) == 0 {
		return nil, bce.NewBceClientError("copy source should not be null")
	}
	req.SetHeader(http.BCE_COPY_SOURCE, util.UriEncode(source, false))

	// Optional arguments settings
	if args != nil {
		setOptionalNullHeaders(req, map[string]string{
			http.CACHE_CONTROL:                       args.CacheControl,
			http.CONTENT_DISPOSITION:                 args.ContentDisposition,
			http.CONTENT_ENCODING:                    args.ContentEncoding,
			http.CONTENT_RANGE:                       args.ContentRange,
			http.CONTENT_TYPE:                        args.ContentType,
			http.EXPIRES:                             args.Expires,
			http.LAST_MODIFIED:                       args.LastModified,
			http.ETAG:                                args.ETag,
			http.CONTENT_MD5:                         args.ContentMD5,
			http.BCE_CONTENT_SHA256:                  args.ContentSha256,
			http.BCE_OBJECT_TYPE:                     args.ObjectType,
			http.BCE_NEXT_APPEND_OFFSET:              args.NextAppendOffset,
			http.BCE_COPY_SOURCE_IF_MATCH:            args.IfMatch,
			http.BCE_COPY_SOURCE_IF_NONE_MATCH:       args.IfNoneMatch,
			http.BCE_COPY_SOURCE_IF_MODIFIED_SINCE:   args.IfModifiedSince,
			http.BCE_COPY_SOURCE_IF_UNMODIFIED_SINCE: args.IfUnmodifiedSince,
		})
		if args.ContentLength != 0 {
			req.SetHeader(http.CONTENT_LENGTH, fmt.Sprintf("%d", args.ContentLength))
		}
		if validMetadataDirective(args.MetadataDirective) {
			req.SetHeader(http.BCE_COPY_METADATA_DIRECTIVE, args.MetadataDirective)
		} else {
			if len(args.MetadataDirective) != 0 {
				return nil, bce.NewBceClientError(
					"invalid metadata directive value: " + args.MetadataDirective)
			}
		}
		if validMetadataDirective(args.TaggingDirective) {
			req.SetHeader(http.BCE_COPY_TAGGING_DIRECTIVE, args.TaggingDirective)
		}
		if len(args.ObjectTagging) != 0 {
			if ok, encodeTagging := validObjectTagging(args.ObjectTagging); ok {
				req.SetHeader(http.BCE_OBJECT_TAGGING, encodeTagging)
			}
		}
		if validStorageClass(args.StorageClass) {
			req.SetHeader(http.BCE_STORAGE_CLASS, args.StorageClass)
		} else {
			if len(args.StorageClass) != 0 {
				return nil, bce.NewBceClientError("invalid storage class value: " +
					args.StorageClass)
			}
		}

		//set traffic-limit
		if args.TrafficLimit > 0 {
			if args.TrafficLimit > TRAFFIC_LIMIT_MAX || args.TrafficLimit < TRAFFIC_LIMIT_MIN {
				return nil, bce.NewBceClientError(fmt.Sprintf("TrafficLimit must between %d ~ %d, current value:%d", TRAFFIC_LIMIT_MIN, TRAFFIC_LIMIT_MAX, args.TrafficLimit))
			}
			req.SetHeader(http.BCE_TRAFFIC_LIMIT, fmt.Sprintf("%d", args.TrafficLimit))
		}

		if validCannedAcl(args.CannedAcl) {
			req.SetHeader(http.BCE_ACL, args.CannedAcl)
		}

		if err := setUserMetadata(req, args.UserMeta); err != nil {
			return nil, err
		}
	}

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	jsonBody := &CopyObjectResult{}
	if err := resp.ParseJsonBody(jsonBody); err != nil {
		return nil, err
	}
	if resp.Header(http.BCE_VERSION_ID) != "" {
		jsonBody.VersionId = resp.Header(http.BCE_VERSION_ID)
	}
	return jsonBody, nil
}

// GetObject - get the object content with range and response-headers-specified support
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
//     - args: the optional args in querysring
//     - ranges: the optional range start and end to get the given object
// RETURNS:
//     - *GetObjectResult: the output content result of the object
//     - error: nil if ok otherwise the specific error
func GetObject(cli bce.Client, bucket, object string, ctx *BosContext, args map[string]string, // nolint:gocyclo
	ranges ...int64) (*GetObjectResult, error) {

	if object == "" {
		err := fmt.Errorf("Get Object don't accept \"\" as a parameter")
		return nil, err
	}
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.GET)
	ctx.Bucket = bucket
	// Optional arguments settings
	if args != nil {
		for k, v := range args {
			if _, ok := GET_OBJECT_ALLOWED_RESPONSE_HEADERS[k]; ok {
				req.SetParam("response"+k, v)
			}
			if strings.HasPrefix(k, http.BCE_PREFIX) {
				req.SetParam(k, v)
			}
		}
	}
	if len(ranges) != 0 {
		rangeStr := "bytes="
		if len(ranges) == 1 {
			rangeStr += fmt.Sprintf("%d", ranges[0]) + "-"
		} else {
			rangeStr += fmt.Sprintf("%d", ranges[0]) + "-" + fmt.Sprintf("%d", ranges[1])
		}
		req.SetHeader("Range", rangeStr)
	}

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	headers := resp.Headers()
	result := &GetObjectResult{}
	if val, ok := headers[http.CACHE_CONTROL]; ok {
		result.CacheControl = val
	}
	if val, ok := headers[http.CONTENT_DISPOSITION]; ok {
		result.ContentDisposition = val
	}
	if val, ok := headers[http.CONTENT_LENGTH]; ok {
		if length, err := strconv.ParseInt(val, 10, 64); err == nil {
			result.ContentLength = length
		}
	}
	if val, ok := headers[http.CONTENT_RANGE]; ok {
		result.ContentRange = val
	}
	if val, ok := headers[http.CONTENT_TYPE]; ok {
		result.ContentType = val
	}
	if val, ok := headers[http.CONTENT_MD5]; ok {
		result.ContentMD5 = val
	}
	if val, ok := headers[http.EXPIRES]; ok {
		result.Expires = val
	}
	if val, ok := headers[http.LAST_MODIFIED]; ok {
		result.LastModified = val
	}
	if val, ok := headers[http.ETAG]; ok {
		result.ETag = strings.Trim(val, "\"")
	}
	if val, ok := headers[http.CONTENT_LANGUAGE]; ok {
		result.ContentLanguage = val
	}
	if val, ok := headers[http.CONTENT_ENCODING]; ok {
		result.ContentEncoding = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_CONTENT_SHA256)]; ok {
		result.ContentSha256 = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_CONTENT_CRC32)]; ok {
		result.ContentCrc32 = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_STORAGE_CLASS)]; ok {
		result.StorageClass = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_VERSION_ID)]; ok {
		result.VersionId = val
	}
	bcePrefix := toHttpHeaderKey(http.BCE_USER_METADATA_PREFIX)
	for k, v := range headers {
		if strings.Index(k, bcePrefix) == 0 {
			if result.UserMeta == nil {
				result.UserMeta = make(map[string]string)
			}
			result.UserMeta[k[len(bcePrefix):]] = v
		}
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_OBJECT_TYPE)]; ok {
		result.ObjectType = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_NEXT_APPEND_OFFSET)]; ok {
		result.NextAppendOffset = val
	}
	result.Body = resp.Body()
	return result, nil
}

// GetObjectMeta - get the meta data of the given object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
// RETURNS:
//     - *GetObjectMetaResult: the result of this api
//     - error: nil if ok otherwise the specific error
func GetObjectMeta(cli bce.Client, bucket, object string, ctx *BosContext) (*GetObjectMetaResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.HEAD)
	ctx.Bucket = bucket
	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	headers := resp.Headers()
	result := &GetObjectMetaResult{}
	if val, ok := headers[http.CACHE_CONTROL]; ok {
		result.CacheControl = val
	}
	if val, ok := headers[http.CONTENT_DISPOSITION]; ok {
		result.ContentDisposition = val
	}
	if val, ok := headers[http.CONTENT_LENGTH]; ok {
		if length, err := strconv.ParseInt(val, 10, 64); err == nil {
			result.ContentLength = length
		}
	}
	if val, ok := headers[http.CONTENT_RANGE]; ok {
		result.ContentRange = val
	}
	if val, ok := headers[http.CONTENT_TYPE]; ok {
		result.ContentType = val
	}
	if val, ok := headers[http.CONTENT_MD5]; ok {
		result.ContentMD5 = val
	}
	if val, ok := headers[http.EXPIRES]; ok {
		result.Expires = val
	}
	if val, ok := headers[http.LAST_MODIFIED]; ok {
		result.LastModified = val
	}
	if val, ok := headers[http.ETAG]; ok {
		result.ETag = strings.Trim(val, "\"")
	}
	if val, ok := headers[http.CONTENT_ENCODING]; ok {
		result.ContentEncoding = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_CONTENT_SHA256)]; ok {
		result.ContentSha256 = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_CONTENT_CRC32)]; ok {
		result.ContentCrc32 = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_STORAGE_CLASS)]; ok {
		result.StorageClass = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_RESTORE)]; ok {
		result.BceRestore = val
	}
	if val, ok := headers[http.BCE_OBJECT_TYPE]; ok {
		result.BceObjectType = val
	}
	if val, ok := headers[http.BCE_VERSION_ID]; ok {
		result.VersionId = val
	}
	bcePrefix := toHttpHeaderKey(http.BCE_USER_METADATA_PREFIX)
	for k, v := range headers {
		if strings.Index(k, bcePrefix) == 0 {
			if result.UserMeta == nil {
				result.UserMeta = make(map[string]string)
			}
			result.UserMeta[k[len(bcePrefix):]] = v
		}
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_OBJECT_TYPE)]; ok {
		result.ObjectType = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_NEXT_APPEND_OFFSET)]; ok {
		result.NextAppendOffset = val
	}
	defer func() { resp.Body().Close() }()
	return result, nil
}

// SelectObject - select the object content
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
//     - args: the optional arguments to perform the select operation
// RETURNS:
//     - *SelectObjectResult: the output select content result of the object
//     - error: nil if ok otherwise the specific error
func SelectObject(cli bce.Client, bucket, object string, args *SelectObjectArgs, ctx *BosContext) (*SelectObjectResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.POST)
	req.SetParam("select", "")
	req.SetParam("type", args.SelectType)
	ctx.Bucket = bucket
	jsonBytes, jsonErr := json.Marshal(args)
	if jsonErr != nil {
		return nil, jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return nil, err
	}
	req.SetBody(body)

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}

	result := &SelectObjectResult{}

	result.Body = resp.Body()
	return result, nil
}

// FetchObject - fetch the object by the given url and store it to a bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name to store the object
//     - object: the name of the object to be stored
//     - source: the source url to fetch
//     - args: the optional arguments to perform the fetch operation
// RETURNS:
//     - *FetchObjectArgs: the result of this api
//     - error: nil if ok otherwise the specific error
func FetchObject(cli bce.Client, bucket, object, source string,
	args *FetchObjectArgs, ctx *BosContext) (*FetchObjectResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.POST)
	req.SetParam("fetch", "")
	ctx.Bucket = bucket
	if len(source) == 0 {
		return nil, bce.NewBceClientError("invalid fetch source value: " + source)
	}
	req.SetHeader(http.BCE_PREFIX+"fetch-source", source)

	// Optional arguments settings
	if args != nil {
		if validFetchMode(args.FetchMode) {
			req.SetHeader(http.BCE_PREFIX+"fetch-mode", args.FetchMode)
		} else {
			if len(args.FetchMode) != 0 {
				return nil, bce.NewBceClientError("invalid fetch mode value: " + args.FetchMode)
			}
		}
		if validStorageClass(args.StorageClass) {
			req.SetHeader(http.BCE_STORAGE_CLASS, args.StorageClass)
		} else {
			if len(args.StorageClass) != 0 {
				return nil, bce.NewBceClientError("invalid storage class value: " +
					args.StorageClass)
			}
		}
		if len(args.FetchCallBackAddress) != 0 {
			req.SetHeader(http.BCE_FETCH_CALLBACK_ADDRESS, args.FetchCallBackAddress)
		}
	}

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	jsonBody := &FetchObjectResult{}
	if err := resp.ParseJsonBody(jsonBody); err != nil {
		return nil, err
	}
	return jsonBody, nil
}

// AppendObject - append the given content to a new or existed object which is appendable
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
//     - content: the content to be appended
//     - args: the optional arguments to perform the append operation
// RETURNS:
//     - *AppendObjectResult: the result status for this api
//     - error: nil if ok otherwise the specific error
func AppendObject(cli bce.Client, bucket, object string, content *bce.Body,
	args *AppendObjectArgs, ctx *BosContext) (*AppendObjectResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.POST)
	req.SetParam("append", "")
	ctx.Bucket = bucket
	if content == nil {
		return nil, bce.NewBceClientError("AppendObject body should not be emtpy")
	}
	if content.Size() >= THRESHOLD_100_CONTINUE {
		req.SetHeader("Expect", "100-continue")
	}
	req.SetBody(content)

	// Optional arguments settings
	if args != nil {
		if args.Offset < 0 {
			return nil, bce.NewBceClientError(
				fmt.Sprintf("invalid append offset value: %d", args.Offset))
		}
		if args.Offset > 0 {
			req.SetParam("offset", fmt.Sprintf("%d", args.Offset))
		}
		setOptionalNullHeaders(req, map[string]string{
			http.CACHE_CONTROL:       args.CacheControl,
			http.CONTENT_DISPOSITION: args.ContentDisposition,
			http.CONTENT_MD5:         args.ContentMD5,
			http.CONTENT_TYPE:        args.ContentType,
			http.EXPIRES:             args.Expires,
			http.BCE_CONTENT_SHA256:  args.ContentSha256,
			http.BCE_CONTENT_CRC32:   args.ContentCrc32,
		})

		if validStorageClass(args.StorageClass) {
			req.SetHeader(http.BCE_STORAGE_CLASS, args.StorageClass)
		} else {
			if len(args.StorageClass) != 0 {
				return nil, bce.NewBceClientError("invalid storage class value: " +
					args.StorageClass)
			}
		}
		if err := setUserMetadata(req, args.UserMeta); err != nil {
			return nil, err
		}
		//set traffic-limit
		if args.TrafficLimit > 0 {
			if args.TrafficLimit > TRAFFIC_LIMIT_MAX || args.TrafficLimit < TRAFFIC_LIMIT_MIN {
				return nil, bce.NewBceClientError(fmt.Sprintf("TrafficLimit must between %d ~ %d, current value:%d", TRAFFIC_LIMIT_MIN, TRAFFIC_LIMIT_MAX, args.TrafficLimit))
			}
			req.SetHeader(http.BCE_TRAFFIC_LIMIT, fmt.Sprintf("%d", args.TrafficLimit))
		}
	}

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	headers := resp.Headers()
	result := &AppendObjectResult{}
	if val, ok := headers[http.CONTENT_MD5]; ok {
		result.ContentMD5 = val
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_NEXT_APPEND_OFFSET)]; ok {
		nextOffset, offsetErr := strconv.ParseInt(val, 10, 64)
		if offsetErr != nil {
			nextOffset = content.Size()
		}
		result.NextAppendOffset = nextOffset
	} else {
		result.NextAppendOffset = content.Size()
	}
	if val, ok := headers[toHttpHeaderKey(http.BCE_CONTENT_CRC32)]; ok {
		result.ContentCrc32 = val
	}
	if val, ok := headers[http.ETAG]; ok {
		result.ETag = strings.Trim(val, "\"")
	}
	defer func() { resp.Body().Close() }()
	return result, nil
}

// DeleteObject - delete the given object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object to be deleted
//     - object: the name of the object
// RETURNS:
//     - error: nil if ok otherwise the specific error
func DeleteObject(cli bce.Client, bucket, object, versionId string, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.DELETE)
	if versionId != "" {
		req.SetParam("versionId", versionId)
	}
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// DeleteMultipleObjects - delete the given objects within a single http request
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the objects to be deleted
//     - objectListStream: the objects list to be delete with json format
// RETURNS:
//     - *DeleteMultipleObjectsResult: the objects failed to delete
//     - error: nil if ok otherwise the specific error
func DeleteMultipleObjects(cli bce.Client, bucket string,
	objectListStream *bce.Body, ctx *BosContext) (*DeleteMultipleObjectsResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.POST)
	req.SetParam("delete", "")
	req.SetHeader(http.CONTENT_TYPE, "application/json; charset=utf-8")
	ctx.Bucket = bucket
	if objectListStream == nil {
		return nil, bce.NewBceClientError("DeleteMultipleObjects body should not be emtpy")
	}
	if objectListStream.Size() >= THRESHOLD_100_CONTINUE {
		req.SetHeader("Expect", "100-continue")
	}
	req.SetBody(objectListStream)

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	jsonBody := &DeleteMultipleObjectsResult{}

	if resp.Header(http.CONTENT_LENGTH) == "0" {
		resp.Body().Close()
		return jsonBody, nil
	}
	if err := resp.ParseJsonBody(jsonBody); err != nil {
		return nil, err
	}
	return jsonBody, nil
}

// GeneratePresignedUrl - generate an authorization url with expire time and optional arguments
//
// PARAMS:
//     - conf: the client configuration
//     - signer: the client signer object to generate the authorization string
//     - bucket: the target bucket name
//     - object: the target object name
//     - expire: expire time in seconds
//     - method: optional sign method, default is GET
//     - headers: optional sign headers, default just set the Host
//     - params: optional sign params, default is empty
// RETURNS:
//     - string: the presigned url with authorization string

func GeneratePresignedUrl(conf *bce.BceClientConfiguration, signer auth.Signer, bucket,
	object string, expire int, method string, headers, params map[string]string) string {
	return GeneratePresignedUrlInternal(conf, signer, bucket, object, expire, method, headers, params, false)
}
func GeneratePresignedUrlPathStyle(conf *bce.BceClientConfiguration, signer auth.Signer, bucket,
	object string, expire int, method string, headers, params map[string]string) string {
	return GeneratePresignedUrlInternal(conf, signer, bucket, object, expire, method, headers, params, true)
}

func GeneratePresignedUrlInternal(conf *bce.BceClientConfiguration, signer auth.Signer, bucket,
	object string, expire int, method string, headers, params map[string]string, path_style bool) string {
	req := &bce.BceRequest{}
	// Set basic arguments
	if len(method) == 0 {
		method = http.GET
	}
	req.SetMethod(method)
	req.SetEndpoint(conf.Endpoint)
	if req.Protocol() == "" {
		req.SetProtocol(bce.DEFAULT_PROTOCOL)
	}
	domain := req.Host()
	if pos := strings.Index(domain, ":"); pos != -1 {
		domain = domain[:pos]
	}
	if path_style {
		req.SetUri(getObjectUri(bucket, object))
		if conf.CnameEnabled || isCnameLikeHost(conf.Endpoint) {
			req.SetUri(getCnameUri(req.Uri()))
		}
	} else {
		if len(bucket) != 0 && net.ParseIP(domain) == nil { // not use an IP as the endpoint by client
			req.SetUri(bce.URI_PREFIX + object)
			if !conf.CnameEnabled && !isCnameLikeHost(conf.Endpoint) {
				req.SetHost(bucket + "." + req.Host())
			}
		} else {
			req.SetUri(getObjectUri(bucket, object))
			if conf.CnameEnabled || isCnameLikeHost(conf.Endpoint) {
				req.SetUri(getCnameUri(req.Uri()))
			}
		}
	}
	// Set headers and params if given.
	req.SetHeader(http.HOST, req.Host())
	if headers != nil {
		for k, v := range headers {
			req.SetHeader(k, v)
		}
	}
	if params != nil {
		for k, v := range params {
			req.SetParam(k, v)
		}
	}
	// Copy one SignOptions object to rewrite it.
	option := *conf.SignOption
	if expire != 0 {
		option.ExpireSeconds = expire
	}

	if conf.Credentials.SessionToken != "" {
		req.SetParam(http.BCE_SECURITY_TOKEN, conf.Credentials.SessionToken)
	}

	// Generate the authorization string and return the signed url.
	signer.Sign(&req.Request, conf.Credentials, &option)
	req.SetParam("authorization", req.Header(http.AUTHORIZATION))
	return fmt.Sprintf("%s://%s%s?%s", req.Protocol(), req.Host(),
		util.UriEncode(req.Uri(), false), req.QueryString())
}

// PutObjectAcl - set the ACL of the given object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
//     - cannedAcl: support private and public-read
//     - grantRead: user id list
//     - grantFullControl: user id list
//     - aclBody: the acl file body
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutObjectAcl(cli bce.Client, bucket, object, cannedAcl string,
	grantRead, grantFullControl []string, aclBody *bce.Body, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	req.SetParam("acl", "")
	ctx.Bucket = bucket
	// Joiner for generate the user id list string for grant acl header
	joiner := func(ids []string) string {
		for i := range ids {
			ids[i] = "id=\"" + ids[i] + "\""
		}
		return strings.Join(ids, ",")
	}

	// Choose a acl setting method
	methods := 0
	if len(cannedAcl) != 0 {
		methods += 1
		if validCannedAcl(cannedAcl) {
			req.SetHeader(http.BCE_ACL, cannedAcl)
		}
	}
	if len(grantRead) != 0 {
		methods += 1
		req.SetHeader(http.BCE_GRANT_READ, joiner(grantRead))
	}
	if len(grantFullControl) != 0 {
		methods += 1
		req.SetHeader(http.BCE_GRANT_FULL_CONTROL, joiner(grantFullControl))
	}
	if aclBody != nil {
		methods += 1
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		req.SetBody(aclBody)
	}
	if methods != 1 {
		return bce.NewBceClientError("BOS only support one acl setting method at the same time")
	}

	// Do sending request
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetObjectAcl - get the ACL of the given object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
// RETURNS:
//     - result: the object acl result object
//     - error: nil if success otherwise the specific error
func GetObjectAcl(cli bce.Client, bucket, object string, ctx *BosContext) (*GetObjectAclResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.GET)
	req.SetParam("acl", "")
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetObjectAclResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteObjectAcl - delete the ACL of the given object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteObjectAcl(cli bce.Client, bucket, object string, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.DELETE)
	req.SetParam("acl", "")
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// RestoreObject - restore the archive object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
//	   - args: the restore args
// RETURNS:
//     - error: nil if success otherwise the specific error
func RestoreObject(cli bce.Client, bucket string, object string, args ArchiveRestoreArgs, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetParam("restore", "")
	req.SetMethod(http.POST)
	req.SetHeader(http.BCE_RESTORE_DAYS, strconv.Itoa(args.RestoreDays))
	req.SetHeader(http.BCE_RESTORE_TIER, args.RestoreTier)
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}

	return nil
}

// PutObjectSymlink - put the object from the string or the stream
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
//     - symlinkKey: the name of the symlink
//     - symlinkArgs: the optional arguments of this api
// RETURNS:
//     - error: nil if ok otherwise the specific error
func PutObjectSymlink(cli bce.Client, bucket string, object string, symlinkKey string, symlinkArgs *PutSymlinkArgs, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, symlinkKey))
	req.SetParam("symlink", "")
	req.SetMethod(http.PUT)
	ctx.Bucket = bucket
	if symlinkArgs != nil {
		if len(symlinkArgs.ForbidOverwrite) != 0 {
			if !validForbidOverwrite(symlinkArgs.ForbidOverwrite) {
				return bce.NewBceClientError("invalid forbid overwrite val," + symlinkArgs.ForbidOverwrite)
			}
			req.SetHeader(http.BCE_FORBID_OVERWRITE, symlinkArgs.ForbidOverwrite)
		}

		if len(symlinkArgs.StorageClass) != 0 {
			if !validStorageClass(symlinkArgs.StorageClass) {
				return bce.NewBceClientError("invalid storage class val," + symlinkArgs.StorageClass)
			}
			if symlinkArgs.StorageClass == STORAGE_CLASS_ARCHIVE {
				return bce.NewBceClientError("archive storage class not support")
			}
			req.SetHeader(http.BCE_STORAGE_CLASS, symlinkArgs.StorageClass)
		}

		if len(symlinkArgs.UserMeta) != 0 {
			if err := setUserMetadata(req, symlinkArgs.UserMeta); err != nil {
				return err
			}
		}
		if len(symlinkArgs.SymlinkBucket) != 0 {
			req.SetHeader(http.BCE_SYMLINK_BUCKET, symlinkArgs.SymlinkBucket)
		}
	}
	req.SetHeader(http.BCE_SYMLINK_TARGET, object)

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutObjectSymlink - put the object from the string or the stream
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - symlinkKey: the name of the symlink
// RETURNS:
//	   - string: the name of the target object
//     - error: nil if ok otherwise the specific error
func GetObjectSymlink(cli bce.Client, bucket string, symlinkKey string, ctx *BosContext) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, symlinkKey))
	req.SetParam("symlink", "")
	req.SetMethod(http.GET)
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	if resp.Header(http.BCE_SYMLINK_BUCKET) != "" {
		result := BOS_CONFIG_PREFIX + resp.Header(http.BCE_SYMLINK_BUCKET) + "/" + resp.Header(http.BCE_SYMLINK_TARGET)
		return result, nil
	}
	return resp.Header(http.BCE_SYMLINK_TARGET), nil
}

// PutObjectTag - set tag for given object
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name of the object
//     - object: the name of the object
//     - putObjectTagArgs: the arguments of object tag
// RETURNS:
//     - error: nil if ok otherwise the specific error

func PutObjectTag(cli bce.Client, bucket, object string, putObjectTagArgs *PutObjectTagArgs, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	req.SetParam("tagging", "")
	ctx.Bucket = bucket
	reqByte, _ := json.Marshal(putObjectTagArgs)
	body, err := bce.NewBodyFromString(string(reqByte))
	if err != nil {
		return err
	}
	req.SetBody(body)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

func GetObjectTag(cli bce.Client, bucket, object string, ctx *BosContext) (map[string]interface{}, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.GET)
	req.SetParam("tagging", "")
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	bodyBytes, err := ioutil.ReadAll(resp.Body())
	if err != nil {
		return nil, err
	}

	result, err := ParseObjectTagResult(bodyBytes)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func DeleteObjectTag(cli bce.Client, bucket, object string, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.DELETE)
	req.SetParam("tagging", "")
	ctx.Bucket = bucket
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}
