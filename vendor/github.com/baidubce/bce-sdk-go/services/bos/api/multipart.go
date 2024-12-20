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

// multipart.go - the multipart-related APIs definition supported by the BOS service

package api

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
)

// InitiateMultipartUpload - initiate a multipart upload to get a upload ID
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
//     - contentType: the content type of the object to be uploaded which should be specified,
//       otherwise use the default(application/octet-stream)
//     - args: the optional arguments
// RETURNS:
//     - *InitiateMultipartUploadResult: the result data structure
//     - error: nil if ok otherwise the specific error
func InitiateMultipartUpload(cli bce.Client, bucket, object, contentType string,
	args *InitiateMultipartUploadArgs, ctx *BosContext) (*InitiateMultipartUploadResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.POST)
	req.SetParam("uploads", "")
	ctx.Bucket = bucket
	if len(contentType) == 0 {
		contentType = RAW_CONTENT_TYPE
	}
	req.SetHeader(http.CONTENT_TYPE, contentType)

	// Optional arguments settings
	if args != nil {
		setOptionalNullHeaders(req, map[string]string{
			http.CACHE_CONTROL:       args.CacheControl,
			http.CONTENT_DISPOSITION: args.ContentDisposition,
			http.EXPIRES:             args.Expires,
		})

		if validStorageClass(args.StorageClass) {
			req.SetHeader(http.BCE_STORAGE_CLASS, args.StorageClass)
		} else {
			if len(args.StorageClass) != 0 {
				return nil, bce.NewBceClientError("invalid storage class value: " +
					args.StorageClass)
			}
		}
		if len(args.ObjectTagging) != 0 {
			if ok, encodeTagging := validObjectTagging(args.ObjectTagging); ok {
				req.SetHeader(http.BCE_OBJECT_TAGGING, encodeTagging)
			}
		}
		if validMetadataDirective(args.TaggingDirective) {
			req.SetHeader(http.BCE_COPY_TAGGING_DIRECTIVE, args.TaggingDirective)
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
	result := &InitiateMultipartUploadResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	defer func() { resp.Body().Close() }()
	return result, nil
}

// UploadPart - upload the single part in the multipart upload process
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - content: the uploaded part content
//     - args: the optional arguments
// RETURNS:
//     - string: the etag of the uploaded part
//     - error: nil if ok otherwise the specific error
func UploadPart(cli bce.Client, bucket, object, uploadId string, partNumber int,
	content *bce.Body, args *UploadPartArgs, ctx *BosContext) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	req.SetParam("uploadId", uploadId)
	req.SetParam("partNumber", fmt.Sprintf("%d", partNumber))
	ctx.Bucket = bucket
	if content == nil {
		return "", bce.NewBceClientError("upload part content should not be empty")
	}
	if content.Size() >= THRESHOLD_100_CONTINUE {
		req.SetHeader("Expect", "100-continue")
	}
	req.SetBody(content)

	// Optional arguments settings
	if args != nil {
		setOptionalNullHeaders(req, map[string]string{
			http.CONTENT_MD5:        args.ContentMD5,
			http.BCE_CONTENT_SHA256: args.ContentSha256,
			http.BCE_CONTENT_CRC32:  args.ContentCrc32,
		})
		//set traffic-limit
		if args.TrafficLimit > 0 {
			if args.TrafficLimit > TRAFFIC_LIMIT_MAX || args.TrafficLimit < TRAFFIC_LIMIT_MIN {
				return "", bce.NewBceClientError(fmt.Sprintf("TrafficLimit must between %d ~ %d, current value:%d", TRAFFIC_LIMIT_MIN, TRAFFIC_LIMIT_MAX, args.TrafficLimit))
			}
			req.SetHeader(http.BCE_TRAFFIC_LIMIT, fmt.Sprintf("%d", args.TrafficLimit))
		}

	}

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return strings.Trim(resp.Header(http.ETAG), "\""), nil
}

// UploadPartFromBytes - upload the single part in the multipart upload process
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - content: the uploaded part content
//     - args: the optional arguments
// RETURNS:
//     - string: the etag of the uploaded part
//     - error: nil if ok otherwise the specific error
func UploadPartFromBytes(cli bce.Client, bucket, object, uploadId string, partNumber int,
	content []byte, args *UploadPartArgs, ctx *BosContext) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	req.SetParam("uploadId", uploadId)
	req.SetParam("partNumber", fmt.Sprintf("%d", partNumber))
	ctx.Bucket = bucket
	if content == nil {
		return "", bce.NewBceClientError("upload part content should not be empty")
	}
	size := len(content)
	if size >= THRESHOLD_100_CONTINUE {
		req.SetHeader("Expect", "100-continue")
	}
	// set md5 and content-length
	req.SetLength(int64(size))
	if size > 0 {
		// calc md5
		if args == nil || args.ContentMD5 == "" {
			buf := bytes.NewBuffer(content)
			contentMD5, err := util.CalculateContentMD5(buf, int64(size))
			if err != nil {
				return "", err
			}
			req.SetHeader(http.CONTENT_MD5, contentMD5)
		}
		req.SetHeader(http.CONTENT_LENGTH, fmt.Sprintf("%d", size))
	}
	// Optional arguments settings
	if args != nil {
		setOptionalNullHeaders(req, map[string]string{
			http.CONTENT_MD5:        args.ContentMD5,
			http.BCE_CONTENT_SHA256: args.ContentSha256,
			http.BCE_CONTENT_CRC32:  args.ContentCrc32,
		})
		//set traffic-limit
		if args.TrafficLimit > 0 {
			if args.TrafficLimit > TRAFFIC_LIMIT_MAX || args.TrafficLimit < TRAFFIC_LIMIT_MIN {
				return "", bce.NewBceClientError(fmt.Sprintf("TrafficLimit must between %d ~ %d, current value:%d",
					TRAFFIC_LIMIT_MIN, TRAFFIC_LIMIT_MAX, args.TrafficLimit))
			}
			req.SetHeader(http.BCE_TRAFFIC_LIMIT, fmt.Sprintf("%d", args.TrafficLimit))
		}
	}
	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequestFromBytes(cli, req, resp, ctx, content); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return strings.Trim(resp.Header(http.ETAG), "\""), nil
}

// UploadPartCopy - copy the multipart data
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - source: the copy source uri
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - args: the optional arguments
// RETURNS:
//     - *CopyObjectResult: the lastModified and eTag of the part
//     - error: nil if ok otherwise the specific error
func UploadPartCopy(cli bce.Client, bucket, object, source, uploadId string, partNumber int,
	args *UploadPartCopyArgs, ctx *BosContext) (*CopyObjectResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.PUT)
	req.SetParam("uploadId", uploadId)
	req.SetParam("partNumber", fmt.Sprintf("%d", partNumber))
	ctx.Bucket = bucket
	if len(source) == 0 {
		return nil, bce.NewBceClientError("upload part copy source should not be empty")
	}
	req.SetHeader(http.BCE_COPY_SOURCE, util.UriEncode(source, false))

	// Optional arguments settings
	if args != nil {
		setOptionalNullHeaders(req, map[string]string{
			http.BCE_COPY_SOURCE_RANGE:               args.SourceRange,
			http.BCE_COPY_SOURCE_IF_MATCH:            args.IfMatch,
			http.BCE_COPY_SOURCE_IF_NONE_MATCH:       args.IfNoneMatch,
			http.BCE_COPY_SOURCE_IF_MODIFIED_SINCE:   args.IfModifiedSince,
			http.BCE_COPY_SOURCE_IF_UNMODIFIED_SINCE: args.IfUnmodifiedSince,
		})
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
	result := &CopyObjectResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// CompleteMultipartUpload - finish a multipart upload operation
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
//     - parts: all parts info stream
//     - meta: user defined meta data
// RETURNS:
//     - *CompleteMultipartUploadResult: the result data
//     - error: nil if ok otherwise the specific error
func CompleteMultipartUpload(cli bce.Client, bucket, object, uploadId string,
	body *bce.Body, args *CompleteMultipartUploadArgs, ctx *BosContext) (*CompleteMultipartUploadResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.POST)
	req.SetParam("uploadId", uploadId)
	ctx.Bucket = bucket
	if body == nil {
		return nil, bce.NewBceClientError("upload body info should not be emtpy")
	}
	if body.Size() >= THRESHOLD_100_CONTINUE {
		req.SetHeader("Expect", "100-continue")
	}
	req.SetBody(body)

	// Optional arguments settings
	if args.UserMeta != nil {
		if err := setUserMetadata(req, args.UserMeta); err != nil {
			return nil, err
		}
	}
	if len(args.Process) != 0 {
		req.SetHeader(http.BCE_PROCESS, args.Process)
	}
	if len(args.ContentCrc32) != 0 {
		req.SetHeader(http.BCE_CONTENT_CRC32, args.ContentCrc32)
	}

	// Send request and get the result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &CompleteMultipartUploadResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	headers := resp.Headers()
	if val, ok := headers[toHttpHeaderKey(http.BCE_CONTENT_CRC32)]; ok {
		result.ContentCrc32 = val
	}
	return result, nil
}

// AbortMultipartUpload - abort a multipart upload operation
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
// RETURNS:
//     - error: nil if ok otherwise the specific error
func AbortMultipartUpload(cli bce.Client, bucket, object, uploadId string, ctx *BosContext) error {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.DELETE)
	req.SetParam("uploadId", uploadId)
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

// ListParts - list the successfully uploaded parts info by upload id
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
//     - args: the optional arguments
//             partNumberMarker: return parts after this marker
//             maxParts: the max number of return parts, default and maximum is 1000
// RETURNS:
//     - *ListPartsResult: the uploaded parts info result
//     - error: nil if ok otherwise the specific error
func ListParts(cli bce.Client, bucket, object, uploadId string,
	args *ListPartsArgs, ctx *BosContext) (*ListPartsResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getObjectUri(bucket, object))
	req.SetMethod(http.GET)
	req.SetParam("uploadId", uploadId)
	ctx.Bucket = bucket
	// Optional arguments settings
	if args != nil {
		if len(args.PartNumberMarker) > 0 {
			req.SetParam("partNumberMarker", args.PartNumberMarker)
		}
		if args.MaxParts > 0 {
			req.SetParam("maxParts", fmt.Sprintf("%d", args.MaxParts))
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
	result := &ListPartsResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListMultipartUploads - list the unfinished uploaded parts of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the destination bucket name
//     - args: the optional arguments
// RETURNS:
//     - *ListMultipartUploadsResult: the unfinished uploaded parts info result
//     - error: nil if ok otherwise the specific error
func ListMultipartUploads(cli bce.Client, bucket string,
	args *ListMultipartUploadsArgs, ctx *BosContext) (*ListMultipartUploadsResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("uploads", "")
	ctx.Bucket = bucket

	// Optional arguments settings
	if args != nil {
		if len(args.Delimiter) > 0 {
			req.SetParam("delimiter", args.Delimiter)
		}
		if len(args.KeyMarker) > 0 {
			req.SetParam("keyMarker", args.KeyMarker)
		}
		if args.MaxUploads > 0 {
			req.SetParam("maxUploads", fmt.Sprintf("%d", args.MaxUploads))
		}
		if len(args.Prefix) > 0 {
			req.SetParam("prefix", args.Prefix)
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
	result := &ListMultipartUploadsResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}
