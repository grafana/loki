// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

/*
Package public provides a client for authentication of "public" applications. A "public"
application is defined as an app that runs on client devices (android, ios, windows, linux, ...).
These devices are "untrusted" and access resources via web APIs that must authenticate.
*/
package public

/*
Design note:

public.Client uses client.Base as an embedded type. client.Base statically assigns its attributes
during creation. As it doesn't have any pointers in it, anything borrowed from it, such as
Base.AuthParams is a copy that is free to be manipulated here.
*/

// TODO(msal): This should have example code for each method on client using Go's example doc framework.
// base usage details should be includee in the package documentation.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/base"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/local"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/accesstokens"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/authority"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/shared"
	"github.com/google/uuid"
	"github.com/pkg/browser"
)

// AuthResult contains the results of one token acquisition operation.
// For details see https://aka.ms/msal-net-authenticationresult
type AuthResult = base.AuthResult

type Account = shared.Account

// Options configures the Client's behavior.
type Options struct {
	// Accessor controls cache persistence. By default there is no cache persistence.
	// This can be set with the WithCache() option.
	Accessor cache.ExportReplace

	// The host of the Azure Active Directory authority. The default is https://login.microsoftonline.com/common.
	// This can be changed with the WithAuthority() option.
	Authority string

	// The HTTP client used for making requests.
	// It defaults to a shared http.Client.
	HTTPClient ops.HTTPClient
}

func (p *Options) validate() error {
	u, err := url.Parse(p.Authority)
	if err != nil {
		return fmt.Errorf("Authority options cannot be URL parsed: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("Authority(%s) did not start with https://", u.String())
	}
	return nil
}

// Option is an optional argument to the New constructor.
type Option func(o *Options)

// WithAuthority allows for a custom authority to be set. This must be a valid https url.
func WithAuthority(authority string) Option {
	return func(o *Options) {
		o.Authority = authority
	}
}

// WithCache allows you to set some type of cache for storing authentication tokens.
func WithCache(accessor cache.ExportReplace) Option {
	return func(o *Options) {
		o.Accessor = accessor
	}
}

// WithHTTPClient allows for a custom HTTP client to be set.
func WithHTTPClient(httpClient ops.HTTPClient) Option {
	return func(o *Options) {
		o.HTTPClient = httpClient
	}
}

// Client is a representation of authentication client for public applications as defined in the
// package doc. For more information, visit https://docs.microsoft.com/azure/active-directory/develop/msal-client-applications.
type Client struct {
	base base.Client
}

// New is the constructor for Client.
func New(clientID string, options ...Option) (Client, error) {
	opts := Options{
		Authority:  base.AuthorityPublicCloud,
		HTTPClient: shared.DefaultClient,
	}

	for _, o := range options {
		o(&opts)
	}
	if err := opts.validate(); err != nil {
		return Client{}, err
	}

	base, err := base.New(clientID, opts.Authority, oauth.New(opts.HTTPClient), base.WithCacheAccessor(opts.Accessor))
	if err != nil {
		return Client{}, err
	}
	return Client{base}, nil
}

// CreateAuthCodeURL creates a URL used to acquire an authorization code.
func (pca Client) CreateAuthCodeURL(ctx context.Context, clientID, redirectURI string, scopes []string) (string, error) {
	return pca.base.AuthCodeURL(ctx, clientID, redirectURI, scopes, pca.base.AuthParams)
}

// AcquireTokenSilentOptions are all the optional settings to an AcquireTokenSilent() call.
// These are set by using various AcquireTokenSilentOption functions.
type AcquireTokenSilentOptions struct {
	// Account represents the account to use. To set, use the WithSilentAccount() option.
	Account Account
}

// AcquireTokenSilentOption changes options inside AcquireTokenSilentOptions used in .AcquireTokenSilent().
type AcquireTokenSilentOption func(a *AcquireTokenSilentOptions)

// WithSilentAccount uses the passed account during an AcquireTokenSilent() call.
func WithSilentAccount(account Account) AcquireTokenSilentOption {
	return func(a *AcquireTokenSilentOptions) {
		a.Account = account
	}
}

// AcquireTokenSilent acquires a token from either the cache or using a refresh token.
func (pca Client) AcquireTokenSilent(ctx context.Context, scopes []string, options ...AcquireTokenSilentOption) (AuthResult, error) {
	opts := AcquireTokenSilentOptions{}
	for _, o := range options {
		o(&opts)
	}

	silentParameters := base.AcquireTokenSilentParameters{
		Scopes:      scopes,
		Account:     opts.Account,
		RequestType: accesstokens.ATPublic,
		IsAppCache:  false,
	}

	return pca.base.AcquireTokenSilent(ctx, silentParameters)
}

// AcquireTokenByUsernamePassword acquires a security token from the authority, via Username/Password Authentication.
// NOTE: this flow is NOT recommended.
func (pca Client) AcquireTokenByUsernamePassword(ctx context.Context, scopes []string, username string, password string) (AuthResult, error) {
	authParams := pca.base.AuthParams
	authParams.Scopes = scopes
	authParams.AuthorizationType = authority.ATUsernamePassword
	authParams.Username = username
	authParams.Password = password

	token, err := pca.base.Token.UsernamePassword(ctx, authParams)
	if err != nil {
		return AuthResult{}, err
	}
	return pca.base.AuthResultFromToken(ctx, authParams, token, true)
}

type DeviceCodeResult = accesstokens.DeviceCodeResult

// DeviceCode provides the results of the device code flows first stage (containing the code)
// that must be entered on the second device and provides a method to retrieve the AuthenticationResult
// once that code has been entered and verified.
type DeviceCode struct {
	// Result holds the information about the device code (such as the code).
	Result DeviceCodeResult

	authParams authority.AuthParams
	client     Client
	dc         oauth.DeviceCode
}

// AuthenticationResult retreives the AuthenticationResult once the user enters the code
// on the second device. Until then it blocks until the .AcquireTokenByDeviceCode() context
// is cancelled or the token expires.
func (d DeviceCode) AuthenticationResult(ctx context.Context) (AuthResult, error) {
	token, err := d.dc.Token(ctx)
	if err != nil {
		return AuthResult{}, err
	}
	return d.client.base.AuthResultFromToken(ctx, d.authParams, token, true)
}

// AcquireTokenByDeviceCode acquires a security token from the authority, by acquiring a device code and using that to acquire the token.
// Users need to create an AcquireTokenDeviceCodeParameters instance and pass it in.
func (pca Client) AcquireTokenByDeviceCode(ctx context.Context, scopes []string) (DeviceCode, error) {
	authParams := pca.base.AuthParams
	authParams.Scopes = scopes
	authParams.AuthorizationType = authority.ATDeviceCode

	dc, err := pca.base.Token.DeviceCode(ctx, authParams)
	if err != nil {
		return DeviceCode{}, err
	}

	return DeviceCode{Result: dc.Result, authParams: authParams, client: pca, dc: dc}, nil
}

// AcquireTokenByAuthCodeOptions contains the optional parameters used to acquire an access token using the authorization code flow.
type AcquireTokenByAuthCodeOptions struct {
	Challenge string
}

// AcquireTokenByAuthCodeOption changes options inside AcquireTokenByAuthCodeOptions used in .AcquireTokenByAuthCode().
type AcquireTokenByAuthCodeOption func(a *AcquireTokenByAuthCodeOptions)

// WithChallenge allows you to provide a code for the .AcquireTokenByAuthCode() call.
func WithChallenge(challenge string) AcquireTokenByAuthCodeOption {
	return func(a *AcquireTokenByAuthCodeOptions) {
		a.Challenge = challenge
	}
}

// AcquireTokenByAuthCode is a request to acquire a security token from the authority, using an authorization code.
// The specified redirect URI must be the same URI that was used when the authorization code was requested.
func (pca Client) AcquireTokenByAuthCode(ctx context.Context, code string, redirectURI string, scopes []string, options ...AcquireTokenByAuthCodeOption) (AuthResult, error) {
	opts := AcquireTokenByAuthCodeOptions{}
	for _, o := range options {
		o(&opts)
	}

	params := base.AcquireTokenAuthCodeParameters{
		Scopes:      scopes,
		Code:        code,
		Challenge:   opts.Challenge,
		AppType:     accesstokens.ATPublic,
		RedirectURI: redirectURI,
	}

	return pca.base.AcquireTokenByAuthCode(ctx, params)
}

// Accounts gets all the accounts in the token cache.
// If there are no accounts in the cache the returned slice is empty.
func (pca Client) Accounts() []Account {
	return pca.base.AllAccounts()
}

// RemoveAccount signs the account out and forgets account from token cache.
func (pca Client) RemoveAccount(account Account) error {
	pca.base.RemoveAccount(account)
	return nil
}

// InteractiveAuthOptions contains the optional parameters used to acquire an access token for interactive auth code flow.
type InteractiveAuthOptions struct {
	// Used to specify a custom port for the local server.  http://localhost:portnumber
	// All other URI components are ignored.
	RedirectURI string
}

// InteractiveAuthOption changes options inside InteractiveAuthOptions used in .AcquireTokenInteractive().
type InteractiveAuthOption func(*InteractiveAuthOptions)

// WithRedirectURI uses the specified redirect URI for interactive auth.
func WithRedirectURI(redirectURI string) InteractiveAuthOption {
	return func(o *InteractiveAuthOptions) {
		o.RedirectURI = redirectURI
	}
}

// AcquireTokenInteractive acquires a security token from the authority using the default web browser to select the account.
// https://docs.microsoft.com/en-us/azure/active-directory/develop/msal-authentication-flows#interactive-and-non-interactive-authentication
func (pca Client) AcquireTokenInteractive(ctx context.Context, scopes []string, options ...InteractiveAuthOption) (AuthResult, error) {
	opts := InteractiveAuthOptions{}
	for _, opt := range options {
		opt(&opts)
	}
	// the code verifier is a random 32-byte sequence that's been base-64 encoded without padding.
	// it's used to prevent MitM attacks during auth code flow, see https://tools.ietf.org/html/rfc7636
	cv, challenge, err := codeVerifier()
	if err != nil {
		return AuthResult{}, err
	}
	var redirectURL *url.URL
	if opts.RedirectURI != "" {
		redirectURL, err = url.Parse(opts.RedirectURI)
		if err != nil {
			return AuthResult{}, err
		}
	}
	authParams := pca.base.AuthParams // This is a copy, as we dont' have a pointer receiver and .AuthParams is not a pointer.
	authParams.Scopes = scopes
	authParams.AuthorizationType = authority.ATInteractive
	authParams.CodeChallenge = challenge
	authParams.CodeChallengeMethod = "S256"
	authParams.State = uuid.New().String()
	authParams.Prompt = "select_account"
	res, err := pca.browserLogin(ctx, redirectURL, authParams)
	if err != nil {
		return AuthResult{}, err
	}
	authParams.Redirecturi = res.redirectURI

	req, err := accesstokens.NewCodeChallengeRequest(authParams, accesstokens.ATPublic, nil, res.authCode, cv)
	if err != nil {
		return AuthResult{}, err
	}

	token, err := pca.base.Token.AuthCode(ctx, req)
	if err != nil {
		return AuthResult{}, err
	}

	return pca.base.AuthResultFromToken(ctx, authParams, token, true)
}

type interactiveAuthResult struct {
	authCode    string
	redirectURI string
}

// provides a test hook to simulate opening a browser
var browserOpenURL = func(authURL string) error {
	return browser.OpenURL(authURL)
}

// parses the port number from the provided URL.
// returns 0 if nil or no port is specified.
func parsePort(u *url.URL) (int, error) {
	if u == nil {
		return 0, nil
	}
	p := u.Port()
	if p == "" {
		return 0, nil
	}
	return strconv.Atoi(p)
}

// browserLogin launches the system browser for interactive login
func (pca Client) browserLogin(ctx context.Context, redirectURI *url.URL, params authority.AuthParams) (interactiveAuthResult, error) {
	// start local redirect server so login can call us back
	port, err := parsePort(redirectURI)
	if err != nil {
		return interactiveAuthResult{}, err
	}
	srv, err := local.New(params.State, port)
	if err != nil {
		return interactiveAuthResult{}, err
	}
	defer srv.Shutdown()
	params.Scopes = accesstokens.AppendDefaultScopes(params)
	authURL, err := pca.base.AuthCodeURL(ctx, params.ClientID, srv.Addr, params.Scopes, params)
	if err != nil {
		return interactiveAuthResult{}, err
	}
	// open browser window so user can select credentials
	if err := browserOpenURL(authURL); err != nil {
		return interactiveAuthResult{}, err
	}
	// now wait until the logic calls us back
	res := srv.Result(ctx)
	if res.Err != nil {
		return interactiveAuthResult{}, res.Err
	}
	return interactiveAuthResult{
		authCode:    res.Code,
		redirectURI: srv.Addr,
	}, nil
}

// creates a code verifier string along with its SHA256 hash which
// is used as the challenge when requesting an auth code.
// used in interactive auth flow for PKCE.
func codeVerifier() (codeVerifier string, challenge string, err error) {
	cvBytes := make([]byte, 32)
	if _, err = rand.Read(cvBytes); err != nil {
		return
	}
	codeVerifier = base64.RawURLEncoding.EncodeToString(cvBytes)
	// for PKCE, create a hash of the code verifier
	cvh := sha256.Sum256([]byte(codeVerifier))
	challenge = base64.RawURLEncoding.EncodeToString(cvh[:])
	return
}
