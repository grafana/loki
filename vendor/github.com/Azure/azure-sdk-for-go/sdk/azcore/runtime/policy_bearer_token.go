// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// BearerTokenPolicy authorizes requests with bearer tokens acquired from a TokenCredential.
type BearerTokenPolicy struct {
	// mainResource is the resource to be retreived using the tenant specified in the credential
	mainResource *shared.ExpiringResource
	// the following fields are read-only
	cred   azcore.TokenCredential
	scopes []string
}

type acquiringResourceState struct {
	req *policy.Request
	p   *BearerTokenPolicy
}

// acquire acquires or updates the resource; only one
// thread/goroutine at a time ever calls this function
func acquire(state interface{}) (newResource interface{}, newExpiration time.Time, err error) {
	s := state.(acquiringResourceState)
	tk, err := s.p.cred.GetToken(s.req.Raw().Context(), policy.TokenRequestOptions{Scopes: s.p.scopes})
	if err != nil {
		return nil, time.Time{}, err
	}
	return tk, tk.ExpiresOn, nil
}

// NewBearerTokenPolicy creates a policy object that authorizes requests with bearer tokens.
// cred: an azcore.TokenCredential implementation such as a credential object from azidentity
// scopes: the list of permission scopes required for the token.
// opts: optional settings. Pass nil to accept default values; this is the same as passing a zero-value options.
func NewBearerTokenPolicy(cred azcore.TokenCredential, scopes []string, opts *policy.BearerTokenOptions) *BearerTokenPolicy {
	return &BearerTokenPolicy{
		cred:         cred,
		scopes:       scopes,
		mainResource: shared.NewExpiringResource(acquire),
	}
}

// Do authorizes a request with a bearer token
func (b *BearerTokenPolicy) Do(req *policy.Request) (*http.Response, error) {
	as := acquiringResourceState{
		p:   b,
		req: req,
	}
	tk, err := b.mainResource.GetResource(as)
	if err != nil {
		return nil, err
	}
	if token, ok := tk.(*azcore.AccessToken); ok {
		req.Raw().Header.Set(shared.HeaderAuthorization, shared.BearerTokenPrefix+token.Token)
	}
	return req.Next()
}
