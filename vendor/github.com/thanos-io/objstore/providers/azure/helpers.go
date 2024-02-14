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

func getContainerClient(conf Config) (*container.Client, error) {
	dt, err := exthttp.DefaultTransport(conf.HTTPConfig)
	if err != nil {
		return nil, err
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
			Transport: &http.Client{Transport: dt},
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
	var cred azcore.TokenCredential

	// Use Managed Identity Credential if a user assigned ID is set
	if conf.UserAssignedID != "" {
		msiOpt := &azidentity.ManagedIdentityCredentialOptions{}
		msiOpt.ID = azidentity.ClientID(conf.UserAssignedID)
		cred, err = azidentity.NewManagedIdentityCredential(msiOpt)
	} else {
		// Otherwise use Default Azure Credential
		cred, err = azidentity.NewDefaultAzureCredential(nil)
	}

	if err != nil {
		return nil, err
	}

	containerClient, err := container.NewClient(containerURL, cred, opt)
	if err != nil {
		return nil, err
	}

	return containerClient, nil
}
