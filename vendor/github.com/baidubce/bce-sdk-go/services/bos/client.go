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

// client.go - define the client for BOS service

// Package bos defines the BOS services of BCE. The supported APIs are all defined in sub-package
// model with three types: 16 bucket APIs, 9 object APIs and 7 multipart APIs.
package bos

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	sdk_http "github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/services/bos/api"
	"github.com/baidubce/bce-sdk-go/services/sts"
	"github.com/baidubce/bce-sdk-go/util/log"
)

const (
	DEFAULT_SERVICE_DOMAIN = bce.DEFAULT_REGION + ".bcebos.com"
	DEFAULT_MAX_PARALLEL   = 10
	MULTIPART_ALIGN        = 1 << 20         // 1MB
	MIN_MULTIPART_SIZE     = 100 * (1 << 10) // 100 KB
	DEFAULT_MULTIPART_SIZE = 12 * (1 << 20)  // 12MB

	MAX_PART_NUMBER        = 10000
	MAX_SINGLE_PART_SIZE   = 5 * (1 << 30)    // 5GB
	MAX_SINGLE_OBJECT_SIZE = 48.8 * (1 << 40) // 48.8TB
)

// Client of BOS service is a kind of BceClient, so derived from BceClient
type Client struct {
	*bce.BceClient

	// Fileds that used in parallel operation for BOS service
	MaxParallel   int64
	MultipartSize int64
	BosContext    *api.BosContext
}

// BosClientConfiguration defines the config components structure by user.
type BosClientConfiguration struct {
	Ak               string
	Sk               string
	Endpoint         string
	RedirectDisabled bool
	PathStyleEnable  bool
}

// NewClient make the BOS service client with default configuration.
// Use `cli.Config.xxx` to access the config or change it to non-default value.
func NewClient(ak, sk, endpoint string) (*Client, error) {
	return NewClientWithConfig(&BosClientConfiguration{
		Ak:               ak,
		Sk:               sk,
		Endpoint:         endpoint,
		RedirectDisabled: false,
		PathStyleEnable:  false,
	})
}

// NewStsClient make the BOS service client with STS configuration, it will first apply stsAK,stsSK, sessionToken, then return bosClient using temporary sts Credential
func NewStsClient(ak, sk, endpoint string, expiration int) (*Client, error) {
	stsClient, err := sts.NewClient(ak, sk)
	if err != nil {
		fmt.Println("create sts client object :", err)
		return nil, err
	}
	sts, err := stsClient.GetSessionToken(expiration, "")
	if err != nil {
		fmt.Println("get session token failed:", err)
		return nil, err
	}

	bosClient, err := NewClient(sts.AccessKeyId, sts.SecretAccessKey, endpoint)
	if err != nil {
		fmt.Println("create bos client failed:", err)
		return nil, err
	}
	stsCredential, err := auth.NewSessionBceCredentials(
		sts.AccessKeyId,
		sts.SecretAccessKey,
		sts.SessionToken)
	if err != nil {
		fmt.Println("create sts credential object failed:", err)
		return nil, err
	}
	bosClient.Config.Credentials = stsCredential
	return bosClient, nil
}

func NewClientWithConfig(config *BosClientConfiguration) (*Client, error) {
	var credentials *auth.BceCredentials
	var err error
	ak, sk, endpoint := config.Ak, config.Sk, config.Endpoint
	if len(ak) == 0 && len(sk) == 0 { // to support public-read-write request
		credentials, err = nil, nil
	} else {
		credentials, err = auth.NewBceCredentials(ak, sk)
		if err != nil {
			return nil, err
		}
	}
	if len(endpoint) == 0 {
		endpoint = DEFAULT_SERVICE_DOMAIN
	}
	defaultSignOptions := &auth.SignOptions{
		HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
		ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS}
	defaultConf := &bce.BceClientConfiguration{
		Endpoint:                  endpoint,
		Region:                    bce.DEFAULT_REGION,
		UserAgent:                 bce.DEFAULT_USER_AGENT,
		Credentials:               credentials,
		SignOption:                defaultSignOptions,
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: bce.DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS,
		RedirectDisabled:          config.RedirectDisabled}
	v1Signer := &auth.BceV1Signer{}
	defaultContext := &api.BosContext{
		PathStyleEnable: config.PathStyleEnable,
	}
	client := &Client{bce.NewBceClient(defaultConf, v1Signer),
		DEFAULT_MAX_PARALLEL, DEFAULT_MULTIPART_SIZE, defaultContext}
	return client, nil
}

// ListBuckets - list all buckets
//
// RETURNS:
//     - *api.ListBucketsResult: the all buckets
//     - error: the return error if any occurs
func (c *Client) ListBuckets() (*api.ListBucketsResult, error) {
	return api.ListBuckets(c, c.BosContext)
}

// ListObjects - list all objects of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - args: the optional arguments to list objects
// RETURNS:
//     - *api.ListObjectsResult: the all objects of the bucket
//     - error: the return error if any occurs
func (c *Client) ListObjects(bucket string,
	args *api.ListObjectsArgs) (*api.ListObjectsResult, error) {
	return api.ListObjects(c, bucket, args, c.BosContext)
}

// SimpleListObjects - list all objects of the given bucket with simple arguments
//
// PARAMS:
//     - bucket: the bucket name
//     - prefix: the prefix for listing
//     - maxKeys: the max number of result objects
//     - marker: the marker to mark the beginning for the listing
//     - delimiter: the delimiter for list objects
// RETURNS:
//     - *api.ListObjectsResult: the all objects of the bucket
//     - error: the return error if any occurs
func (c *Client) SimpleListObjects(bucket, prefix string, maxKeys int, marker,
	delimiter string) (*api.ListObjectsResult, error) {
	args := &api.ListObjectsArgs{delimiter, marker, maxKeys, prefix}
	return api.ListObjects(c, bucket, args, c.BosContext)
}

// HeadBucket - test the given bucket existed and access authority
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if exists and have authority otherwise the specific error
func (c *Client) HeadBucket(bucket string) error {
	err, _ := api.HeadBucket(c, bucket, c.BosContext)
	return err
}

// DoesBucketExist - test the given bucket existed or not
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - bool: true if exists and false if not exists or occurs error
//     - error: nil if exists or not exist, otherwise the specific error
func (c *Client) DoesBucketExist(bucket string) (bool, error) {
	err, _ := api.HeadBucket(c, bucket, c.BosContext)
	if err == nil {
		return true, nil
	}
	if realErr, ok := err.(*bce.BceServiceError); ok {
		if realErr.StatusCode == http.StatusForbidden {
			return true, nil
		}
		if realErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
	}
	return false, err
}

//IsNsBucket - test the given bucket is namespace bucket or not
func (c *Client) IsNsBucket(bucket string) bool {
	err, resp := api.HeadBucket(c, bucket, c.BosContext)
	if err == nil && resp.Header(sdk_http.BCE_BUCKET_TYPE) == api.NAMESPACE_BUCKET {
		return true
	}
	if realErr, ok := err.(*bce.BceServiceError); ok {
		if realErr.StatusCode == http.StatusForbidden &&
			resp.Header(sdk_http.BCE_BUCKET_TYPE) == api.NAMESPACE_BUCKET {
			return true
		}
	}
	return false
}

// PutBucket - create a new bucket
//
// PARAMS:
//     - bucket: the new bucket name
// RETURNS:
//     - string: the location of the new bucket if create success
//     - error: nil if create success otherwise the specific error
func (c *Client) PutBucket(bucket string) (string, error) {
	return api.PutBucket(c, bucket, nil, c.BosContext)
}

func (c *Client) PutBucketWithArgs(bucket string, args *api.PutBucketArgs) (string, error) {
	return api.PutBucket(c, bucket, args, c.BosContext)
}

// DeleteBucket - delete a empty bucket
//
// PARAMS:
//     - bucket: the bucket name to be deleted
// RETURNS:
//     - error: nil if delete success otherwise the specific error
func (c *Client) DeleteBucket(bucket string) error {
	return api.DeleteBucket(c, bucket, c.BosContext)
}

// GetBucketLocation - get the location fo the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - string: the location of the bucket
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketLocation(bucket string) (string, error) {
	return api.GetBucketLocation(c, bucket, c.BosContext)
}

// PutBucketAcl - set the acl of the given bucket with acl body stream
//
// PARAMS:
//     - bucket: the bucket name
//     - aclBody: the acl json body stream
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketAcl(bucket string, aclBody *bce.Body) error {
	return api.PutBucketAcl(c, bucket, "", aclBody, c.BosContext)
}

// PutBucketAclFromCanned - set the canned acl of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - cannedAcl: the cannedAcl string
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketAclFromCanned(bucket, cannedAcl string) error {
	return api.PutBucketAcl(c, bucket, cannedAcl, nil, c.BosContext)
}

// PutBucketAclFromFile - set the acl of the given bucket with acl json file name
//
// PARAMS:
//     - bucket: the bucket name
//     - aclFile: the acl file name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketAclFromFile(bucket, aclFile string) error {
	body, err := bce.NewBodyFromFile(aclFile)
	if err != nil {
		return err
	}
	return api.PutBucketAcl(c, bucket, "", body, c.BosContext)
}

// PutBucketAclFromString - set the acl of the given bucket with acl json string
//
// PARAMS:
//     - bucket: the bucket name
//     - aclString: the acl string with json format
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketAclFromString(bucket, aclString string) error {
	body, err := bce.NewBodyFromString(aclString)
	if err != nil {
		return err
	}
	return api.PutBucketAcl(c, bucket, "", body, c.BosContext)
}

// PutBucketAclFromStruct - set the acl of the given bucket with acl data structure
//
// PARAMS:
//     - bucket: the bucket name
//     - aclObj: the acl struct object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketAclFromStruct(bucket string, aclObj *api.PutBucketAclArgs) error {
	jsonBytes, jsonErr := json.Marshal(aclObj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	return api.PutBucketAcl(c, bucket, "", body, c.BosContext)
}

// GetBucketAcl - get the acl of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - *api.GetBucketAclResult: the result of the bucket acl
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketAcl(bucket string) (*api.GetBucketAclResult, error) {
	return api.GetBucketAcl(c, bucket, c.BosContext)
}

// PutBucketLogging - set the loging setting of the given bucket with json stream
//
// PARAMS:
//     - bucket: the bucket name
//     - body: the json body
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketLogging(bucket string, body *bce.Body) error {
	return api.PutBucketLogging(c, bucket, body, c.BosContext)
}

// PutBucketLoggingFromString - set the loging setting of the given bucket with json string
//
// PARAMS:
//     - bucket: the bucket name
//     - logging: the json format string
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketLoggingFromString(bucket, logging string) error {
	body, err := bce.NewBodyFromString(logging)
	if err != nil {
		return err
	}
	return api.PutBucketLogging(c, bucket, body, c.BosContext)
}

// PutBucketLoggingFromStruct - set the loging setting of the given bucket with args object
//
// PARAMS:
//     - bucket: the bucket name
//     - obj: the logging setting object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketLoggingFromStruct(bucket string, obj *api.PutBucketLoggingArgs) error {
	jsonBytes, jsonErr := json.Marshal(obj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	return api.PutBucketLogging(c, bucket, body, c.BosContext)
}

// GetBucketLogging - get the logging setting of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - *api.GetBucketLoggingResult: the logging setting of the bucket
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketLogging(bucket string) (*api.GetBucketLoggingResult, error) {
	return api.GetBucketLogging(c, bucket, c.BosContext)
}

// DeleteBucketLogging - delete the logging setting of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketLogging(bucket string) error {
	return api.DeleteBucketLogging(c, bucket, c.BosContext)
}

// PutBucketLifecycle - set the lifecycle rule of the given bucket with raw stream
//
// PARAMS:
//     - bucket: the bucket name
//     - lifecycle: the lifecycle rule json body
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketLifecycle(bucket string, lifecycle *bce.Body) error {
	return api.PutBucketLifecycle(c, bucket, lifecycle, c.BosContext)
}

// PutBucketLifecycleFromString - set the lifecycle rule of the given bucket with string
//
// PARAMS:
//     - bucket: the bucket name
//     - lifecycle: the lifecycle rule json format string body
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketLifecycleFromString(bucket, lifecycle string) error {
	body, err := bce.NewBodyFromString(lifecycle)
	if err != nil {
		return err
	}
	return api.PutBucketLifecycle(c, bucket, body, c.BosContext)
}

// GetBucketLifecycle - get the lifecycle rule of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - *api.GetBucketLifecycleResult: the lifecycle rule of the bucket
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketLifecycle(bucket string) (*api.GetBucketLifecycleResult, error) {
	return api.GetBucketLifecycle(c, bucket, c.BosContext)
}

// DeleteBucketLifecycle - delete the lifecycle rule of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketLifecycle(bucket string) error {
	return api.DeleteBucketLifecycle(c, bucket, c.BosContext)
}

// PutBucketStorageclass - set the storage class of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - storageClass: the storage class string value
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketStorageclass(bucket, storageClass string) error {
	return api.PutBucketStorageclass(c, bucket, storageClass, c.BosContext)
}

// GetBucketStorageclass - get the storage class of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - string: the storage class string value
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketStorageclass(bucket string) (string, error) {
	return api.GetBucketStorageclass(c, bucket, c.BosContext)
}

// PutBucketReplication - set the bucket replication config of different region
//
// PARAMS:
//     - bucket: the bucket name
//     - replicationConf: the replication config json body stream
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketReplication(bucket string, replicationConf *bce.Body, replicationRuleId string) error {
	return api.PutBucketReplication(c, bucket, replicationConf, replicationRuleId, c.BosContext)
}

// PutBucketReplicationFromFile - set the bucket replication config with json file name
//
// PARAMS:
//     - bucket: the bucket name
//     - confFile: the config json file name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketReplicationFromFile(bucket, confFile string, replicationRuleId string) error {
	body, err := bce.NewBodyFromFile(confFile)
	if err != nil {
		return err
	}
	return api.PutBucketReplication(c, bucket, body, replicationRuleId, c.BosContext)
}

// PutBucketReplicationFromString - set the bucket replication config with json string
//
// PARAMS:
//     - bucket: the bucket name
//     - confString: the config string with json format
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketReplicationFromString(bucket, confString string, replicationRuleId string) error {
	body, err := bce.NewBodyFromString(confString)
	if err != nil {
		return err
	}
	return api.PutBucketReplication(c, bucket, body, replicationRuleId, c.BosContext)
}

// PutBucketReplicationFromStruct - set the bucket replication config with struct
//
// PARAMS:
//     - bucket: the bucket name
//     - confObj: the replication config struct object
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketReplicationFromStruct(bucket string,
	confObj *api.PutBucketReplicationArgs, replicationRuleId string) error {
	jsonBytes, jsonErr := json.Marshal(confObj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	return api.PutBucketReplication(c, bucket, body, replicationRuleId, c.BosContext)
}

// GetBucketReplication - get the bucket replication config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - *api.GetBucketReplicationResult: the result of the bucket replication config
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketReplication(bucket string, replicationRuleId string) (*api.GetBucketReplicationResult, error) {
	return api.GetBucketReplication(c, bucket, replicationRuleId, c.BosContext)
}

// ListBucketReplication - get all replication config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - *api.ListBucketReplicationResult: the list of the bucket replication config
//     - error: nil if success otherwise the specific error
func (c *Client) ListBucketReplication(bucket string) (*api.ListBucketReplicationResult, error) {
	return api.ListBucketReplication(c, bucket, c.BosContext)
}

// DeleteBucketReplication - delete the bucket replication config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketReplication(bucket string, replicationRuleId string) error {
	return api.DeleteBucketReplication(c, bucket, replicationRuleId, c.BosContext)
}

// GetBucketReplicationProgress - get the bucket replication process of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - replicationRuleId: the replication rule id composed of [0-9 A-Z a-z _ -]
// RETURNS:
//     - *api.GetBucketReplicationProgressResult: the process of the bucket replication
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketReplicationProgress(bucket string, replicationRuleId string) (
	*api.GetBucketReplicationProgressResult, error) {
	return api.GetBucketReplicationProgress(c, bucket, replicationRuleId, c.BosContext)
}

// PutBucketEncryption - set the bucket encryption config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
//     - algorithm: the encryption algorithm name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketEncryption(bucket, algorithm string) error {
	return api.PutBucketEncryption(c, bucket, algorithm, c.BosContext)
}

// GetBucketEncryption - get the bucket encryption config
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - string: the encryption algorithm name
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketEncryption(bucket string) (string, error) {
	return api.GetBucketEncryption(c, bucket, c.BosContext)
}

// DeleteBucketEncryption - delete the bucket encryption config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketEncryption(bucket string) error {
	return api.DeleteBucketEncryption(c, bucket, c.BosContext)
}

// PutBucketStaticWebsite - set the bucket static website config
//
// PARAMS:
//     - bucket: the bucket name
//     - config: the static website config body stream
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketStaticWebsite(bucket string, config *bce.Body) error {
	return api.PutBucketStaticWebsite(c, bucket, config, c.BosContext)
}

// PutBucketStaticWebsiteFromString - set the bucket static website config from json string
//
// PARAMS:
//     - bucket: the bucket name
//     - jsonConfig: the static website config json string
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketStaticWebsiteFromString(bucket, jsonConfig string) error {
	body, err := bce.NewBodyFromString(jsonConfig)
	if err != nil {
		return err
	}
	return api.PutBucketStaticWebsite(c, bucket, body, c.BosContext)
}

// PutBucketStaticWebsiteFromStruct - set the bucket static website config from struct
//
// PARAMS:
//     - bucket: the bucket name
//     - confObj: the static website config object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketStaticWebsiteFromStruct(bucket string,
	confObj *api.PutBucketStaticWebsiteArgs) error {
	jsonBytes, jsonErr := json.Marshal(confObj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	return api.PutBucketStaticWebsite(c, bucket, body, c.BosContext)
}

// SimplePutBucketStaticWebsite - simple set the bucket static website config
//
// PARAMS:
//     - bucket: the bucket name
//     - index: the static website config for index file name
//     - notFound: the static website config for notFound file name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) SimplePutBucketStaticWebsite(bucket, index, notFound string) error {
	confObj := &api.PutBucketStaticWebsiteArgs{index, notFound}
	return c.PutBucketStaticWebsiteFromStruct(bucket, confObj)
}

// GetBucketStaticWebsite - get the bucket static website config
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - result: the static website config result object
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketStaticWebsite(bucket string) (
	*api.GetBucketStaticWebsiteResult, error) {
	return api.GetBucketStaticWebsite(c, bucket, c.BosContext)
}

// DeleteBucketStaticWebsite - delete the bucket static website config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketStaticWebsite(bucket string) error {
	return api.DeleteBucketStaticWebsite(c, bucket, c.BosContext)
}

// PutBucketCors - set the bucket CORS config
//
// PARAMS:
//     - bucket: the bucket name
//     - config: the bucket CORS config body stream
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketCors(bucket string, config *bce.Body) error {
	return api.PutBucketCors(c, bucket, config, c.BosContext)
}

// PutBucketCorsFromFile - set the bucket CORS config from json config file
//
// PARAMS:
//     - bucket: the bucket name
//     - filename: the bucket CORS json config file name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketCorsFromFile(bucket, filename string) error {
	body, err := bce.NewBodyFromFile(filename)
	if err != nil {
		return err
	}
	return api.PutBucketCors(c, bucket, body, c.BosContext)
}

// PutBucketCorsFromString - set the bucket CORS config from json config string
//
// PARAMS:
//     - bucket: the bucket name
//     - filename: the bucket CORS json config string
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketCorsFromString(bucket, jsonConfig string) error {
	body, err := bce.NewBodyFromString(jsonConfig)
	if err != nil {
		return err
	}
	return api.PutBucketCors(c, bucket, body, c.BosContext)
}

// PutBucketCorsFromStruct - set the bucket CORS config from json config object
//
// PARAMS:
//     - bucket: the bucket name
//     - filename: the bucket CORS json config object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketCorsFromStruct(bucket string, confObj *api.PutBucketCorsArgs) error {
	jsonBytes, jsonErr := json.Marshal(confObj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	return api.PutBucketCors(c, bucket, body, c.BosContext)
}

// GetBucketCors - get the bucket CORS config
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - result: the bucket CORS config result object
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketCors(bucket string) (*api.GetBucketCorsResult, error) {
	return api.GetBucketCors(c, bucket, c.BosContext)
}

// DeleteBucketCors - delete the bucket CORS config of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketCors(bucket string) error {
	return api.DeleteBucketCors(c, bucket, c.BosContext)
}

// PutBucketCopyrightProtection - set the copyright protection config of the given bucket
//
// PARAMS:
//     - cli: the client agent which can perform sending request
//     - bucket: the bucket name
//     - resources: the resource items in the bucket to be protected
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketCopyrightProtection(bucket string, resources ...string) error {
	return api.PutBucketCopyrightProtection(c, c.BosContext, bucket, resources...)
}

// GetBucketCopyrightProtection - get the bucket copyright protection config
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - result: the bucket copyright protection config resources
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketCopyrightProtection(bucket string) ([]string, error) {
	return api.GetBucketCopyrightProtection(c, bucket, c.BosContext)
}

// DeleteBucketCopyrightProtection - delete the bucket copyright protection config
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketCopyrightProtection(bucket string) error {
	return api.DeleteBucketCopyrightProtection(c, bucket, c.BosContext)
}

// PutObject - upload a new object or rewrite the existed object with raw stream
//
// PARAMS:
//     - bucket: the name of the bucket to store the object
//     - object: the name of the object
//     - body: the object content body
//     - args: the optional arguments
// RETURNS:
//     - string: etag of the uploaded object
//     - error: the uploaded error if any occurs
func (c *Client) PutObject(bucket, object string, body *bce.Body,
	args *api.PutObjectArgs) (string, error) {
	etag, _, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, err
}

// BasicPutObject - the basic interface of uploading an object
//
// PARAMS:
//     - bucket: the name of the bucket to store the object
//     - object: the name of the object
//     - body: the object content body
// RETURNS:
//     - string: etag of the uploaded object
//     - error: the uploaded error if any occurs
func (c *Client) BasicPutObject(bucket, object string, body *bce.Body) (string, error) {
	etag, _, err := api.PutObject(c, bucket, object, body, nil, c.BosContext)
	return etag, err
}

// PutObjectFromBytes - upload a new object or rewrite the existed object from a byte array
//
// PARAMS:
//     - bucket: the name of the bucket to store the object
//     - object: the name of the object
//     - bytesArr: the content byte array
//     - args: the optional arguments
// RETURNS:
//     - string: etag of the uploaded object
//     - error: the uploaded error if any occurs
func (c *Client) PutObjectFromBytes(bucket, object string, bytesArr []byte,
	args *api.PutObjectArgs) (string, error) {
	body, err := bce.NewBodyFromBytes(bytesArr)
	if err != nil {
		return "", err
	}
	etag, _, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, err

}

// PutObjectFromString - upload a new object or rewrite the existed object from a string
//
// PARAMS:
//     - bucket: the name of the bucket to store the object
//     - object: the name of the object
//     - content: the content string
//     - args: the optional arguments
// RETURNS:
//     - string: etag of the uploaded object
//     - error: the uploaded error if any occurs
func (c *Client) PutObjectFromString(bucket, object, content string,
	args *api.PutObjectArgs) (string, error) {
	body, err := bce.NewBodyFromString(content)
	if err != nil {
		return "", err
	}
	etag, _, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, err

}

// PutObjectFromFile - upload a new object or rewrite the existed object from a local file
//
// PARAMS:
//     - bucket: the name of the bucket to store the object
//     - object: the name of the object
//     - fileName: the local file full path name
//     - args: the optional arguments
// RETURNS:
//     - string: etag of the uploaded object
//     - error: the uploaded error if any occurs
func (c *Client) PutObjectFromFile(bucket, object, fileName string,
	args *api.PutObjectArgs) (string, error) {
	body, err := bce.NewBodyFromFile(fileName)
	if err != nil {
		return "", err
	}
	etag, _, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, err
}

// PutObjectFromStream - upload a new object or rewrite the existed object from stream
//
// PARAMS:
//     - bucket: the name of the bucket to store the object
//     - object: the name of the object
//     - fileName: the local file full path name
//     - args: the optional arguments
// RETURNS:
//     - string: etag of the uploaded object
//     - error: the uploaded error if any occurs
func (c *Client) PutObjectFromStream(bucket, object string, reader io.Reader,
	args *api.PutObjectArgs) (string, error) {
	body, err := bce.NewBodyFromSizedReader(reader, -1)
	if err != nil {
		return "", err
	}
	etag, _, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, err
}

func (c *Client) PutObjectFromFileWithCallback(bucket, object, fileName string,
	args *api.PutObjectArgs) (string, *api.PutObjectResult, error) {
	body, err := bce.NewBodyFromFile(fileName)
	if err != nil {
		return "", nil, err
	}
	etag, putObjectResult, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, putObjectResult, err
}

func (c *Client) PutObjectWithCallback(bucket, object string, body *bce.Body,
	args *api.PutObjectArgs) (string, *api.PutObjectResult, error) {
	etag, putObjectResult, err := api.PutObject(c, bucket, object, body, args, c.BosContext)
	return etag, putObjectResult, err
}

// CopyObject - copy a remote object to another one
//
// PARAMS:
//     - bucket: the name of the destination bucket
//     - object: the name of the destination object
//     - srcBucket: the name of the source bucket
//     - srcObject: the name of the source object
//     - args: the optional arguments for copying object which are MetadataDirective, StorageClass,
//       IfMatch, IfNoneMatch, ifModifiedSince, IfUnmodifiedSince
// RETURNS:
//     - *api.CopyObjectResult: result struct which contains "ETag" and "LastModified" fields
//     - error: any error if it occurs
func (c *Client) CopyObject(bucket, object, srcBucket, srcObject string,
	args *api.CopyObjectArgs) (*api.CopyObjectResult, error) {
	source := fmt.Sprintf("/%s/%s", srcBucket, srcObject)
	return api.CopyObject(c, bucket, object, source, args, c.BosContext)
}

// BasicCopyObject - the basic interface of copying a object to another one
//
// PARAMS:
//     - bucket: the name of the destination bucket
//     - object: the name of the destination object
//     - srcBucket: the name of the source bucket
//     - srcObject: the name of the source object
// RETURNS:
//     - *api.CopyObjectResult: result struct which contains "ETag" and "LastModified" fields
//     - error: any error if it occurs
func (c *Client) BasicCopyObject(bucket, object, srcBucket,
	srcObject string) (*api.CopyObjectResult, error) {
	source := fmt.Sprintf("/%s/%s", srcBucket, srcObject)
	return api.CopyObject(c, bucket, object, source, nil, c.BosContext)
}

// GetObject - get the given object with raw stream return
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - args: the optional args in querysring
//     - ranges: the optional range start and end to get the given object
// RETURNS:
//     - *api.GetObjectResult: result struct which contains "Body" and header fields
//       for details reference https://cloud.baidu.com/doc/BOS/API.html#GetObject.E6.8E.A5.E5.8F.A3
//     - error: any error if it occurs
func (c *Client) GetObject(bucket, object string, args map[string]string,
	ranges ...int64) (*api.GetObjectResult, error) {
	return api.GetObject(c, bucket, object, c.BosContext, args, ranges...)
}

// BasicGetObject - the basic interface of geting the given object
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
// RETURNS:
//     - *api.GetObjectResult: result struct which contains "Body" and header fields
//       for details reference https://cloud.baidu.com/doc/BOS/API.html#GetObject.E6.8E.A5.E5.8F.A3
//     - error: any error if it occurs
func (c *Client) BasicGetObject(bucket, object string) (*api.GetObjectResult, error) {
	return api.GetObject(c, bucket, object, c.BosContext, nil)
}

// BasicGetObjectToFile - use basic interface to get the given object to the given file path
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - filePath: the file path to store the object content
// RETURNS:
//     - error: any error if it occurs
func (c *Client) BasicGetObjectToFile(bucket, object, filePath string) error {
	res, err := api.GetObject(c, bucket, object, c.BosContext, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	file, fileErr := os.OpenFile(filePath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if fileErr != nil {
		return fileErr
	}
	defer file.Close()

	written, writeErr := io.CopyN(file, res.Body, res.ContentLength)
	if writeErr != nil {
		return writeErr
	}
	if written != res.ContentLength {
		return fmt.Errorf("written content size does not match the response content")
	}
	return nil
}

// GetObjectMeta - get the given object metadata
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
// RETURNS:
//     - *api.GetObjectMetaResult: metadata result, for details reference
//       https://cloud.baidu.com/doc/BOS/API.html#GetObjectMeta.E6.8E.A5.E5.8F.A3
//     - error: any error if it occurs
func (c *Client) GetObjectMeta(bucket, object string) (*api.GetObjectMetaResult, error) {
	return api.GetObjectMeta(c, bucket, object, c.BosContext)
}

// SelectObject - select the object content
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - args: the optional arguments to select the object
// RETURNS:
//     - *api.SelectObjectResult: select object result
//     - error: any error if it occurs
func (c *Client) SelectObject(bucket, object string, args *api.SelectObjectArgs) (*api.SelectObjectResult, error) {
	return api.SelectObject(c, bucket, object, args, c.BosContext)
}

// FetchObject - fetch the object content from the given source and store
//
// PARAMS:
//     - bucket: the name of the bucket to store
//     - object: the name of the object to store
//     - source: fetch source url
//     - args: the optional arguments to fetch the object
// RETURNS:
//     - *api.FetchObjectResult: result struct with Code, Message, RequestId and JobId fields
//     - error: any error if it occurs
func (c *Client) FetchObject(bucket, object, source string,
	args *api.FetchObjectArgs) (*api.FetchObjectResult, error) {
	return api.FetchObject(c, bucket, object, source, args, c.BosContext)
}

// BasicFetchObject - the basic interface of the fetch object api
//
// PARAMS:
//     - bucket: the name of the bucket to store
//     - object: the name of the object to store
//     - source: fetch source url
// RETURNS:
//     - *api.FetchObjectResult: result struct with Code, Message, RequestId and JobId fields
//     - error: any error if it occurs
func (c *Client) BasicFetchObject(bucket, object, source string) (*api.FetchObjectResult, error) {
	return api.FetchObject(c, bucket, object, source, nil, c.BosContext)
}

// SimpleFetchObject - fetch object with simple arguments interface
//
// PARAMS:
//     - bucket: the name of the bucket to store
//     - object: the name of the object to store
//     - source: fetch source url
//     - mode: fetch mode which supports sync and async
//     - storageClass: the storage class of the fetched object
// RETURNS:
//     - *api.FetchObjectResult: result struct with Code, Message, RequestId and JobId fields
//     - error: any error if it occurs
func (c *Client) SimpleFetchObject(bucket, object, source, mode,
	storageClass string) (*api.FetchObjectResult, error) {
	args := &api.FetchObjectArgs{mode, storageClass, ""}
	return api.FetchObject(c, bucket, object, source, args, c.BosContext)
}

// AppendObject - append the given content to a new or existed object which is appendable
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - content: the append object stream
//     - args: the optional arguments to append object
// RETURNS:
//     - *api.AppendObjectResult: the result of the appended object
//     - error: any error if it occurs
func (c *Client) AppendObject(bucket, object string, content *bce.Body,
	args *api.AppendObjectArgs) (*api.AppendObjectResult, error) {
	return api.AppendObject(c, bucket, object, content, args, c.BosContext)
}

// SimpleAppendObject - the interface to append object with simple offset argument
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - content: the append object stream
//     - offset: the offset of where to append
// RETURNS:
//     - *api.AppendObjectResult: the result of the appended object
//     - error: any error if it occurs
func (c *Client) SimpleAppendObject(bucket, object string, content *bce.Body,
	offset int64) (*api.AppendObjectResult, error) {
	return api.AppendObject(c, bucket, object, content, &api.AppendObjectArgs{Offset: offset}, c.BosContext)
}

// SimpleAppendObjectFromString - the simple interface of appending an object from a string
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - content: the object string to append
//     - offset: the offset of where to append
// RETURNS:
//     - *api.AppendObjectResult: the result of the appended object
//     - error: any error if it occurs
func (c *Client) SimpleAppendObjectFromString(bucket, object, content string,
	offset int64) (*api.AppendObjectResult, error) {
	body, err := bce.NewBodyFromString(content)
	if err != nil {
		return nil, err
	}
	return api.AppendObject(c, bucket, object, body, &api.AppendObjectArgs{Offset: offset}, c.BosContext)
}

// SimpleAppendObjectFromFile - the simple interface of appending an object from a file
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - filePath: the full file path
//     - offset: the offset of where to append
// RETURNS:
//     - *api.AppendObjectResult: the result of the appended object
//     - error: any error if it occurs
func (c *Client) SimpleAppendObjectFromFile(bucket, object, filePath string,
	offset int64) (*api.AppendObjectResult, error) {
	body, err := bce.NewBodyFromFile(filePath)
	if err != nil {
		return nil, err
	}
	return api.AppendObject(c, bucket, object, body, &api.AppendObjectArgs{Offset: offset}, c.BosContext)
}

// DeleteObject - delete the given object
//
// PARAMS:
//     - bucket: the name of the bucket to delete
//     - object: the name of the object to delete
// RETURNS:
//     - error: any error if it occurs
func (c *Client) DeleteObject(bucket, object string) error {
	return api.DeleteObject(c, bucket, object, c.BosContext)
}

// DeleteMultipleObjects - delete a list of objects
//
// PARAMS:
//     - bucket: the name of the bucket to delete
//     - objectListStream: the object list stream to be deleted
// RETURNS:
//     - *api.DeleteMultipleObjectsResult: the delete information
//     - error: any error if it occurs
func (c *Client) DeleteMultipleObjects(bucket string,
	objectListStream *bce.Body) (*api.DeleteMultipleObjectsResult, error) {
	return api.DeleteMultipleObjects(c, bucket, objectListStream, c.BosContext)
}

// DeleteMultipleObjectsFromString - delete a list of objects with json format string
//
// PARAMS:
//     - bucket: the name of the bucket to delete
//     - objectListString: the object list string to be deleted
// RETURNS:
//     - *api.DeleteMultipleObjectsResult: the delete information
//     - error: any error if it occurs
func (c *Client) DeleteMultipleObjectsFromString(bucket,
	objectListString string) (*api.DeleteMultipleObjectsResult, error) {
	body, err := bce.NewBodyFromString(objectListString)
	if err != nil {
		return nil, err
	}
	return api.DeleteMultipleObjects(c, bucket, body, c.BosContext)
}

// DeleteMultipleObjectsFromStruct - delete a list of objects with object list struct
//
// PARAMS:
//     - bucket: the name of the bucket to delete
//     - objectListStruct: the object list struct to be deleted
// RETURNS:
//     - *api.DeleteMultipleObjectsResult: the delete information
//     - error: any error if it occurs
func (c *Client) DeleteMultipleObjectsFromStruct(bucket string,
	objectListStruct *api.DeleteMultipleObjectsArgs) (*api.DeleteMultipleObjectsResult, error) {
	jsonBytes, jsonErr := json.Marshal(objectListStruct)
	if jsonErr != nil {
		return nil, jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return nil, err
	}
	return api.DeleteMultipleObjects(c, bucket, body, c.BosContext)
}

// DeleteMultipleObjectsFromKeyList - delete a list of objects with given key string array
//
// PARAMS:
//     - bucket: the name of the bucket to delete
//     - keyList: the key string list to be deleted
// RETURNS:
//     - *api.DeleteMultipleObjectsResult: the delete information
//     - error: any error if it occurs
func (c *Client) DeleteMultipleObjectsFromKeyList(bucket string,
	keyList []string) (*api.DeleteMultipleObjectsResult, error) {
	if len(keyList) == 0 {
		return nil, fmt.Errorf("the key list to be deleted is empty")
	}
	args := make([]api.DeleteObjectArgs, len(keyList))
	for i, k := range keyList {
		args[i].Key = k
	}
	argsContainer := &api.DeleteMultipleObjectsArgs{args}

	jsonBytes, jsonErr := json.Marshal(argsContainer)
	if jsonErr != nil {
		return nil, jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return nil, err
	}
	return api.DeleteMultipleObjects(c, bucket, body, c.BosContext)
}

// InitiateMultipartUpload - initiate a multipart upload to get a upload ID
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - contentType: the content type of the object to be uploaded which should be specified,
//       otherwise use the default(application/octet-stream)
//     - args: the optional arguments
// RETURNS:
//     - *InitiateMultipartUploadResult: the result data structure
//     - error: nil if ok otherwise the specific error
func (c *Client) InitiateMultipartUpload(bucket, object, contentType string,
	args *api.InitiateMultipartUploadArgs) (*api.InitiateMultipartUploadResult, error) {
	return api.InitiateMultipartUpload(c, bucket, object, contentType, args, c.BosContext)
}

// BasicInitiateMultipartUpload - basic interface to initiate a multipart upload
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
// RETURNS:
//     - *InitiateMultipartUploadResult: the result data structure
//     - error: nil if ok otherwise the specific error
func (c *Client) BasicInitiateMultipartUpload(bucket,
	object string) (*api.InitiateMultipartUploadResult, error) {
	return api.InitiateMultipartUpload(c, bucket, object, "", nil, c.BosContext)
}

// UploadPart - upload the single part in the multipart upload process
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - content: the uploaded part content
//     - args: the optional arguments
// RETURNS:
//     - string: the etag of the uploaded part
//     - error: nil if ok otherwise the specific error
func (c *Client) UploadPart(bucket, object, uploadId string, partNumber int,
	content *bce.Body, args *api.UploadPartArgs) (string, error) {
	return api.UploadPart(c, bucket, object, uploadId, partNumber, content, args, c.BosContext)
}

// BasicUploadPart - basic interface to upload the single part in the multipart upload process
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - content: the uploaded part content
// RETURNS:
//     - string: the etag of the uploaded part
//     - error: nil if ok otherwise the specific error
func (c *Client) BasicUploadPart(bucket, object, uploadId string, partNumber int,
	content *bce.Body) (string, error) {
	return api.UploadPart(c, bucket, object, uploadId, partNumber, content, nil, c.BosContext)
}

// UploadPartFromBytes - upload the single part in the multipart upload process
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - content: the uploaded part content
//     - args: the optional arguments
// RETURNS:
//     - string: the etag of the uploaded part
//     - error: nil if ok otherwise the specific error
func (c *Client) UploadPartFromBytes(bucket, object, uploadId string, partNumber int,
	content []byte, args *api.UploadPartArgs) (string, error) {
	return api.UploadPartFromBytes(c, bucket, object, uploadId, partNumber, content, args, c.BosContext)
}

// UploadPartCopy - copy the multipart object
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - srcBucket: the source bucket
//     - srcObject: the source object
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
//     - args: the optional arguments
// RETURNS:
//     - *CopyObjectResult: the lastModified and eTag of the part
//     - error: nil if ok otherwise the specific error
func (c *Client) UploadPartCopy(bucket, object, srcBucket, srcObject, uploadId string,
	partNumber int, args *api.UploadPartCopyArgs) (*api.CopyObjectResult, error) {
	source := fmt.Sprintf("/%s/%s", srcBucket, srcObject)
	return api.UploadPartCopy(c, bucket, object, source, uploadId, partNumber, args, c.BosContext)
}

// BasicUploadPartCopy - basic interface to copy the multipart object
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - srcBucket: the source bucket
//     - srcObject: the source object
//     - uploadId: the multipart upload id
//     - partNumber: the current part number
// RETURNS:
//     - *CopyObjectResult: the lastModified and eTag of the part
//     - error: nil if ok otherwise the specific error
func (c *Client) BasicUploadPartCopy(bucket, object, srcBucket, srcObject, uploadId string,
	partNumber int) (*api.CopyObjectResult, error) {
	source := fmt.Sprintf("/%s/%s", srcBucket, srcObject)
	return api.UploadPartCopy(c, bucket, object, source, uploadId, partNumber, nil, c.BosContext)
}

// CompleteMultipartUpload - finish a multipart upload operation with parts stream
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
//     - parts: all parts info stream
//     - meta: user defined meta data
// RETURNS:
//     - *CompleteMultipartUploadResult: the result data
//     - error: nil if ok otherwise the specific error
func (c *Client) CompleteMultipartUpload(bucket, object, uploadId string,
	body *bce.Body, args *api.CompleteMultipartUploadArgs) (*api.CompleteMultipartUploadResult, error) {
	return api.CompleteMultipartUpload(c, bucket, object, uploadId, body, args, c.BosContext)
}

// CompleteMultipartUploadFromStruct - finish a multipart upload operation with parts struct
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
//     - args: args info struct object
// RETURNS:
//     - *CompleteMultipartUploadResult: the result data
//     - error: nil if ok otherwise the specific error
func (c *Client) CompleteMultipartUploadFromStruct(bucket, object, uploadId string,
	args *api.CompleteMultipartUploadArgs) (*api.CompleteMultipartUploadResult, error) {
	jsonBytes, jsonErr := json.Marshal(args)
	if jsonErr != nil {
		return nil, jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return nil, err
	}
	return api.CompleteMultipartUpload(c, bucket, object, uploadId, body, args, c.BosContext)
}

// AbortMultipartUpload - abort a multipart upload operation
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
// RETURNS:
//     - error: nil if ok otherwise the specific error
func (c *Client) AbortMultipartUpload(bucket, object, uploadId string) error {
	return api.AbortMultipartUpload(c, bucket, object, uploadId, c.BosContext)
}

// ListParts - list the successfully uploaded parts info by upload id
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
//     - args: the optional arguments
// RETURNS:
//     - *ListPartsResult: the uploaded parts info result
//     - error: nil if ok otherwise the specific error
func (c *Client) ListParts(bucket, object, uploadId string,
	args *api.ListPartsArgs) (*api.ListPartsResult, error) {
	return api.ListParts(c, bucket, object, uploadId, args, c.BosContext)
}

// BasicListParts - basic interface to list the successfully uploaded parts info by upload id
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - uploadId: the multipart upload id
// RETURNS:
//     - *ListPartsResult: the uploaded parts info result
//     - error: nil if ok otherwise the specific error
func (c *Client) BasicListParts(bucket, object, uploadId string) (*api.ListPartsResult, error) {
	return api.ListParts(c, bucket, object, uploadId, nil, c.BosContext)
}

// ListMultipartUploads - list the unfinished uploaded parts of the given bucket
//
// PARAMS:
//     - bucket: the destination bucket name
//     - args: the optional arguments
// RETURNS:
//     - *ListMultipartUploadsResult: the unfinished uploaded parts info result
//     - error: nil if ok otherwise the specific error
func (c *Client) ListMultipartUploads(bucket string,
	args *api.ListMultipartUploadsArgs) (*api.ListMultipartUploadsResult, error) {
	return api.ListMultipartUploads(c, bucket, args, c.BosContext)
}

// BasicListMultipartUploads - basic interface to list the unfinished uploaded parts
//
// PARAMS:
//     - bucket: the destination bucket name
// RETURNS:
//     - *ListMultipartUploadsResult: the unfinished uploaded parts info result
//     - error: nil if ok otherwise the specific error
func (c *Client) BasicListMultipartUploads(bucket string) (
	*api.ListMultipartUploadsResult, error) {
	return api.ListMultipartUploads(c, bucket, nil, c.BosContext)
}

// UploadSuperFile - parallel upload the super file by using the multipart upload interface
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - fileName: the local full path filename of the super file
//     - storageClass: the storage class to be set to the uploaded file
// RETURNS:
//     - error: nil if ok otherwise the specific error
func (c *Client) UploadSuperFile(bucket, object, fileName, storageClass string) error {
	// Get the file size and check the size for multipart upload
	file, fileErr := os.Open(fileName)
	if fileErr != nil {
		return fileErr
	}
	oldTimeout := c.Config.ConnectionTimeoutInMillis
	c.Config.ConnectionTimeoutInMillis = 0
	defer func() {
		c.Config.ConnectionTimeoutInMillis = oldTimeout
		file.Close()
	}()
	fileInfo, infoErr := file.Stat()
	if infoErr != nil {
		return infoErr
	}
	size := fileInfo.Size()
	if size < MIN_MULTIPART_SIZE || c.MultipartSize < MIN_MULTIPART_SIZE {
		return bce.NewBceClientError("multipart size should not be less than 1MB")
	}

	// Calculate part size and total part number
	partSize := (c.MultipartSize + MULTIPART_ALIGN - 1) / MULTIPART_ALIGN * MULTIPART_ALIGN
	partNum := (size + partSize - 1) / partSize
	if partNum > MAX_PART_NUMBER {
		partSize = (size + MAX_PART_NUMBER - 1) / MAX_PART_NUMBER
		partSize = (partSize + MULTIPART_ALIGN - 1) / MULTIPART_ALIGN * MULTIPART_ALIGN
		partNum = (size + partSize - 1) / partSize
	}
	log.Debugf("starting upload super file, total parts: %d, part size: %d", partNum, partSize)

	// Inner wrapper function of parallel uploading each part to get the ETag of the part
	uploadPart := func(bucket, object, uploadId string, partNumber int, body *bce.Body,
		result chan *api.UploadInfoType, ret chan error, id int64, pool chan int64) {
		etag, err := c.BasicUploadPart(bucket, object, uploadId, partNumber, body)
		if err != nil {
			result <- nil
			ret <- err
		} else {
			result <- &api.UploadInfoType{partNumber, etag}
		}
		pool <- id
	}

	// Do the parallel multipart upload
	resp, err := c.InitiateMultipartUpload(bucket, object, "",
		&api.InitiateMultipartUploadArgs{StorageClass: storageClass})
	if err != nil {
		return err
	}
	uploadId := resp.UploadId
	uploadedResult := make(chan *api.UploadInfoType, partNum)
	retChan := make(chan error, partNum)
	workerPool := make(chan int64, c.MaxParallel)
	for i := int64(0); i < c.MaxParallel; i++ {
		workerPool <- i
	}
	for partId := int64(1); partId <= partNum; partId++ {
		uploadSize := partSize
		offset := (partId - 1) * partSize
		left := size - offset
		if uploadSize > left {
			uploadSize = left
		}
		partBody, _ := bce.NewBodyFromSectionFile(file, offset, uploadSize)
		select { // wait until get a worker to upload
		case workerId := <-workerPool:
			go uploadPart(bucket, object, uploadId, int(partId), partBody,
				uploadedResult, retChan, workerId, workerPool)
		case uploadPartErr := <-retChan:
			c.AbortMultipartUpload(bucket, object, uploadId)
			return uploadPartErr
		}
	}

	// Check the return of each part uploading, and decide to complete or abort it
	completeArgs := &api.CompleteMultipartUploadArgs{
		Parts: make([]api.UploadInfoType, partNum),
	}
	for i := partNum; i > 0; i-- {
		uploaded := <-uploadedResult
		if uploaded == nil { // error occurs and not be caught in `select' statement
			c.AbortMultipartUpload(bucket, object, uploadId)
			return <-retChan
		}
		completeArgs.Parts[uploaded.PartNumber-1] = *uploaded
		log.Debugf("upload part %d success, etag: %s", uploaded.PartNumber, uploaded.ETag)
	}
	if _, err := c.CompleteMultipartUploadFromStruct(bucket, object,
		uploadId, completeArgs); err != nil {
		c.AbortMultipartUpload(bucket, object, uploadId)
		return err
	}
	return nil
}

// DownloadSuperFile - parallel download the super file using the get object with range
//
// PARAMS:
//     - bucket: the destination bucket name
//     - object: the destination object name
//     - fileName: the local full path filename to store the object
// RETURNS:
//     - error: nil if ok otherwise the specific error
func (c *Client) DownloadSuperFile(bucket, object, fileName string) (err error) {
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	oldTimeout := c.Config.ConnectionTimeoutInMillis
	c.Config.ConnectionTimeoutInMillis = 0
	defer func() {
		c.Config.ConnectionTimeoutInMillis = oldTimeout
		file.Close()
		if err != nil {
			os.Remove(fileName)
		}
	}()

	meta, err := c.GetObjectMeta(bucket, object)
	if err != nil {
		return
	}
	size := meta.ContentLength
	partSize := (c.MultipartSize + MULTIPART_ALIGN - 1) / MULTIPART_ALIGN * MULTIPART_ALIGN
	partNum := (size + partSize - 1) / partSize
	log.Debugf("starting download super file, total parts: %d, part size: %d", partNum, partSize)

	doneChan := make(chan struct{}, partNum)
	abortChan := make(chan struct{})

	// Set up multiple goroutine workers to download the object
	workerPool := make(chan int64, c.MaxParallel)
	for i := int64(0); i < c.MaxParallel; i++ {
		workerPool <- i
	}
	for i := int64(0); i < partNum; i++ {
		rangeStart := i * partSize
		rangeEnd := (i+1)*partSize - 1
		if rangeEnd > size-1 {
			rangeEnd = size - 1
		}
		select {
		case workerId := <-workerPool:
			go func(rangeStart, rangeEnd, workerId int64) {
				res, rangeGetErr := c.GetObject(bucket, object, nil, rangeStart, rangeEnd)
				if rangeGetErr != nil {
					log.Errorf("download object part(offset:%d, size:%d) failed: %v",
						rangeStart, res.ContentLength, rangeGetErr)
					abortChan <- struct{}{}
					err = rangeGetErr
					return
				}
				defer res.Body.Close()
				log.Debugf("writing part %d with offset=%d, size=%d", rangeStart/partSize,
					rangeStart, res.ContentLength)
				buf := make([]byte, 4096)
				offset := rangeStart
				for {
					n, e := res.Body.Read(buf)
					if e != nil && e != io.EOF {
						abortChan <- struct{}{}
						err = e
						return
					}
					if n == 0 {
						break
					}
					if _, writeErr := file.WriteAt(buf[:n], offset); writeErr != nil {
						abortChan <- struct{}{}
						err = writeErr
						return
					}
					offset += int64(n)
				}
				log.Debugf("writing part %d done", rangeStart/partSize)
				workerPool <- workerId
				doneChan <- struct{}{}
			}(rangeStart, rangeEnd, workerId)
		case <-abortChan: // abort range get if error occurs during downloading any part
			return
		}
	}

	// Wait for writing to local file done
	for i := partNum; i > 0; i-- {
		<-doneChan
	}
	return nil
}

// GeneratePresignedUrl - generate an authorization url with expire time and optional arguments
//
// PARAMS:
//     - bucket: the target bucket name
//     - object: the target object name
//     - expireInSeconds: the expire time in seconds of the signed url
//     - method: optional sign method, default is GET
//     - headers: optional sign headers, default just set the Host
//     - params: optional sign params, default is empty
// RETURNS:
//     - string: the presigned url with authorization string
func (c *Client) GeneratePresignedUrl(bucket, object string, expireInSeconds int, method string,
	headers, params map[string]string) string {
	return api.GeneratePresignedUrl(c.Config, c.Signer, bucket, object,
		expireInSeconds, method, headers, params)
}

func (c *Client) GeneratePresignedUrlPathStyle(bucket, object string, expireInSeconds int, method string,
	headers, params map[string]string) string {
	return api.GeneratePresignedUrlPathStyle(c.Config, c.Signer, bucket, object,
		expireInSeconds, method, headers, params)
}

// BasicGeneratePresignedUrl - basic interface to generate an authorization url with expire time
//
// PARAMS:
//     - bucket: the target bucket name
//     - object: the target object name
//     - expireInSeconds: the expire time in seconds of the signed url
// RETURNS:
//     - string: the presigned url with authorization string
func (c *Client) BasicGeneratePresignedUrl(bucket, object string, expireInSeconds int) string {
	return api.GeneratePresignedUrl(c.Config, c.Signer, bucket, object,
		expireInSeconds, "", nil, nil)
}

// PutObjectAcl - set the ACL of the given object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - aclBody: the acl json body stream
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAcl(bucket, object string, aclBody *bce.Body) error {
	return api.PutObjectAcl(c, bucket, object, "", nil, nil, aclBody, c.BosContext)
}

// PutObjectAclFromCanned - set the canned acl of the given object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - cannedAcl: the cannedAcl string
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAclFromCanned(bucket, object, cannedAcl string) error {
	return api.PutObjectAcl(c, bucket, object, cannedAcl, nil, nil, nil, c.BosContext)
}

// PutObjectAclGrantRead - set the canned grant read acl of the given object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - ids: the user id list to grant read for this object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAclGrantRead(bucket, object string, ids ...string) error {
	return api.PutObjectAcl(c, bucket, object, "", ids, nil, nil, c.BosContext)
}

// PutObjectAclGrantFullControl - set the canned grant full-control acl of the given object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - ids: the user id list to grant full-control for this object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAclGrantFullControl(bucket, object string, ids ...string) error {
	return api.PutObjectAcl(c, bucket, object, "", nil, ids, nil, c.BosContext)
}

// PutObjectAclFromFile - set the acl of the given object with acl json file name
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - aclFile: the acl file name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAclFromFile(bucket, object, aclFile string) error {
	body, err := bce.NewBodyFromFile(aclFile)
	if err != nil {
		return err
	}
	return api.PutObjectAcl(c, bucket, object, "", nil, nil, body, c.BosContext)
}

// PutObjectAclFromString - set the acl of the given object with acl json string
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - aclString: the acl string with json format
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAclFromString(bucket, object, aclString string) error {
	body, err := bce.NewBodyFromString(aclString)
	if err != nil {
		return err
	}
	return api.PutObjectAcl(c, bucket, object, "", nil, nil, body, c.BosContext)
}

// PutObjectAclFromStruct - set the acl of the given object with acl data structure
//
// PARAMS:
//     - bucket: the bucket name
//     - aclObj: the acl struct object
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutObjectAclFromStruct(bucket, object string, aclObj *api.PutObjectAclArgs) error {
	jsonBytes, jsonErr := json.Marshal(aclObj)
	if jsonErr != nil {
		return jsonErr
	}
	body, err := bce.NewBodyFromBytes(jsonBytes)
	if err != nil {
		return err
	}
	return api.PutObjectAcl(c, bucket, object, "", nil, nil, body, c.BosContext)
}

// GetObjectAcl - get the acl of the given object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
// RETURNS:
//     - *api.GetObjectAclResult: the result of the object acl
//     - error: nil if success otherwise the specific error
func (c *Client) GetObjectAcl(bucket, object string) (*api.GetObjectAclResult, error) {
	return api.GetObjectAcl(c, bucket, object, c.BosContext)
}

// DeleteObjectAcl - delete the acl of the given object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteObjectAcl(bucket, object string) error {
	return api.DeleteObjectAcl(c, bucket, object, c.BosContext)
}

// RestoreObject - restore the archive object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - restoreDays: the effective time of restore
//     - restoreTier: the tier of restore
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) RestoreObject(bucket string, object string, restoreDays int, restoreTier string) error {
	if _, ok := api.VALID_RESTORE_TIER[restoreTier]; !ok {
		return errors.New("invalid restore tier")
	}

	if restoreDays <= 0 {
		return errors.New("invalid restore days")
	}

	args := api.ArchiveRestoreArgs{
		RestoreTier: restoreTier,
		RestoreDays: restoreDays,
	}
	return api.RestoreObject(c, bucket, object, args, c.BosContext)
}

// PutBucketTrash - put the bucket trash
//
// PARAMS:
//     - bucket: the bucket name
//     - trashReq: the trash request
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketTrash(bucket string, trashReq api.PutBucketTrashReq) error {
	return api.PutBucketTrash(c, bucket, trashReq, c.BosContext)
}

// GetBucketTrash - get the bucket trash
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - *api.GetBucketTrashResult,: the result of the bucket trash
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketTrash(bucket string) (*api.GetBucketTrashResult, error) {
	return api.GetBucketTrash(c, bucket, c.BosContext)
}

// DeleteBucketTrash - delete the trash of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketTrash(bucket string) error {
	return api.DeleteBucketTrash(c, bucket, c.BosContext)
}

// PutBucketNotification - put the bucket notification
//
// PARAMS:
//     - bucket: the bucket name
//     - putBucketNotificationReq: the bucket notification request
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) PutBucketNotification(bucket string, putBucketNotificationReq api.PutBucketNotificationReq) error {
	return api.PutBucketNotification(c, bucket, putBucketNotificationReq, c.BosContext)
}

// GetBucketNotification - get the bucket notification
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - *api.PutBucketNotificationReq,: the result of the bucket notification
//     - error: nil if success otherwise the specific error
func (c *Client) GetBucketNotification(bucket string) (*api.PutBucketNotificationReq, error) {
	return api.GetBucketNotification(c, bucket, c.BosContext)
}

// DeleteBucketNotification - delete the notification of the given bucket
//
// PARAMS:
//     - bucket: the bucket name
// RETURNS:
//     - error: nil if success otherwise the specific error
func (c *Client) DeleteBucketNotification(bucket string) error {
	return api.DeleteBucketNotification(c, bucket, c.BosContext)
}

// ParallelUpload - auto multipart upload object
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - filename: the filename
//     - contentType: the content type default(application/octet-stream)
//     - args: the bucket name nil using default
// RETURNS:
//     - *api.CompleteMultipartUploadResult: multipart upload result
//     - error: nil if success otherwise the specific error
func (c *Client) ParallelUpload(bucket string, object string, filename string, contentType string, args *api.InitiateMultipartUploadArgs) (*api.CompleteMultipartUploadResult, error) {

	initiateMultipartUploadResult, err := api.InitiateMultipartUpload(c, bucket, object, contentType, args, c.BosContext)
	if err != nil {
		return nil, err
	}

	partEtags, err := c.parallelPartUpload(bucket, object, filename, initiateMultipartUploadResult.UploadId)
	if err != nil {
		c.AbortMultipartUpload(bucket, object, initiateMultipartUploadResult.UploadId)
		return nil, err
	}

	completeMultipartUploadResult, err := c.CompleteMultipartUploadFromStruct(bucket, object, initiateMultipartUploadResult.UploadId, &api.CompleteMultipartUploadArgs{Parts: partEtags})
	if err != nil {
		c.AbortMultipartUpload(bucket, object, initiateMultipartUploadResult.UploadId)
		return nil, err
	}
	return completeMultipartUploadResult, nil
}

// parallelPartUpload - single part upload
//
// PARAMS:
//     - bucket: the bucket name
//     - object: the object name
//     - filename: the uploadId
//     - uploadId: the uploadId
// RETURNS:
//     - []api.UploadInfoType: multipart upload result
//     - error: nil if success otherwise the specific error
func (c *Client) parallelPartUpload(bucket string, object string, filename string, uploadId string) ([]api.UploadInfoType, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// 分块大小按MULTIPART_ALIGN=1MB对齐
	partSize := (c.MultipartSize +
		MULTIPART_ALIGN - 1) / MULTIPART_ALIGN * MULTIPART_ALIGN

	// 获取文件大小，并计算分块数目，最大分块数MAX_PART_NUMBER=10000
	fileInfo, _ := file.Stat()
	fileSize := fileInfo.Size()
	partNum := (fileSize + partSize - 1) / partSize
	if partNum > MAX_PART_NUMBER { // 超过最大分块数，需调整分块大小
		partSize = (fileSize + MAX_PART_NUMBER + 1) / MAX_PART_NUMBER
		partSize = (partSize + MULTIPART_ALIGN - 1) / MULTIPART_ALIGN * MULTIPART_ALIGN
		partNum = (fileSize + partSize - 1) / partSize
	}

	parallelChan := make(chan int, c.MaxParallel)

	errChan := make(chan error, c.MaxParallel)

	resultChan := make(chan api.UploadInfoType, partNum)

	// 逐个分块上传
	for i := int64(1); i <= partNum; i++ {
		// 计算偏移offset和本次上传的大小uploadSize
		uploadSize := partSize
		offset := partSize * (i - 1)
		left := fileSize - offset
		if left < partSize {
			uploadSize = left
		}

		// 创建指定偏移、指定大小的文件流
		partBody, _ := bce.NewBodyFromSectionFile(file, offset, uploadSize)

		select {
		case err = <-errChan:
			return nil, err
		default:
			select {
			case err = <-errChan:
				return nil, err
			case parallelChan <- 1:
				go c.singlePartUpload(bucket, object, uploadId, int(i), partBody, parallelChan, errChan, resultChan)
			}

		}
	}

	partEtags := make([]api.UploadInfoType, partNum)
	for i := int64(0); i < partNum; i++ {
		select {
		case err := <-errChan:
			return nil, err
		case result := <-resultChan:
			partEtags[result.PartNumber-1].PartNumber = result.PartNumber
			partEtags[result.PartNumber-1].ETag = result.ETag
		}
	}
	return partEtags, nil
}

// singlePartUpload - single part upload
//
// PARAMS:
//     - pararelChan: the pararelChan
//     - errChan: the error chan
//     - result: the upload result chan
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the uploadId
//     - partNumber: the part number of the object
//     - content: the content of current part
func (c *Client) singlePartUpload(
	bucket string, object string, uploadId string,
	partNumber int, content *bce.Body,
	parallelChan chan int, errChan chan error, result chan api.UploadInfoType) {

	defer func() {
		if r := recover(); r != nil {
			log.Fatal("parallelPartUpload recovered in f:", r)
			errChan <- errors.New("parallelPartUpload panic")
		}
		<-parallelChan
	}()

	var args api.UploadPartArgs
	args.ContentMD5 = content.ContentMD5()

	etag, err := api.UploadPart(c, bucket, object, uploadId, partNumber, content, &args, c.BosContext)
	if err != nil {
		errChan <- err
		log.Error("upload part fail,err:%v", err)
		return
	}
	result <- api.UploadInfoType{PartNumber: partNumber, ETag: etag}
	return
}

// ParallelCopy - auto multipart copy object
//
// PARAMS:
//     - srcBucketName: the src bucket name
//     - srcObjectName: the src object name
//     - destBucketName: the dest bucket name
//     - destObjectName: the dest object name
//     - args: the copy args
//     - srcClient: the src region client
// RETURNS:
//     - *api.CompleteMultipartUploadResult: multipart upload result
//     - error: nil if success otherwise the specific error
func (c *Client) ParallelCopy(srcBucketName string, srcObjectName string,
	destBucketName string, destObjectName string,
	args *api.MultiCopyObjectArgs, srcClient *Client) (*api.CompleteMultipartUploadResult, error) {

	if srcClient == nil {
		srcClient = c
	}
	objectMeta, err := srcClient.GetObjectMeta(srcBucketName, srcObjectName)
	if err != nil {
		return nil, err
	}

	initArgs := api.InitiateMultipartUploadArgs{
		CacheControl:       objectMeta.CacheControl,
		ContentDisposition: objectMeta.ContentDisposition,
		Expires:            objectMeta.Expires,
		StorageClass:       objectMeta.StorageClass,
		ObjectTagging: args.ObjectTagging,
		TaggingDirective: args.TaggingDirective,
	}
	if args != nil {
		if len(args.StorageClass) != 0 {
			initArgs.StorageClass = args.StorageClass
		}
	}
	initiateMultipartUploadResult, err := api.InitiateMultipartUpload(c, destBucketName, destObjectName, objectMeta.ContentType, &initArgs, c.BosContext)

	if err != nil {
		return nil, err
	}

	source := fmt.Sprintf("/%s/%s", srcBucketName, srcObjectName)
	partEtags, err := c.parallelPartCopy(*objectMeta, source, destBucketName, destObjectName, initiateMultipartUploadResult.UploadId)

	if err != nil {
		c.AbortMultipartUpload(destBucketName, destObjectName, initiateMultipartUploadResult.UploadId)
		return nil, err
	}

	completeMultipartUploadResult, err := c.CompleteMultipartUploadFromStruct(destBucketName, destObjectName, initiateMultipartUploadResult.UploadId, &api.CompleteMultipartUploadArgs{Parts: partEtags})
	if err != nil {
		c.AbortMultipartUpload(destBucketName, destObjectName, initiateMultipartUploadResult.UploadId)
		return nil, err
	}
	return completeMultipartUploadResult, nil
}

// parallelPartCopy - parallel part copy
//
// PARAMS:
//     - srcMeta: the copy source object meta
//     - source: the copy source
//     - bucket: the dest bucket name
//     - object: the dest object name
//     - uploadId: the uploadId
// RETURNS:
//     - []api.UploadInfoType: multipart upload result
//     - error: nil if success otherwise the specific error
func (c *Client) parallelPartCopy(srcMeta api.GetObjectMetaResult, source string, bucket string, object string, uploadId string) ([]api.UploadInfoType, error) {
	var err error
	size := srcMeta.ContentLength
	partSize := int64(DEFAULT_MULTIPART_SIZE)
	if partSize*MAX_PART_NUMBER < size {
		lowerLimit := int64(math.Ceil(float64(size) / MAX_PART_NUMBER))
		partSize = int64(math.Ceil(float64(lowerLimit)/float64(partSize))) * partSize
	}
	partNum := (size + partSize - 1) / partSize

	parallelChan := make(chan int, c.MaxParallel)

	errChan := make(chan error, c.MaxParallel)

	resultChan := make(chan api.UploadInfoType, partNum)

	for i := int64(1); i <= partNum; i++ {
		// 计算偏移offset和本次上传的大小uploadSize
		uploadSize := partSize
		offset := partSize * (i - 1)
		left := size - offset
		if left < partSize {
			uploadSize = left
		}

		partCopyArgs := api.UploadPartCopyArgs{
			SourceRange: fmt.Sprintf("bytes=%d-%d", (i-1)*partSize, (i-1)*partSize+uploadSize-1),
			IfMatch:     srcMeta.ETag,
		}

		select {
		case err = <-errChan:
			return nil, err
		default:
			select {
			case err = <-errChan:
				return nil, err
			case parallelChan <- 1:
				go c.singlePartCopy(source, bucket, object, uploadId, int(i), &partCopyArgs, parallelChan, errChan, resultChan)
			}

		}
	}

	partEtags := make([]api.UploadInfoType, partNum)
	for i := int64(0); i < partNum; i++ {
		select {
		case err := <-errChan:
			return nil, err
		case result := <-resultChan:
			partEtags[result.PartNumber-1].PartNumber = result.PartNumber
			partEtags[result.PartNumber-1].ETag = result.ETag
		}
	}
	return partEtags, nil
}

// singlePartCopy - single part copy
//
// PARAMS:
//     - pararelChan: the pararelChan
//     - errChan: the error chan
//     - result: the upload result chan
//     - source: the copy source
//     - bucket: the bucket name
//     - object: the object name
//     - uploadId: the uploadId
//     - partNumber: the part number of the object
//     - args: the copy args
func (c *Client) singlePartCopy(source string, bucket string, object string, uploadId string,
	partNumber int, args *api.UploadPartCopyArgs,
	parallelChan chan int, errChan chan error, result chan api.UploadInfoType) {

	defer func() {
		if r := recover(); r != nil {
			log.Fatal("parallelPartUpload recovered in f:", r)
			errChan <- errors.New("parallelPartUpload panic")
		}
		<-parallelChan
	}()

	copyObjectResult, err := api.UploadPartCopy(c, bucket, object, source, uploadId, partNumber, args, c.BosContext)
	if err != nil {
		errChan <- err
		log.Error("upload part fail,err:%v", err)
		return
	}
	result <- api.UploadInfoType{PartNumber: partNumber, ETag: copyObjectResult.ETag}
	return
}

// PutSymlink - create symlink for exist target object
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the object
//     - symlinkKey: the name of the symlink
//     - symlinkArgs: the optional arguments
// RETURNS:
//     - error: the put error if any occurs
func (c *Client) PutSymlink(bucket string, object string, symlinkKey string, symlinkArgs *api.PutSymlinkArgs) error {
	return api.PutObjectSymlink(c, bucket, object, symlinkKey, symlinkArgs, c.BosContext)
}

// PutSymlink - create symlink for exist target object
//
// PARAMS:
//     - bucket: the name of the bucket
//     - object: the name of the symlink
// RETURNS:
//	   - string: the target of the symlink
//     - error: the put error if any occurs
func (c *Client) GetSymlink(bucket string, object string) (string, error) {
	return api.GetObjectSymlink(c, bucket, object, c.BosContext)
}

func (c *Client) PutBucketMirror(bucket string, putBucketMirrorArgs *api.PutBucketMirrorArgs) error {
	return api.PutBucketMirror(c, bucket, putBucketMirrorArgs, c.BosContext)
}

func (c *Client) GetBucketMirror(bucket string) (*api.PutBucketMirrorArgs, error) {
	return api.GetBucketMirror(c, bucket, c.BosContext)
}

func (c *Client) DeleteBucketMirror(bucket string) error {
	return api.DeleteBucketMirror(c, bucket, c.BosContext)
}

func (c *Client) PutBucketTag(bucket string, putBucketTagArgs *api.PutBucketTagArgs) error {
	return api.PutBucketTag(c, bucket, putBucketTagArgs, c.BosContext)
}

func (c *Client) GetBucketTag(bucket string) (*api.GetBucketTagResult, error) {
	return api.GetBucketTag(c, bucket, c.BosContext)
}

func (c *Client) DeleteBucketTag(bucket string) error {
	return api.DeleteBucketTag(c, bucket, c.BosContext)
}

func (c *Client) PutObjectTag(bucket string, object string, putObjectTagArgs *api.PutObjectTagArgs) error {
	return api.PutObjectTag(c, bucket, object, putObjectTagArgs, c.BosContext)
}

func (c *Client) GetObjectTag(bucket string, object string) (map[string]interface{}, error) {
	return api.GetObjectTag(c, bucket, object, c.BosContext)
}

func (c *Client) DeleteObjectTag(bucket string, object string) error {
	return api.DeleteObjectTag(c, bucket, object, c.BosContext)
}

func (c *Client) BosShareLinkGet(bucket string, prefix string, shareCode string, duration int) (string, error) {
	return api.GetBosShareLink(c, bucket, prefix, shareCode, duration)
}