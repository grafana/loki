// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package azure

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/thanos-io/objstore/exthttp"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

func getContainerClient(conf Config, wrapRoundtripper func(http.RoundTripper) http.RoundTripper) (*container.Client, error) {
	var rt http.RoundTripper
	rt, err := exthttp.DefaultTransport(conf.HTTPConfig)
	if err != nil {
		return nil, err
	}
	if conf.HTTPConfig.Transport != nil {
		rt = conf.HTTPConfig.Transport
	}
	if wrapRoundtripper != nil {
		rt = wrapRoundtripper(rt)
	}
	opt := &container.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Retry: policy.RetryOptions{
				MaxRetries:    conf.PipelineConfig.MaxTries,
				TryTimeout:    time.Duration(conf.PipelineConfig.TryTimeout),
				RetryDelay:    time.Duration(conf.PipelineConfig.RetryDelay),
				MaxRetryDelay: time.Duration(conf.PipelineConfig.MaxRetryDelay),
			},
			Telemetry: policy.TelemetryOptions{
				ApplicationID: "Thanos",
			},
			Transport: &http.Client{Transport: rt},
		},
	}

	// Use connection string if set
	if conf.StorageConnectionString != "" {
		containerClient, err := container.NewClientFromConnectionString(conf.StorageConnectionString, conf.ContainerName, opt)
		if err != nil {
			return nil, err
		}
		return containerClient, nil
	}

	containerURL := fmt.Sprintf("https://%s.%s/%s", conf.StorageAccountName, conf.Endpoint, conf.ContainerName)

	// Use shared keys if set
	if conf.StorageAccountKey != "" {
		cred, err := container.NewSharedKeyCredential(conf.StorageAccountName, conf.StorageAccountKey)
		if err != nil {
			return nil, err
		}
		containerClient, err := container.NewClientWithSharedKeyCredential(containerURL, cred, opt)
		if err != nil {
			return nil, err
		}
		return containerClient, nil
	}

	// Otherwise use a token credential
	cred, err := getTokenCredential(conf)

	if err != nil {
		return nil, err
	}

	containerClient, err := container.NewClient(containerURL, cred, opt)
	if err != nil {
		return nil, err
	}

	return containerClient, nil
}

func getTokenCredential(conf Config) (azcore.TokenCredential, error) {
	if conf.ClientSecret != "" && conf.AzTenantID != "" && conf.ClientID != "" {
		return azidentity.NewClientSecretCredential(conf.AzTenantID, conf.ClientID, conf.ClientSecret, &azidentity.ClientSecretCredentialOptions{})
	}

	if conf.UserAssignedID == "" {
		return azidentity.NewDefaultAzureCredential(nil)
	}

	msiOpt := &azidentity.ManagedIdentityCredentialOptions{}
	msiOpt.ID = azidentity.ClientID(conf.UserAssignedID)
	return azidentity.NewManagedIdentityCredential(msiOpt)
}
