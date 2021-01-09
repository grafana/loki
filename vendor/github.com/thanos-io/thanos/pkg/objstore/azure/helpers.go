// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package azure

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	blob "github.com/Azure/azure-storage-blob-go/azblob"
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

func getContainerURL(ctx context.Context, conf Config) (blob.ContainerURL, error) {
	c, err := blob.NewSharedKeyCredential(conf.StorageAccountName, conf.StorageAccountKey)
	if err != nil {
		return blob.ContainerURL{}, err
	}

	retryOptions := blob.RetryOptions{
		MaxTries: int32(conf.MaxRetries),
	}
	if deadline, ok := ctx.Deadline(); ok {
		retryOptions.TryTimeout = time.Until(deadline)
	}

	p := blob.NewPipeline(c, blob.PipelineOptions{
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
	})
	u, err := url.Parse(fmt.Sprintf("https://%s.%s", conf.StorageAccountName, conf.Endpoint))
	if err != nil {
		return blob.ContainerURL{}, err
	}
	service := blob.NewServiceURL(*u, p)

	return service.NewContainerURL(conf.ContainerName), nil
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
