// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// SASCredentialPolicy authorizes requests with a [azcore.SASCredential].
type SASCredentialPolicy struct {
	cred   *exported.SASCredential
	header string
}

// SASCredentialPolicyOptions contains the optional values configuring [SASCredentialPolicy].
type SASCredentialPolicyOptions struct {
	// placeholder for future optional values
}

// NewSASCredentialPolicy creates a new instance of [SASCredentialPolicy].
//   - cred is the [azcore.SASCredential] used to authenticate with the service
//   - header is the name of the HTTP request header in which the shared access signature is placed
//   - options contains optional configuration, pass nil to accept the default values
func NewSASCredentialPolicy(cred *exported.SASCredential, header string, options *SASCredentialPolicyOptions) *SASCredentialPolicy {
	return &SASCredentialPolicy{
		cred:   cred,
		header: header,
	}
}

// Do implementes the Do method on the [policy.Polilcy] interface.
func (k *SASCredentialPolicy) Do(req *policy.Request) (*http.Response, error) {
	// skip adding the authorization header if no SASCredential was provided.
	// this prevents a panic that might be hard to diagnose and allows testing
	// against http endpoints that don't require authentication.
	if k.cred != nil {
		if err := checkHTTPSForAuth(req); err != nil {
			return nil, err
		}
		req.Raw().Header.Add(k.header, exported.SASCredentialGet(k.cred))
	}
	return req.Next()
}
