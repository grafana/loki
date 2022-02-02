//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pipeline"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// PipelineOptions contains Pipeline options for SDK developers
type PipelineOptions struct {
	AllowedHeaders, AllowedQueryParameters []string
	PerCall, PerRetry                      []policy.Policy
}

// NewPipeline creates a pipeline from connection options, with any additional policies as specified.
// module, version: used by the telemetry policy, when enabled
// perCall: additional policies to invoke once per request
// perRetry: additional policies to invoke once per request and once per retry of that request
func NewPipeline(module, version string, plOpts PipelineOptions, options *policy.ClientOptions) Pipeline {
	cp := policy.ClientOptions{}
	if options != nil {
		cp = *options
	}
	if len(plOpts.AllowedHeaders) > 0 {
		headers := make([]string, 0, len(plOpts.AllowedHeaders)+len(cp.Logging.AllowedHeaders))
		copy(headers, plOpts.AllowedHeaders)
		headers = append(headers, cp.Logging.AllowedHeaders...)
		cp.Logging.AllowedHeaders = headers
	}
	if len(plOpts.AllowedQueryParameters) > 0 {
		qp := make([]string, 0, len(plOpts.AllowedQueryParameters)+len(cp.Logging.AllowedQueryParams))
		copy(qp, plOpts.AllowedQueryParameters)
		qp = append(qp, cp.Logging.AllowedQueryParams...)
		cp.Logging.AllowedQueryParams = qp
	}
	policies := []policy.Policy{}
	if !cp.Telemetry.Disabled {
		policies = append(policies, NewTelemetryPolicy(module, version, &cp.Telemetry))
	}
	policies = append(policies, cp.PerCallPolicies...)
	policies = append(policies, plOpts.PerCall...)
	policies = append(policies, NewRetryPolicy(&cp.Retry))
	policies = append(policies, cp.PerRetryPolicies...)
	policies = append(policies, plOpts.PerRetry...)
	policies = append(policies, NewLogPolicy(&cp.Logging))
	policies = append(policies, pipeline.PolicyFunc(httpHeaderPolicy), pipeline.PolicyFunc(bodyDownloadPolicy))
	transport := cp.Transport
	if transport == nil {
		transport = defaultHTTPClient
	}
	return pipeline.NewPipeline(transport, policies...)
}
