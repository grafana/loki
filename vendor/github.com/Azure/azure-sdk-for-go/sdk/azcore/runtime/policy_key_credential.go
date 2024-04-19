// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// KeyCredentialPolicy authorizes requests with a [azcore.KeyCredential].
type KeyCredentialPolicy struct {
	cred   *exported.KeyCredential
	header string
	prefix string
}

// KeyCredentialPolicyOptions contains the optional values configuring [KeyCredentialPolicy].
type KeyCredentialPolicyOptions struct {
	// Prefix is used if the key requires a prefix before it's inserted into the HTTP request.
	Prefix string
}

// NewKeyCredentialPolicy creates a new instance of [KeyCredentialPolicy].
//   - cred is the [azcore.KeyCredential] used to authenticate with the service
//   - header is the name of the HTTP request header in which the key is placed
//   - options contains optional configuration, pass nil to accept the default values
func NewKeyCredentialPolicy(cred *exported.KeyCredential, header string, options *KeyCredentialPolicyOptions) *KeyCredentialPolicy {
	if options == nil {
		options = &KeyCredentialPolicyOptions{}
	}
	return &KeyCredentialPolicy{
		cred:   cred,
		header: header,
		prefix: options.Prefix,
	}
}

// Do implementes the Do method on the [policy.Polilcy] interface.
func (k *KeyCredentialPolicy) Do(req *policy.Request) (*http.Response, error) {
	// skip adding the authorization header if no KeyCredential was provided.
	// this prevents a panic that might be hard to diagnose and allows testing
	// against http endpoints that don't require authentication.
	if k.cred != nil {
		if err := checkHTTPSForAuth(req); err != nil {
			return nil, err
		}
		val := exported.KeyCredentialGet(k.cred)
		if k.prefix != "" {
			val = k.prefix + val
		}
		req.Raw().Header.Add(k.header, val)
	}
	return req.Next()
}
