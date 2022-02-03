//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
)

// NewResponseError creates an *azcore.ResponseError from the provided HTTP response.
// Call this when a service request returns a non-successful status code.
func NewResponseError(resp *http.Response) error {
	return shared.NewResponseError(resp)
}
