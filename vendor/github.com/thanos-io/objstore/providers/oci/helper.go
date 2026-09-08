// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package oci

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
)

func CustomTransport(config Config) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		IdleConnTimeout:       time.Duration(config.HTTPConfig.IdleConnTimeout),
		ResponseHeaderTimeout: time.Duration(config.HTTPConfig.ResponseHeaderTimeout),
		TLSHandshakeTimeout:   time.Duration(config.HTTPConfig.TLSHandshakeTimeout),
		ExpectContinueTimeout: time.Duration(config.HTTPConfig.ExpectContinueTimeout),
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: config.HTTPConfig.InsecureSkipVerify},
		MaxIdleConns:          config.HTTPConfig.MaxIdleConns,
		MaxIdleConnsPerHost:   config.HTTPConfig.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.HTTPConfig.MaxConnsPerHost,
		DisableCompression:    config.HTTPConfig.DisableCompression,
	}
}

func getNamespace(client objectstorage.ObjectStorageClient, requestMetadata common.RequestMetadata) (namespace *string, err error) {
	response, err := client.GetNamespace(
		context.Background(),
		objectstorage.GetNamespaceRequest{RequestMetadata: requestMetadata},
	)
	if err != nil {
		return nil, err
	}
	return response.Value, nil
}

func getObject(ctx context.Context, bkt Bucket, objectName string, byteRange string) (response objectstorage.GetObjectResponse, err error) {
	if len(objectName) == 0 {
		err = fmt.Errorf("value cannot be empty for field ObjectName in path")
		return
	}
	request := objectstorage.GetObjectRequest{
		NamespaceName:   &bkt.namespace,
		BucketName:      &bkt.name,
		ObjectName:      &objectName,
		RequestMetadata: bkt.requestMetadata,
	}
	if byteRange != "" {
		request.Range = &byteRange
	}
	return bkt.client.GetObject(ctx, request)
}

func listAllObjects(ctx context.Context, bkt Bucket, prefix string, options ...objstore.IterOption) (objectNames []string, err error) {
	var allObjectNames []string
	var nextStartWith *string = nil
	init := true

	for init || nextStartWith != nil {
		init = false
		objectNames, nextStartWith, err = listObjects(ctx, bkt, prefix, nextStartWith)
		if err != nil {
			return nil, err
		}

		if objstore.ApplyIterOptions(options...).Recursive {
			for _, objectName := range objectNames {
				if strings.HasSuffix(objectName, DirDelim) {
					subObjectNames, err := listAllObjects(ctx, bkt, objectName, options...)
					if err != nil {
						return nil, err
					}
					allObjectNames = append(allObjectNames, subObjectNames...)
				} else {
					allObjectNames = append(allObjectNames, objectName)
				}
			}
		} else {
			allObjectNames = append(allObjectNames, objectNames...)
		}
	}
	return allObjectNames, nil
}

func listObjects(ctx context.Context, bkt Bucket, prefix string, start *string) (objectNames []string, nextStartWith *string, err error) {
	request := objectstorage.ListObjectsRequest{
		NamespaceName:   &bkt.namespace,
		BucketName:      &bkt.name,
		Delimiter:       common.String(DirDelim),
		Prefix:          &prefix,
		Start:           start,
		RequestMetadata: bkt.requestMetadata,
	}
	response, err := bkt.client.ListObjects(ctx, request)
	if err != nil {
		return nil, nil, err
	}

	for _, object := range response.ListObjects.Objects {
		objectNames = append(objectNames, *object.Name)
	}
	objectNames = append(objectNames, response.ListObjects.Prefixes...)

	return objectNames, response.NextStartWith, nil
}

func (config *Config) validateConfig() (err error) {
	var errMsg []string

	if config.Tenancy == "" {
		errMsg = append(errMsg, "no OCI tenancy ocid specified")
	}
	if config.User == "" {
		errMsg = append(errMsg, "no OCI user ocid specified")
	}
	if config.Region == "" {
		errMsg = append(errMsg, "no OCI region specified")
	}
	if config.Fingerprint == "" {
		errMsg = append(errMsg, "no OCI fingerprint specified")
	}
	if config.PrivateKey == "" {
		errMsg = append(errMsg, "no OCI privatekey specified")
	}

	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, ", "))
	}

	return
}

func getRequestMetadata(maxRequestRetries int, requestRetryInterval int) common.RequestMetadata {
	if maxRequestRetries <= 1 {
		retryPolicy := common.NoRetryPolicy()
		return common.RequestMetadata{
			RetryPolicy: &retryPolicy,
		}
	}
	retryPolicy := common.NewRetryPolicyWithOptions(common.WithMaximumNumberAttempts(uint(maxRequestRetries)),
		common.WithFixedBackoff(time.Duration(requestRetryInterval)*time.Second))
	return common.RequestMetadata{
		RetryPolicy: &retryPolicy,
	}
}

func getConfigFromEnv() (config Config, err error) {
	config = Config{
		Provider:    strings.ToLower(os.Getenv("OCI_PROVIDER")),
		Bucket:      os.Getenv("OCI_BUCKET"),
		Compartment: os.Getenv("OCI_COMPARTMENT"),
		Tenancy:     os.Getenv("OCI_TENANCY_OCID"),
		User:        os.Getenv("OCI_USER_OCID"),
		Region:      os.Getenv("OCI_REGION"),
		Fingerprint: os.Getenv("OCI_FINGERPRINT"),
		PrivateKey:  os.Getenv("OCI_PRIVATEKEY"),
		Passphrase:  os.Getenv("OCI_PASSPHRASE"),
	}

	// [Optional] Override the default part size of 128 MiB, value is in bytes. The max part size is 50GiB
	if os.Getenv("OCI_PART_SIZE") != "" {
		partSize, err := strconv.ParseInt(os.Getenv("OCI_PART_SIZE"), 10, 64)
		if err != nil {
			return Config{}, err
		}
		config.PartSize = partSize
	}

	if os.Getenv("OCI_MAX_REQUEST_RETRIES") != "" {
		maxRequestRetries, err := strconv.Atoi(os.Getenv("OCI_MAX_REQUEST_RETRIES"))
		if err != nil {
			return Config{}, err
		}
		config.MaxRequestRetries = maxRequestRetries
	}

	if os.Getenv("OCI_REQUEST_RETRY_INTERVAL") != "" {
		requestRetryInterval, err := strconv.Atoi(os.Getenv("OCI_REQUEST_RETRY_INTERVAL"))
		if err != nil {
			return Config{}, err
		}
		config.RequestRetryInterval = requestRetryInterval
	}

	if os.Getenv("HTTP_CONFIG_IDLE_CONN_TIMEOUT") != "" {
		idleConnTimeout, err := model.ParseDuration(os.Getenv("HTTP_CONFIG_IDLE_CONN_TIMEOUT"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.IdleConnTimeout = idleConnTimeout
	}

	if os.Getenv("HTTP_CONFIG_RESPONSE_HEADER_TIMEOUT") != "" {
		responseHeaderTimeout, err := model.ParseDuration(os.Getenv("HTTP_CONFIG_RESPONSE_HEADER_TIMEOUT"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.ResponseHeaderTimeout = responseHeaderTimeout
	}

	if os.Getenv("HTTP_CONFIG_TLS_HANDSHAKE_TIMEOUT") != "" {
		tlsHandshakeTimeout, err := model.ParseDuration(os.Getenv("HTTP_CONFIG_TLS_HANDSHAKE_TIMEOUT"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.TLSHandshakeTimeout = tlsHandshakeTimeout
	}

	if os.Getenv("HTTP_CONFIG_EXPECT_CONTINUE_TIMEOUT") != "" {
		expectContinueTimeout, err := model.ParseDuration(os.Getenv("HTTP_CONFIG_EXPECT_CONTINUE_TIMEOUT"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.ExpectContinueTimeout = expectContinueTimeout
	}

	if os.Getenv("HTTP_CONFIG_INSECURE_SKIP_VERIFY") != "" {
		insecureSkipVerify, err := strconv.ParseBool(os.Getenv("HTTP_CONFIG_INSECURE_SKIP_VERIFY"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.InsecureSkipVerify = insecureSkipVerify
	}

	if os.Getenv("HTTP_CONFIG_MAX_IDLE_CONNS") != "" {
		maxIdleConns, err := strconv.Atoi(os.Getenv("HTTP_CONFIG_MAX_IDLE_CONNS"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.MaxIdleConns = maxIdleConns
	}

	if os.Getenv("HTTP_CONFIG_MAX_IDLE_CONNS_PER_HOST") != "" {
		maxIdleConnsPerHost, err := strconv.Atoi(os.Getenv("HTTP_CONFIG_MAX_IDLE_CONNS_PER_HOST"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.MaxIdleConnsPerHost = maxIdleConnsPerHost
	}

	if os.Getenv("HTTP_CONFIG_MAX_CONNS_PER_HOST") != "" {
		maxConnsPerHost, err := strconv.Atoi(os.Getenv("HTTP_CONFIG_MAX_CONNS_PER_HOST"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.MaxConnsPerHost = maxConnsPerHost
	}

	if os.Getenv("HTTP_CONFIG_DISABLE_COMPRESSION") != "" {
		disableCompression, err := strconv.ParseBool(os.Getenv("HTTP_CONFIG_DISABLE_COMPRESSION"))
		if err != nil {
			return Config{}, err
		}
		config.HTTPConfig.DisableCompression = disableCompression
	}

	return config, nil
}
