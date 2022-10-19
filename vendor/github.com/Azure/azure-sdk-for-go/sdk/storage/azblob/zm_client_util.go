//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// ClientOptions adds additional client options while constructing connection
type ClientOptions struct {
	// Logging configures the built-in logging policy.
	Logging policy.LogOptions

	// Retry configures the built-in retry policy.
	Retry policy.RetryOptions

	// Telemetry configures the built-in telemetry policy.
	Telemetry policy.TelemetryOptions

	// Transport sets the transport for HTTP requests.
	Transport policy.Transporter

	// PerCallPolicies contains custom policies to inject into the pipeline.
	// Each policy is executed once per request.
	PerCallPolicies []policy.Policy

	// PerRetryPolicies contains custom policies to inject into the pipeline.
	// Each policy is executed once per request, and for each retry of that request.
	PerRetryPolicies []policy.Policy
}

func (c *ClientOptions) toPolicyOptions() *azcore.ClientOptions {
	return &azcore.ClientOptions{
		Logging:          c.Logging,
		Retry:            c.Retry,
		Telemetry:        c.Telemetry,
		Transport:        c.Transport,
		PerCallPolicies:  c.PerCallPolicies,
		PerRetryPolicies: c.PerRetryPolicies,
	}
}

// ---------------------------------------------------------------------------------------------------------------------

func getConnectionOptions(options *ClientOptions) *policy.ClientOptions {
	if options == nil {
		options = &ClientOptions{}
	}
	return options.toPolicyOptions()
}
