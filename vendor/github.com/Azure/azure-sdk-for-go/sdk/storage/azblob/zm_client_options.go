// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

type ClientOptions struct {
	// Transporter sets the transport for making HTTP requests.
	Transporter policy.Transporter
	// Retry configures the built-in retry policy behavior.
	Retry policy.RetryOptions
	// Telemetry configures the built-in telemetry policy behavior.
	Telemetry policy.TelemetryOptions
	// PerCallOptions are options to run on every request
	PerCallOptions []policy.Policy
}

func (o *ClientOptions) getConnectionOptions() *policy.ClientOptions {
	if o == nil {
		return nil
	}

	return &policy.ClientOptions{
		Transport:       o.Transporter,
		Retry:           o.Retry,
		Telemetry:       o.Telemetry,
		PerCallPolicies: o.PerCallOptions,
	}
}
