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

// bucket.go - the bucket APIs definition supported by the BOS service

// Package api defines all APIs supported by the BOS service of BCE.
package api

import (
	"encoding/json"
	"strconv"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
)

// ListBuckets - list all buckets of the account
//
// PARAMS:
//     - cli: the client agent which can perform sending request
// RETURNS:
//     - *ListBucketsResult: the result bucket list structure
//     - error: nil if ok otherwise the specific error
func ListBuckets(cli bce.Client) (*ListBucketsResult, error) {
	req := &bce.BceRequest{}
	req.SetMethod(http.GET)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &ListBucketsResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListObjects - list all objects of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - args: the optional arguments to list objects
// RETURNS:
//     - *ListObjectsResult: the result object list structure
//     - error: nil if ok otherwise the specific error
func ListObjects(cli bce.Client, bucket string,
	args *ListObjectsArgs) (*ListObjectsResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)

	// Optional arguments settings
	if args != nil {
		if len(args.Delimiter) != 0 {
			req.SetParam("delimiter", args.Delimiter)
		}
		if len(args.Marker) != 0 {
			req.SetParam("marker", args.Marker)
		}
		if args.MaxKeys != 0 {
			req.SetParam("maxKeys", strconv.Itoa(args.MaxKeys))
		}
		if len(args.Prefix) != 0 {
			req.SetParam("prefix", args.Prefix)
		}
	}
	if args == nil || args.MaxKeys == 0 {
		req.SetParam("maxKeys", "1000")
	}

	// Send the request and get result
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &ListObjectsResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	defer func() { resp.Body().Close() }()
	return result, nil
}

// HeadBucket - test the given bucket existed and access authority
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if exists and have authority otherwise the specific error
func HeadBucket(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.HEAD)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucket - create a new bucket with the given name
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the new bucket name
// RETURNS:
//     - string: the location of the new bucket if create success
//     - error: nil if create success otherwise the specific error
func PutBucket(cli bce.Client, bucket string) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return resp.Header(http.LOCATION), nil
}

// DeleteBucket - delete an empty bucket by given name
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name to be deleted
// RETURNS:
//     - error: nil if delete success otherwise the specific error
func DeleteBucket(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketLocation - get the location of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - string: the location of the bucket
//     - error: nil if delete success otherwise the specific error
func GetBucketLocation(cli bce.Client, bucket string) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("location", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	result := &LocationType{}
	if err := resp.ParseJsonBody(result); err != nil {
		return "", err
	}
	defer func() { resp.Body().Close() }()
	return result.LocationConstraint, nil
}

// PutBucketAcl - set the acl of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - cannedAcl: support private, public-read, public-read-write
//     - aclBody: the acl file body
// RETURNS:
//     - error: nil if delete success otherwise the specific error
func PutBucketAcl(cli bce.Client, bucket, cannedAcl string, aclBody *bce.Body) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("acl", "")

	// The acl setting
	if len(cannedAcl) != 0 && aclBody != nil {
		return bce.NewBceClientError("BOS does not support cannedAcl and acl file at the same time")
	}
	if validCannedAcl(cannedAcl) {
		req.SetHeader(http.BCE_ACL, cannedAcl)
	}
	if aclBody != nil {
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		req.SetBody(aclBody)
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketAcl - get the acl of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - *GetBucketAclResult: the result of the bucket acl
//     - error: nil if success otherwise the specific error
func GetBucketAcl(cli bce.Client, bucket string) (*GetBucketAclResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("acl", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketAclResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	defer func() { resp.Body().Close() }()
	return result, nil
}

// PutBucketLogging - set the logging prefix of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - logging: the logging prefix json string body
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketLogging(cli bce.Client, bucket string, logging *bce.Body) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("logging", "")
	req.SetBody(logging)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketLogging - get the logging config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - *GetBucketLoggingResult: the logging setting of the bucket
//     - error: nil if success otherwise the specific error
func GetBucketLogging(cli bce.Client, bucket string) (*GetBucketLoggingResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("logging", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketLoggingResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteBucketLogging - delete the logging setting of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketLogging(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("logging", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucketLifecycle - set the lifecycle rule of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - lifecycle: the lifecycle rule json string body
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketLifecycle(cli bce.Client, bucket string, lifecycle *bce.Body) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("lifecycle", "")
	req.SetBody(lifecycle)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketLifecycle - get the lifecycle rule of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - *GetBucketLifecycleResult: the lifecycle rule of the bucket
//     - error: nil if success otherwise the specific error
func GetBucketLifecycle(cli bce.Client, bucket string) (*GetBucketLifecycleResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("lifecycle", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketLifecycleResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteBucketLifecycle - delete the lifecycle rule of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketLifecycle(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("lifecycle", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucketStorageclass - set the storage class of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - storageClass: the storage class string
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketStorageclass(cli bce.Client, bucket, storageClass string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("storageClass", "")

	obj := &StorageClassType{storageClass}
	jsonBytes, jsonErr := json.Marshal(obj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	req.SetBody(body)

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketStorageclass - get the storage class of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - string: the storage class of the bucket
//     - error: nil if success otherwise the specific error
func GetBucketStorageclass(cli bce.Client, bucket string) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("storageClass", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	result := &StorageClassType{}
	if err := resp.ParseJsonBody(result); err != nil {
		return "", err
	}
	return result.StorageClass, nil
}

// PutBucketReplication - set the bucket replication of different region
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - replicationConf: the replication config body stream
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketReplication(cli bce.Client, bucket string, replicationConf *bce.Body, replicationRuleId string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("replication", "")
	if len(replicationRuleId) > 0 {
		req.SetParam("id", replicationRuleId)
	}

	if replicationConf != nil {
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		req.SetBody(replicationConf)
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketReplication - get the bucket replication config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - *GetBucketReplicationResult: the result of the bucket replication config
//     - error: nil if success otherwise the specific error
func GetBucketReplication(cli bce.Client, bucket string, replicationRuleId string) (*GetBucketReplicationResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("replication", "")
	if len(replicationRuleId) > 0 {
		req.SetParam("id", replicationRuleId)
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketReplicationResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListBucketReplication - list all replication config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func ListBucketReplication(cli bce.Client, bucket string) (*ListBucketReplicationResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("replication", "")
	req.SetParam("list", "")
	resp := &bce.BceResponse{}
	if err := cli.SendRequest(req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &ListBucketReplicationResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteBucketReplication - delete the bucket replication config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketReplication(cli bce.Client, bucket string, replicationRuleId string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("replication", "")
	if len(replicationRuleId) > 0 {
		req.SetParam("id", replicationRuleId)
	}
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketReplicationProgress - get the bucket replication process of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - *GetBucketReplicationProgressResult: the result of the bucket replication process
//     - error: nil if success otherwise the specific error
func GetBucketReplicationProgress(cli bce.Client, bucket string, replicationRuleId string) (
	*GetBucketReplicationProgressResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("replicationProgress", "")
	if len(replicationRuleId) > 0 {
		req.SetParam("id", replicationRuleId)
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketReplicationProgressResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// PutBucketEncryption - set the bucket encrpytion config
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - algorithm: the encryption algorithm
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketEncryption(cli bce.Client, bucket, algorithm string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("encryption", "")

	obj := &BucketEncryptionType{algorithm}
	jsonBytes, jsonErr := json.Marshal(obj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
	req.SetBody(body)

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketEncryption - get the encryption config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - algorithm: the bucket encryption algorithm
//     - error: nil if success otherwise the specific error
func GetBucketEncryption(cli bce.Client, bucket string) (string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("encryption", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return "", err
	}
	if resp.IsFail() {
		return "", resp.ServiceError()
	}
	result := &BucketEncryptionType{}
	if err := resp.ParseJsonBody(result); err != nil {
		return "", err
	}
	return result.EncryptionAlgorithm, nil
}

// DeleteBucketEncryption - delete the encryption config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketEncryption(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("encryption", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucketStaticWebsite - set the bucket static website config
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - confBody: the static website config body stream
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketStaticWebsite(cli bce.Client, bucket string, confBody *bce.Body) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("website", "")
	if confBody != nil {
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		req.SetBody(confBody)
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketStaticWebsite - get the static website config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - result: the bucket static website config result object
//     - error: nil if success otherwise the specific error
func GetBucketStaticWebsite(cli bce.Client, bucket string) (
	*GetBucketStaticWebsiteResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("website", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketStaticWebsiteResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteBucketStaticWebsite - delete the static website config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketStaticWebsite(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("website", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucketCors - set the bucket CORS config
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - confBody: the CORS config body stream
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketCors(cli bce.Client, bucket string, confBody *bce.Body) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("cors", "")
	if confBody != nil {
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		req.SetBody(confBody)
	}

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketCors - get the CORS config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - result: the bucket CORS config result object
//     - error: nil if success otherwise the specific error
func GetBucketCors(cli bce.Client, bucket string) (
	*GetBucketCorsResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("cors", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketCorsResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteBucketCors - delete the CORS config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketCors(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("cors", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucketCopyrightProtection - set the copyright protection config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - resources: the resource items in the bucket to be protected
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketCopyrightProtection(cli bce.Client, bucket string, resources ...string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("copyrightProtection", "")
	if len(resources) == 0 {
		return bce.NewBceClientError("the resource to set copyright protection is empty")
	}
	arg := &CopyrightProtectionType{resources}
	jsonBytes, jsonErr := json.Marshal(arg)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
	req.SetBody(body)

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// GetBucketCopyrightProtection - get the copyright protection config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - result: the bucket copyright protection resources array
//     - error: nil if success otherwise the specific error
func GetBucketCopyrightProtection(cli bce.Client, bucket string) ([]string, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("copyrightProtection", "")

	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &CopyrightProtectionType{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result.Resource, nil
}

// DeleteBucketCopyrightProtection - delete the copyright protection config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func DeleteBucketCopyrightProtection(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("copyrightProtection", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

// PutBucketTrash - put the trash setting of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//	   - trashDir: the trash dir name
// RETURNS:
//     - error: nil if success otherwise the specific error
func PutBucketTrash(cli bce.Client, bucket string, trashReq PutBucketTrashReq) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("trash", "")
	reqByte, _ := json.Marshal(trashReq)
	body, err := bce.NewBodyFromString(string(reqByte))
	if err != nil {
		return err
	}
	req.SetBody(body)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

func GetBucketTrash(cli bce.Client, bucket string) (*GetBucketTrashResult, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("trash", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &GetBucketTrashResult{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

func DeleteBucketTrash(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("trash", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

func PutBucketNotification(cli bce.Client, bucket string, putBucketNotificationReq PutBucketNotificationReq) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.PUT)
	req.SetParam("notification", "")
	reqByte, _ := json.Marshal(putBucketNotificationReq)
	body, err := bce.NewBodyFromString(string(reqByte))
	if err != nil {
		return err
	}
	req.SetBody(body)
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}

func GetBucketNotification(cli bce.Client, bucket string) (*PutBucketNotificationReq, error) {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.GET)
	req.SetParam("notification", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	result := &PutBucketNotificationReq{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

func DeleteBucketNotification(cli bce.Client, bucket string) error {
	req := &bce.BceRequest{}
	req.SetUri(getBucketUri(bucket))
	req.SetMethod(http.DELETE)
	req.SetParam("notification", "")
	resp := &bce.BceResponse{}
	if err := SendRequest(cli, req, resp); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}
