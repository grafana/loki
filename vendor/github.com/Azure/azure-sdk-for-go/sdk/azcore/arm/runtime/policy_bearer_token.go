// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	armpolicy "github.com/Azure/azure-sdk-for-go/sdk/azcore/arm/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
	azpolicy "github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	azruntime "github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/temporal"
)

const headerAuxiliaryAuthorization = "x-ms-authorization-auxiliary"

// acquiringResourceState holds data for an auxiliary token request
type acquiringResourceState struct {
	ctx    context.Context
	p      *BearerTokenPolicy
	tenant string
}

// acquireAuxToken acquires a token from an auxiliary tenant. Only one thread/goroutine at a time ever calls this function.
func acquireAuxToken(state acquiringResourceState) (newResource azcore.AccessToken, newExpiration time.Time, err error) {
	tk, err := state.p.cred.GetToken(state.ctx, azpolicy.TokenRequestOptions{
		EnableCAE: true,
		Scopes:    state.p.scopes,
		TenantID:  state.tenant,
	})
	if err != nil {
		return azcore.AccessToken{}, time.Time{}, err
	}
	return tk, tk.ExpiresOn, nil
}

// BearerTokenPolicy authorizes requests with bearer tokens acquired from a TokenCredential.
type BearerTokenPolicy struct {
	auxResources map[string]*temporal.Resource[azcore.AccessToken, acquiringResourceState]
	btp          *azruntime.BearerTokenPolicy
	cred         azcore.TokenCredential
	scopes       []string
}

// NewBearerTokenPolicy creates a policy object that authorizes requests with bearer tokens.
// cred: an azcore.TokenCredential implementation such as a credential object from azidentity
// opts: optional settings. Pass nil to accept default values; this is the same as passing a zero-value options.
func NewBearerTokenPolicy(cred azcore.TokenCredential, opts *armpolicy.BearerTokenOptions) *BearerTokenPolicy {
	if opts == nil {
		opts = &armpolicy.BearerTokenOptions{}
	}
	p := &BearerTokenPolicy{cred: cred}
	p.auxResources = make(map[string]*temporal.Resource[azcore.AccessToken, acquiringResourceState], len(opts.AuxiliaryTenants))
	for _, t := range opts.AuxiliaryTenants {
		p.auxResources[t] = temporal.NewResource(acquireAuxToken)
	}
	p.scopes = make([]string, len(opts.Scopes))
	copy(p.scopes, opts.Scopes)
	p.btp = azruntime.NewBearerTokenPolicy(cred, opts.Scopes, &azpolicy.BearerTokenOptions{
		AuthorizationHandler: azpolicy.AuthorizationHandler{
			OnChallenge: p.onChallenge,
			OnRequest:   p.onRequest,
		},
	})
	return p
}

func (b *BearerTokenPolicy) onChallenge(req *azpolicy.Request, res *http.Response, authNZ func(azpolicy.TokenRequestOptions) error) error {
	challenge := res.Header.Get(shared.HeaderWWWAuthenticate)
	claims, err := parseChallenge(challenge)
	if err != nil {
		// the challenge contains claims we can't parse
		return err
	} else if claims != "" {
		// request a new token having the specified claims, send the request again
		return authNZ(azpolicy.TokenRequestOptions{Claims: claims, EnableCAE: true, Scopes: b.scopes})
	}
	// auth challenge didn't include claims, so this is a simple authorization failure
	return azruntime.NewResponseError(res)
}

// onRequest authorizes requests with one or more bearer tokens
func (b *BearerTokenPolicy) onRequest(req *azpolicy.Request, authNZ func(azpolicy.TokenRequestOptions) error) error {
	// authorize the request with a token for the primary tenant
	err := authNZ(azpolicy.TokenRequestOptions{EnableCAE: true, Scopes: b.scopes})
	if err != nil || len(b.auxResources) == 0 {
		return err
	}
	// add tokens for auxiliary tenants
	as := acquiringResourceState{
		ctx: req.Raw().Context(),
		p:   b,
	}
	auxTokens := make([]string, 0, len(b.auxResources))
	for tenant, er := range b.auxResources {
		as.tenant = tenant
		auxTk, err := er.Get(as)
		if err != nil {
			return err
		}
		auxTokens = append(auxTokens, fmt.Sprintf("%s%s", shared.BearerTokenPrefix, auxTk.Token))
	}
	req.Raw().Header.Set(headerAuxiliaryAuthorization, strings.Join(auxTokens, ", "))
	return nil
}

// Do authorizes a request with a bearer token
func (b *BearerTokenPolicy) Do(req *azpolicy.Request) (*http.Response, error) {
	return b.btp.Do(req)
}

// parseChallenge parses claims from an authentication challenge issued by ARM so a client can request a token
// that will satisfy conditional access policies. It returns a non-nil error when the given value contains
// claims it can't parse. If the value contains no claims, it returns an empty string and a nil error.
func parseChallenge(wwwAuthenticate string) (string, error) {
	claims := ""
	var err error
	for _, param := range strings.Split(wwwAuthenticate, ",") {
		if _, after, found := strings.Cut(param, "claims="); found {
			if claims != "" {
				// The header contains multiple challenges, at least two of which specify claims. The specs allow this
				// but it's unclear what a client should do in this case and there's as yet no concrete example of it.
				err = fmt.Errorf("found multiple claims challenges in %q", wwwAuthenticate)
				break
			}
			// trim stuff that would get an error from RawURLEncoding; claims may or may not be padded
			claims = strings.Trim(after, `\"=`)
			// we don't return this error because it's something unhelpful like "illegal base64 data at input byte 42"
			if b, decErr := base64.RawURLEncoding.DecodeString(claims); decErr == nil {
				claims = string(b)
			} else {
				err = fmt.Errorf("failed to parse claims from %q", wwwAuthenticate)
				break
			}
		}
	}
	return claims, err
}
