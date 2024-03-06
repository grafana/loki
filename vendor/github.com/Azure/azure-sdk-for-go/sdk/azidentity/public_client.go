//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/log"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
)

type publicClientOptions struct {
	azcore.ClientOptions

	AdditionallyAllowedTenants []string
	DeviceCodePrompt           func(context.Context, DeviceCodeMessage) error
	DisableInstanceDiscovery   bool
	LoginHint, RedirectURL     string
	Username, Password         string
}

// publicClient wraps the MSAL public client
type publicClient struct {
	account                  public.Account
	cae, noCAE               msalPublicClient
	caeMu, noCAEMu, clientMu *sync.Mutex
	clientID, tenantID       string
	host                     string
	name                     string
	opts                     publicClientOptions
}

func newPublicClient(tenantID, clientID, name string, o publicClientOptions) (*publicClient, error) {
	if !validTenantID(tenantID) {
		return nil, errInvalidTenantID
	}
	host, err := setAuthorityHost(o.Cloud)
	if err != nil {
		return nil, err
	}
	o.AdditionallyAllowedTenants = resolveAdditionalTenants(o.AdditionallyAllowedTenants)
	return &publicClient{
		caeMu:    &sync.Mutex{},
		clientID: clientID,
		clientMu: &sync.Mutex{},
		host:     host,
		name:     name,
		noCAEMu:  &sync.Mutex{},
		opts:     o,
		tenantID: tenantID,
	}, nil
}

// GetToken requests an access token from MSAL, checking the cache first.
func (p *publicClient) GetToken(ctx context.Context, tro policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if len(tro.Scopes) < 1 {
		return azcore.AccessToken{}, fmt.Errorf("%s.GetToken() requires at least one scope", p.name)
	}
	tenant, err := p.resolveTenant(tro.TenantID)
	if err != nil {
		return azcore.AccessToken{}, err
	}
	client, mu, err := p.client(tro)
	if err != nil {
		return azcore.AccessToken{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	ar, err := client.AcquireTokenSilent(ctx, tro.Scopes, public.WithSilentAccount(p.account), public.WithClaims(tro.Claims), public.WithTenantID(tenant))
	if err == nil {
		return p.token(ar, err)
	}
	at, err := p.reqToken(ctx, client, tro)
	if err == nil {
		msg := fmt.Sprintf("%s.GetToken() acquired a token for scope %q", p.name, strings.Join(ar.GrantedScopes, ", "))
		log.Write(EventAuthentication, msg)
	}
	return at, err
}

// reqToken requests a token from the MSAL public client. It's separate from GetToken() to enable Authenticate() to bypass the cache.
func (p *publicClient) reqToken(ctx context.Context, c msalPublicClient, tro policy.TokenRequestOptions) (azcore.AccessToken, error) {
	tenant, err := p.resolveTenant(tro.TenantID)
	if err != nil {
		return azcore.AccessToken{}, err
	}
	var ar public.AuthResult
	switch p.name {
	case credNameBrowser:
		ar, err = c.AcquireTokenInteractive(ctx, tro.Scopes,
			public.WithClaims(tro.Claims),
			public.WithLoginHint(p.opts.LoginHint),
			public.WithRedirectURI(p.opts.RedirectURL),
			public.WithTenantID(tenant),
		)
	case credNameDeviceCode:
		dc, e := c.AcquireTokenByDeviceCode(ctx, tro.Scopes, public.WithClaims(tro.Claims), public.WithTenantID(tenant))
		if e != nil {
			return azcore.AccessToken{}, e
		}
		err = p.opts.DeviceCodePrompt(ctx, DeviceCodeMessage{
			Message:         dc.Result.Message,
			UserCode:        dc.Result.UserCode,
			VerificationURL: dc.Result.VerificationURL,
		})
		if err == nil {
			ar, err = dc.AuthenticationResult(ctx)
		}
	case credNameUserPassword:
		ar, err = c.AcquireTokenByUsernamePassword(ctx, tro.Scopes, p.opts.Username, p.opts.Password, public.WithClaims(tro.Claims), public.WithTenantID(tenant))
	default:
		return azcore.AccessToken{}, fmt.Errorf("unknown credential %q", p.name)
	}
	return p.token(ar, err)
}

func (p *publicClient) client(tro policy.TokenRequestOptions) (msalPublicClient, *sync.Mutex, error) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if tro.EnableCAE {
		if p.cae == nil {
			client, err := p.newMSALClient(true)
			if err != nil {
				return nil, nil, err
			}
			p.cae = client
		}
		return p.cae, p.caeMu, nil
	}
	if p.noCAE == nil {
		client, err := p.newMSALClient(false)
		if err != nil {
			return nil, nil, err
		}
		p.noCAE = client
	}
	return p.noCAE, p.noCAEMu, nil
}

func (p *publicClient) newMSALClient(enableCAE bool) (msalPublicClient, error) {
	o := []public.Option{
		public.WithAuthority(runtime.JoinPaths(p.host, p.tenantID)),
		public.WithHTTPClient(newPipelineAdapter(&p.opts.ClientOptions)),
	}
	if enableCAE {
		o = append(o, public.WithClientCapabilities(cp1))
	}
	if p.opts.DisableInstanceDiscovery || strings.ToLower(p.tenantID) == "adfs" {
		o = append(o, public.WithInstanceDiscovery(false))
	}
	return public.New(p.clientID, o...)
}

func (p *publicClient) token(ar public.AuthResult, err error) (azcore.AccessToken, error) {
	if err == nil {
		p.account = ar.Account
	} else {
		res := getResponseFromError(err)
		err = newAuthenticationFailedError(p.name, err.Error(), res, err)
	}
	return azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
}

// resolveTenant returns the correct tenant for a token request given the client's
// configuration, or an error when that configuration doesn't allow the specified tenant
func (p *publicClient) resolveTenant(specified string) (string, error) {
	return resolveTenant(p.tenantID, specified, p.name, p.opts.AdditionallyAllowedTenants)
}
