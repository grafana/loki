// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"errors"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/internal/errorinfo"
)

// configurableClient is installed at the bottom of the managed identity pipeline as the transport
// for sources that require MSAL to configure the client/transport (Service Fabric today, mTLS later).
type configurableClient struct {
	client *http.Client
}

// Do implements policy.Transporter.
func (c *configurableClient) Do(req *http.Request) (*http.Response, error) {
	if c.client == nil {
		return nil, errorinfo.NonRetriableError(errors.New(credNameManagedIdentity + ": managed identity transport was not configured"))
	}
	return c.client.Do(req)
}
