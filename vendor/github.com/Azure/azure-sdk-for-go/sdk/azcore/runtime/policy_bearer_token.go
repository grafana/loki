// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/errorinfo"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/temporal"
)

// BearerTokenPolicy authorizes requests with bearer tokens acquired from a TokenCredential.
type BearerTokenPolicy struct {
	// mainResource is the resource to be retreived using the tenant specified in the credential
	mainResource *temporal.Resource[exported.AccessToken, acquiringResourceState]
	// the following fields are read-only
	authzHandler policy.AuthorizationHandler
	cred         exported.TokenCredential
	scopes       []string
	allowHTTP    bool
}

type acquiringResourceState struct {
	req *policy.Request
	p   *BearerTokenPolicy
	tro policy.TokenRequestOptions
}

// acquire acquires or updates the resource; only one
// thread/goroutine at a time ever calls this function
func acquire(state acquiringResourceState) (newResource exported.AccessToken, newExpiration time.Time, err error) {
	tk, err := state.p.cred.GetToken(&shared.ContextWithDeniedValues{Context: state.req.Raw().Context()}, state.tro)
	if err != nil {
		return exported.AccessToken{}, time.Time{}, err
	}
	return tk, tk.ExpiresOn, nil
}

// NewBearerTokenPolicy creates a policy object that authorizes requests with bearer tokens.
// cred: an azcore.TokenCredential implementation such as a credential object from azidentity
// scopes: the list of permission scopes required for the token.
// opts: optional settings. Pass nil to accept default values; this is the same as passing a zero-value options.
func NewBearerTokenPolicy(cred exported.TokenCredential, scopes []string, opts *policy.BearerTokenOptions) *BearerTokenPolicy {
	if opts == nil {
		opts = &policy.BearerTokenOptions{}
	}
	return &BearerTokenPolicy{
		authzHandler: opts.AuthorizationHandler,
		cred:         cred,
		scopes:       scopes,
		mainResource: temporal.NewResource(acquire),
		allowHTTP:    opts.InsecureAllowCredentialWithHTTP,
	}
}

// authenticateAndAuthorize returns a function which authorizes req with a token from the policy's credential
func (b *BearerTokenPolicy) authenticateAndAuthorize(req *policy.Request) func(policy.TokenRequestOptions) error {
	return func(tro policy.TokenRequestOptions) error {
		as := acquiringResourceState{p: b, req: req, tro: tro}
		tk, err := b.mainResource.Get(as)
		if err != nil {
			return err
		}
		req.Raw().Header.Set(shared.HeaderAuthorization, shared.BearerTokenPrefix+tk.Token)
		return nil
	}
}

// Do authorizes a request with a bearer token
func (b *BearerTokenPolicy) Do(req *policy.Request) (*http.Response, error) {
	// skip adding the authorization header if no TokenCredential was provided.
	// this prevents a panic that might be hard to diagnose and allows testing
	// against http endpoints that don't require authentication.
	if b.cred == nil {
		return req.Next()
	}

	if err := checkHTTPSForAuth(req, b.allowHTTP); err != nil {
		return nil, err
	}

	var err error
	if b.authzHandler.OnRequest != nil {
		err = b.authzHandler.OnRequest(req, b.authenticateAndAuthorize(req))
	} else {
		err = b.authenticateAndAuthorize(req)(policy.TokenRequestOptions{Scopes: b.scopes})
	}
	if err != nil {
		return nil, errorinfo.NonRetriableError(err)
	}

	res, err := req.Next()
	if err != nil {
		return nil, err
	}

	if res.StatusCode == http.StatusUnauthorized {
		b.mainResource.Expire()
		if res.Header.Get("WWW-Authenticate") != "" && b.authzHandler.OnChallenge != nil {
			if err = b.authzHandler.OnChallenge(req, res, b.authenticateAndAuthorize(req)); err == nil {
				res, err = req.Next()
			}
		}
	}
	if err != nil {
		err = errorinfo.NonRetriableError(err)
	}
	return res, err
}

func checkHTTPSForAuth(req *policy.Request, allowHTTP bool) error {
	if strings.ToLower(req.Raw().URL.Scheme) != "https" && !allowHTTP {
		return errorinfo.NonRetriableError(errors.New("authenticated requests are not permitted for non TLS protected (https) endpoints"))
	}
	return nil
}
