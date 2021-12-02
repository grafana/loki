// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package azure

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	blob "github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

var errorCodeRegex = regexp.MustCompile(`X-Ms-Error-Code:\D*\[(\w+)\]`)

func init() {
	// Disable `ForceLog` in Azure storage module
	// As the time of this patch, the logging function in the storage module isn't correctly
	// detecting expected REST errors like 404 and so outputs them to syslog along with a stacktrace.
	// https://github.com/Azure/azure-storage-blob-go/issues/214
	//
	// This needs to be done at startup because the underlying variable is not thread safe.
	// https://github.com/Azure/azure-pipeline-go/blob/dc95902f1d32034f8f743ccc6c3f2eb36b84da27/pipeline/core.go#L276-L283
	pipeline.SetForceLogEnabled(false)
}

func getAzureStorageCredentials(logger log.Logger, conf Config) (blob.Credential, error) {
	if conf.MSIResource != "" || conf.UserAssignedID != "" {
		spt, err := getServicePrincipalToken(logger, conf)
		if err != nil {
			return nil, err
		}
		if err := spt.Refresh(); err != nil {
			return nil, err
		}

		return blob.NewTokenCredential(spt.Token().AccessToken, func(tc blob.TokenCredential) time.Duration {
			err := spt.Refresh()
			if err != nil {
				level.Error(logger).Log("msg", "could not refresh MSI token", "err", err)
				// Retry later as the error can be related to API throttling
				return 30 * time.Second
			}
			tc.SetToken(spt.Token().AccessToken)
			return spt.Token().Expires().Sub(time.Now().Add(2 * time.Minute))
		}), nil
	}

	credential, err := blob.NewSharedKeyCredential(conf.StorageAccountName, conf.StorageAccountKey)
	if err != nil {
		return nil, err
	}
	return credential, nil
}

func getServicePrincipalToken(logger log.Logger, conf Config) (*adal.ServicePrincipalToken, error) {
	resource := conf.MSIResource
	if resource == "" {
		resource = fmt.Sprintf("https://%s.%s", conf.StorageAccountName, conf.Endpoint)
	}

	msiConfig := auth.MSIConfig{
		Resource: resource,
	}

	if conf.UserAssignedID != "" {
		level.Debug(logger).Log("msg", "using user assigned identity", "clientId", conf.UserAssignedID)
		msiConfig.ClientID = conf.UserAssignedID
	} else {
		level.Debug(logger).Log("msg", "using system assigned identity")
	}

	return msiConfig.ServicePrincipalToken()
}

func getContainerURL(ctx context.Context, logger log.Logger, conf Config) (blob.ContainerURL, error) {
	credentials, err := getAzureStorageCredentials(logger, conf)

	if err != nil {
		return blob.ContainerURL{}, err
	}

	retryOptions := blob.RetryOptions{
		MaxTries:      conf.PipelineConfig.MaxTries,
		TryTimeout:    time.Duration(conf.PipelineConfig.TryTimeout),
		RetryDelay:    time.Duration(conf.PipelineConfig.RetryDelay),
		MaxRetryDelay: time.Duration(conf.PipelineConfig.MaxRetryDelay),
	}

	if deadline, ok := ctx.Deadline(); ok {
		retryOptions.TryTimeout = time.Until(deadline)
	}

	p := blob.NewPipeline(credentials, blob.PipelineOptions{
		Retry:     retryOptions,
		Telemetry: blob.TelemetryOptions{Value: "Thanos"},
		RequestLog: blob.RequestLogOptions{
			// Log a warning if an operation takes longer than the specified duration.
			// (-1=no logging; 0=default 3s threshold)
			LogWarningIfTryOverThreshold: -1,
		},
		Log: pipeline.LogOptions{
			ShouldLog: nil,
		},
		HTTPSender: pipeline.FactoryFunc(func(next pipeline.Policy, po *pipeline.PolicyOptions) pipeline.PolicyFunc {
			return func(ctx context.Context, request pipeline.Request) (pipeline.Response, error) {
				client := http.Client{
					Transport: DefaultTransport(conf),
				}

				resp, err := client.Do(request.WithContext(ctx))

				return pipeline.NewHTTPResponse(resp), err
			}
		}),
	})
	u, err := url.Parse(fmt.Sprintf("https://%s.%s", conf.StorageAccountName, conf.Endpoint))
	if err != nil {
		return blob.ContainerURL{}, err
	}
	service := blob.NewServiceURL(*u, p)

	return service.NewContainerURL(conf.ContainerName), nil
}

func DefaultTransport(config Config) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,

		MaxIdleConns:          config.HTTPConfig.MaxIdleConns,
		MaxIdleConnsPerHost:   config.HTTPConfig.MaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(config.HTTPConfig.IdleConnTimeout),
		MaxConnsPerHost:       config.HTTPConfig.MaxConnsPerHost,
		TLSHandshakeTimeout:   time.Duration(config.HTTPConfig.TLSHandshakeTimeout),
		ExpectContinueTimeout: time.Duration(config.HTTPConfig.ExpectContinueTimeout),

		ResponseHeaderTimeout: time.Duration(config.HTTPConfig.ResponseHeaderTimeout),
		DisableCompression:    config.HTTPConfig.DisableCompression,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: config.HTTPConfig.InsecureSkipVerify},
	}
}

func getContainer(ctx context.Context, logger log.Logger, conf Config) (blob.ContainerURL, error) {
	c, err := getContainerURL(ctx, logger, conf)
	if err != nil {
		return blob.ContainerURL{}, err
	}
	// Getting container properties to check if it exists or not. Returns error which will be parsed further.
	_, err = c.GetProperties(ctx, blob.LeaseAccessConditions{})
	return c, err
}

func createContainer(ctx context.Context, logger log.Logger, conf Config) (blob.ContainerURL, error) {
	c, err := getContainerURL(ctx, logger, conf)
	if err != nil {
		return blob.ContainerURL{}, err
	}
	_, err = c.Create(
		ctx,
		blob.Metadata{},
		blob.PublicAccessNone)
	return c, err
}

func getBlobURL(blobName string, c blob.ContainerURL) blob.BlockBlobURL {
	return c.NewBlockBlobURL(blobName)
}

func parseError(errorCode string) string {
	match := errorCodeRegex.FindStringSubmatch(errorCode)
	if len(match) == 2 {
		return match[1]
	}
	return errorCode
}
