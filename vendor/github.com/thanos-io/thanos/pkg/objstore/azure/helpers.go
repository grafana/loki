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
	"github.com/Azure/go-autorest/autorest/azure/auth"
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

func getAzureStorageCredentials(conf Config) (blob.Credential, error) {
	if conf.MSIResource != "" {
		msiConfig := auth.NewMSIConfig()
		msiConfig.Resource = conf.MSIResource

		azureServicePrincipalToken, err := msiConfig.ServicePrincipalToken()
		if err != nil {
			return nil, err
		}

		// Get a new token.
		err = azureServicePrincipalToken.Refresh()
		if err != nil {
			return nil, err
		}
		token := azureServicePrincipalToken.Token()

		return blob.NewTokenCredential(token.AccessToken, nil), nil
	}

	credential, err := blob.NewSharedKeyCredential(conf.StorageAccountName, conf.StorageAccountKey)
	if err != nil {
		return nil, err
	}
	return credential, nil
}

func getContainerURL(ctx context.Context, conf Config) (blob.ContainerURL, error) {

	credentials, err := getAzureStorageCredentials(conf)

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

func getContainer(ctx context.Context, conf Config) (blob.ContainerURL, error) {
	c, err := getContainerURL(ctx, conf)
	if err != nil {
		return blob.ContainerURL{}, err
	}
	// Getting container properties to check if it exists or not. Returns error which will be parsed further.
	_, err = c.GetProperties(ctx, blob.LeaseAccessConditions{})
	return c, err
}

func createContainer(ctx context.Context, conf Config) (blob.ContainerURL, error) {
	c, err := getContainerURL(ctx, conf)
	if err != nil {
		return blob.ContainerURL{}, err
	}
	_, err = c.Create(
		ctx,
		blob.Metadata{},
		blob.PublicAccessNone)
	return c, err
}

func getBlobURL(ctx context.Context, conf Config, blobName string) (blob.BlockBlobURL, error) {
	c, err := getContainerURL(ctx, conf)
	if err != nil {
		return blob.BlockBlobURL{}, err
	}
	return c.NewBlockBlobURL(blobName), nil
}

func parseError(errorCode string) string {
	match := errorCodeRegex.FindStringSubmatch(errorCode)
	if len(match) == 2 {
		return match[1]
	}
	return errorCode
}
