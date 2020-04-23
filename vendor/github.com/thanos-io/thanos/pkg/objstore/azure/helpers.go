// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package azure

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"

	blob "github.com/Azure/azure-storage-blob-go/azblob"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

var errorCodeRegex = regexp.MustCompile(`X-Ms-Error-Code:\D*\[(\w+)\]`)

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
