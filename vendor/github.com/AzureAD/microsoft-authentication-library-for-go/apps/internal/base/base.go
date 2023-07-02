// Package base contains a "Base" client that is used by the external public.Client and confidential.Client.
// Base holds shared attributes that must be available to both clients and methods that act as
// shared calls.
package base

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/base/internal/storage"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/accesstokens"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/authority"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/shared"
)

const (
	// AuthorityPublicCloud is the default AAD authority host
	AuthorityPublicCloud = "https://login.microsoftonline.com/common"
	scopeSeparator       = " "
)

// manager provides an internal cache. It is defined to allow faking the cache in tests.
// In all production use it is a *storage.Manager.
type manager interface {
	Read(ctx context.Context, authParameters authority.AuthParams, account shared.Account) (storage.TokenResponse, error)
	Write(authParameters authority.AuthParams, tokenResponse accesstokens.TokenResponse) (shared.Account, error)
	AllAccounts() []shared.Account
	Account(homeAccountID string) shared.Account
	RemoveAccount(account shared.Account, clientID string)
}

// partitionedManager provides an internal cache. It is defined to allow faking the cache in tests.
// In all production use it is a *storage.PartitionedManager.
type partitionedManager interface {
	Read(ctx context.Context, authParameters authority.AuthParams) (storage.TokenResponse, error)
	Write(authParameters authority.AuthParams, tokenResponse accesstokens.TokenResponse) (shared.Account, error)
}

type noopCacheAccessor struct{}

func (n noopCacheAccessor) Replace(cache cache.Unmarshaler, key string) {}
func (n noopCacheAccessor) Export(cache cache.Marshaler, key string)    {}

// AcquireTokenSilentParameters contains the parameters to acquire a token silently (from cache).
type AcquireTokenSilentParameters struct {
	Scopes            []string
	Account           shared.Account
	RequestType       accesstokens.AppType
	Credential        *accesstokens.Credential
	IsAppCache        bool
	UserAssertion     string
	AuthorizationType authority.AuthorizeType
}

// AcquireTokenAuthCodeParameters contains the parameters required to acquire an access token using the auth code flow.
// To use PKCE, set the CodeChallengeParameter.
// Code challenges are used to secure authorization code grants; for more information, visit
// https://tools.ietf.org/html/rfc7636.
type AcquireTokenAuthCodeParameters struct {
	Scopes      []string
	Code        string
	Challenge   string
	RedirectURI string
	AppType     accesstokens.AppType
	Credential  *accesstokens.Credential
}

type AcquireTokenOnBehalfOfParameters struct {
	Scopes        []string
	Credential    *accesstokens.Credential
	UserAssertion string
}

// AuthResult contains the results of one token acquisition operation in PublicClientApplication
// or ConfidentialClientApplication. For details see https://aka.ms/msal-net-authenticationresult
type AuthResult struct {
	Account        shared.Account
	IDToken        accesstokens.IDToken
	AccessToken    string
	ExpiresOn      time.Time
	GrantedScopes  []string
	DeclinedScopes []string
}

// AuthResultFromStorage creates an AuthResult from a storage token response (which is generated from the cache).
func AuthResultFromStorage(storageTokenResponse storage.TokenResponse) (AuthResult, error) {
	if err := storageTokenResponse.AccessToken.Validate(); err != nil {
		return AuthResult{}, fmt.Errorf("problem with access token in StorageTokenResponse: %w", err)
	}

	account := storageTokenResponse.Account
	accessToken := storageTokenResponse.AccessToken.Secret
	grantedScopes := strings.Split(storageTokenResponse.AccessToken.Scopes, scopeSeparator)

	// Checking if there was an ID token in the cache; this will throw an error in the case of confidential client applications.
	var idToken accesstokens.IDToken
	if !storageTokenResponse.IDToken.IsZero() {
		err := idToken.UnmarshalJSON([]byte(storageTokenResponse.IDToken.Secret))
		if err != nil {
			return AuthResult{}, fmt.Errorf("problem decoding JWT token: %w", err)
		}
	}
	return AuthResult{account, idToken, accessToken, storageTokenResponse.AccessToken.ExpiresOn.T, grantedScopes, nil}, nil
}

// NewAuthResult creates an AuthResult.
func NewAuthResult(tokenResponse accesstokens.TokenResponse, account shared.Account) (AuthResult, error) {
	if len(tokenResponse.DeclinedScopes) > 0 {
		return AuthResult{}, fmt.Errorf("token response failed because declined scopes are present: %s", strings.Join(tokenResponse.DeclinedScopes, ","))
	}
	return AuthResult{
		Account:       account,
		IDToken:       tokenResponse.IDToken,
		AccessToken:   tokenResponse.AccessToken,
		ExpiresOn:     tokenResponse.ExpiresOn.T,
		GrantedScopes: tokenResponse.GrantedScopes.Slice,
	}, nil
}

// Client is a base client that provides access to common methods and primatives that
// can be used by multiple clients.
type Client struct {
	Token    *oauth.Client
	manager  manager            // *storage.Manager or fakeManager in tests
	pmanager partitionedManager // *storage.PartitionedManager or fakeManager in tests

	AuthParams    authority.AuthParams // DO NOT EVER MAKE THIS A POINTER! See "Note" in New().
	cacheAccessor cache.ExportReplace
}

// Option is an optional argument to the New constructor.
type Option func(c *Client)

// WithCacheAccessor allows you to set some type of cache for storing authentication tokens.
func WithCacheAccessor(ca cache.ExportReplace) Option {
	return func(c *Client) {
		if ca != nil {
			c.cacheAccessor = ca
		}
	}
}

// WithKnownAuthorityHosts specifies hosts Client shouldn't validate or request metadata for because they're known to the user
func WithKnownAuthorityHosts(hosts []string) Option {
	return func(c *Client) {
		cp := make([]string, len(hosts))
		copy(cp, hosts)
		c.AuthParams.KnownAuthorityHosts = cp
	}
}

// WithX5C specifies if x5c claim(public key of the certificate) should be sent to STS to enable Subject Name Issuer Authentication.
func WithX5C(sendX5C bool) Option {
	return func(c *Client) {
		c.AuthParams.SendX5C = sendX5C
	}
}

func WithRegionDetection(region string) Option {
	return func(c *Client) {
		c.AuthParams.AuthorityInfo.Region = region
	}
}

// New is the constructor for Base.
func New(clientID string, authorityURI string, token *oauth.Client, options ...Option) (Client, error) {
	authInfo, err := authority.NewInfoFromAuthorityURI(authorityURI, true)
	if err != nil {
		return Client{}, err
	}
	authParams := authority.NewAuthParams(clientID, authInfo)
	client := Client{ // Note: Hey, don't even THINK about making Base into *Base. See "design notes" in public.go and confidential.go
		Token:         token,
		AuthParams:    authParams,
		cacheAccessor: noopCacheAccessor{},
		manager:       storage.New(token),
		pmanager:      storage.NewPartitionedManager(token),
	}
	for _, o := range options {
		o(&client)
	}
	return client, nil

}

// AuthCodeURL creates a URL used to acquire an authorization code.
func (b Client) AuthCodeURL(ctx context.Context, clientID, redirectURI string, scopes []string, authParams authority.AuthParams) (string, error) {
	endpoints, err := b.Token.ResolveEndpoints(ctx, authParams.AuthorityInfo, "")
	if err != nil {
		return "", err
	}

	baseURL, err := url.Parse(endpoints.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}

	v := url.Values{}
	v.Add("client_id", clientID)
	v.Add("response_type", "code")
	v.Add("redirect_uri", redirectURI)
	v.Add("scope", strings.Join(scopes, scopeSeparator))
	if authParams.State != "" {
		v.Add("state", authParams.State)
	}
	if authParams.CodeChallenge != "" {
		v.Add("code_challenge", authParams.CodeChallenge)
	}
	if authParams.CodeChallengeMethod != "" {
		v.Add("code_challenge_method", authParams.CodeChallengeMethod)
	}
	if authParams.Prompt != "" {
		v.Add("prompt", authParams.Prompt)
	}
	// There were left over from an implementation that didn't use any of these.  We may
	// need to add them later, but as of now aren't needed.
	/*
		if p.ResponseMode != "" {
			urlParams.Add("response_mode", p.ResponseMode)
		}
		if p.LoginHint != "" {
			urlParams.Add("login_hint", p.LoginHint)
		}
		if p.DomainHint != "" {
			urlParams.Add("domain_hint", p.DomainHint)
		}
	*/
	baseURL.RawQuery = v.Encode()
	return baseURL.String(), nil
}

func (b Client) AcquireTokenSilent(ctx context.Context, silent AcquireTokenSilentParameters) (AuthResult, error) {
	authParams := b.AuthParams // This is a copy, as we dont' have a pointer receiver and authParams is not a pointer.
	authParams.Scopes = silent.Scopes
	authParams.HomeAccountID = silent.Account.HomeAccountID
	authParams.AuthorizationType = silent.AuthorizationType
	authParams.UserAssertion = silent.UserAssertion

	var storageTokenResponse storage.TokenResponse
	var err error
	if authParams.AuthorizationType == authority.ATOnBehalfOf {
		if s, ok := b.pmanager.(cache.Serializer); ok {
			suggestedCacheKey := authParams.CacheKey(silent.IsAppCache)
			b.cacheAccessor.Replace(s, suggestedCacheKey)
			defer b.cacheAccessor.Export(s, suggestedCacheKey)
		}
		storageTokenResponse, err = b.pmanager.Read(ctx, authParams)
		if err != nil {
			return AuthResult{}, err
		}
	} else {
		if s, ok := b.manager.(cache.Serializer); ok {
			suggestedCacheKey := authParams.CacheKey(silent.IsAppCache)
			b.cacheAccessor.Replace(s, suggestedCacheKey)
			defer b.cacheAccessor.Export(s, suggestedCacheKey)
		}
		authParams.AuthorizationType = authority.ATRefreshToken
		storageTokenResponse, err = b.manager.Read(ctx, authParams, silent.Account)
		if err != nil {
			return AuthResult{}, err
		}
	}

	result, err := AuthResultFromStorage(storageTokenResponse)
	if err != nil {
		if reflect.ValueOf(storageTokenResponse.RefreshToken).IsZero() {
			return AuthResult{}, errors.New("no token found")
		}

		var cc *accesstokens.Credential
		if silent.RequestType == accesstokens.ATConfidential {
			cc = silent.Credential
		}

		token, err := b.Token.Refresh(ctx, silent.RequestType, authParams, cc, storageTokenResponse.RefreshToken)
		if err != nil {
			return AuthResult{}, err
		}

		return b.AuthResultFromToken(ctx, authParams, token, true)
	}
	return result, nil
}

func (b Client) AcquireTokenByAuthCode(ctx context.Context, authCodeParams AcquireTokenAuthCodeParameters) (AuthResult, error) {
	authParams := b.AuthParams // This is a copy, as we dont' have a pointer receiver and .AuthParams is not a pointer.
	authParams.Scopes = authCodeParams.Scopes
	authParams.Redirecturi = authCodeParams.RedirectURI
	authParams.AuthorizationType = authority.ATAuthCode

	var cc *accesstokens.Credential
	if authCodeParams.AppType == accesstokens.ATConfidential {
		cc = authCodeParams.Credential
		authParams.IsConfidentialClient = true
	}

	req, err := accesstokens.NewCodeChallengeRequest(authParams, authCodeParams.AppType, cc, authCodeParams.Code, authCodeParams.Challenge)
	if err != nil {
		return AuthResult{}, err
	}

	token, err := b.Token.AuthCode(ctx, req)
	if err != nil {
		return AuthResult{}, err
	}

	return b.AuthResultFromToken(ctx, authParams, token, true)
}

// AcquireTokenOnBehalfOf acquires a security token for an app using middle tier apps access token.
func (b Client) AcquireTokenOnBehalfOf(ctx context.Context, onBehalfOfParams AcquireTokenOnBehalfOfParameters) (AuthResult, error) {
	authParams := b.AuthParams // This is a copy, as we dont' have a pointer receiver and .AuthParams is not a pointer.
	authParams.Scopes = onBehalfOfParams.Scopes
	authParams.AuthorizationType = authority.ATOnBehalfOf
	authParams.UserAssertion = onBehalfOfParams.UserAssertion

	silentParameters := AcquireTokenSilentParameters{
		Scopes:            onBehalfOfParams.Scopes,
		RequestType:       accesstokens.ATConfidential,
		Credential:        onBehalfOfParams.Credential,
		UserAssertion:     onBehalfOfParams.UserAssertion,
		AuthorizationType: authority.ATOnBehalfOf,
	}
	token, err := b.AcquireTokenSilent(ctx, silentParameters)
	if err != nil {
		fmt.Println("Acquire Token Silent failed ")
		token, err := b.Token.OnBehalfOf(ctx, authParams, onBehalfOfParams.Credential)
		if err != nil {
			return AuthResult{}, err
		}
		return b.AuthResultFromToken(ctx, authParams, token, true)
	}
	return token, err
}

func (b Client) AuthResultFromToken(ctx context.Context, authParams authority.AuthParams, token accesstokens.TokenResponse, cacheWrite bool) (AuthResult, error) {
	if !cacheWrite {
		return NewAuthResult(token, shared.Account{})
	}

	var account shared.Account
	var err error
	if authParams.AuthorizationType == authority.ATOnBehalfOf {
		if s, ok := b.pmanager.(cache.Serializer); ok {
			suggestedCacheKey := token.CacheKey(authParams)
			b.cacheAccessor.Replace(s, suggestedCacheKey)
			defer b.cacheAccessor.Export(s, suggestedCacheKey)
		}
		account, err = b.pmanager.Write(authParams, token)
		if err != nil {
			return AuthResult{}, err
		}
	} else {
		if s, ok := b.manager.(cache.Serializer); ok {
			suggestedCacheKey := token.CacheKey(authParams)
			b.cacheAccessor.Replace(s, suggestedCacheKey)
			defer b.cacheAccessor.Export(s, suggestedCacheKey)
		}
		account, err = b.manager.Write(authParams, token)
		if err != nil {
			return AuthResult{}, err
		}
	}
	return NewAuthResult(token, account)
}

func (b Client) AllAccounts() []shared.Account {
	if s, ok := b.manager.(cache.Serializer); ok {
		suggestedCacheKey := b.AuthParams.CacheKey(false)
		b.cacheAccessor.Replace(s, suggestedCacheKey)
		defer b.cacheAccessor.Export(s, suggestedCacheKey)
	}

	accounts := b.manager.AllAccounts()
	return accounts
}

func (b Client) Account(homeAccountID string) shared.Account {
	authParams := b.AuthParams // This is a copy, as we dont' have a pointer receiver and .AuthParams is not a pointer.
	authParams.AuthorizationType = authority.AccountByID
	authParams.HomeAccountID = homeAccountID
	if s, ok := b.manager.(cache.Serializer); ok {
		suggestedCacheKey := b.AuthParams.CacheKey(false)
		b.cacheAccessor.Replace(s, suggestedCacheKey)
		defer b.cacheAccessor.Export(s, suggestedCacheKey)
	}
	account := b.manager.Account(homeAccountID)
	return account
}

// RemoveAccount removes all the ATs, RTs and IDTs from the cache associated with this account.
func (b Client) RemoveAccount(account shared.Account) {
	if s, ok := b.manager.(cache.Serializer); ok {
		suggestedCacheKey := b.AuthParams.CacheKey(false)
		b.cacheAccessor.Replace(s, suggestedCacheKey)
		defer b.cacheAccessor.Export(s, suggestedCacheKey)
	}
	b.manager.RemoveAccount(account, b.AuthParams.ClientID)
}
