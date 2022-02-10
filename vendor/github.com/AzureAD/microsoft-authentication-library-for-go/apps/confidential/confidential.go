// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

/*
Package confidential provides a client for authentication of "confidential" applications.
A "confidential" application is defined as an app that run on servers. They are considered
difficult to access and for that reason capable of keeping an application secret.
Confidential clients can hold configuration-time secrets.
*/
package confidential

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/base"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/accesstokens"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/authority"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/shared"
)

/*
Design note:

confidential.Client uses base.Client as an embedded type. base.Client statically assigns its attributes
during creation. As it doesn't have any pointers in it, anything borrowed from it, such as
Base.AuthParams is a copy that is free to be manipulated here.

Duplicate Calls shared between public.Client and this package:
There is some duplicate call options provided here that are the same as in public.Client . This
is a design choices. Go proverb(https://www.youtube.com/watch?v=PAAkCSZUG1c&t=9m28s):
"a little copying is better than a little dependency". Yes, we could have another package with
shared options (fail).  That divides like 2 options from all others which makes the user look
through more docs.  We can have all clients in one package, but I think separate packages
here makes for better naming (public.Client vs client.PublicClient).  So I chose a little
duplication.

.Net People, Take note on X509:
This uses x509.Certificates and private keys. x509 does not store private keys. .Net
has some x509.Certificate2 thing that has private keys, but that is just some bullcrap that .Net
added, it doesn't exist in real life.  Seriously, "x509.Certificate2", bahahahaha.  As such I've
put a PEM decoder into here.
*/

// TODO(msal): This should have example code for each method on client using Go's example doc framework.
// base usage details should be include in the package documentation.

// AuthResult contains the results of one token acquisition operation.
// For details see https://aka.ms/msal-net-authenticationresult
type AuthResult = base.AuthResult

type Account = shared.Account

// CertFromPEM converts a PEM file (.pem or .key) for use with NewCredFromCert(). The file
// must have the public certificate and the private key encoded. The private key must be encoded
// in PKCS8 (not PKCS1). This is usually denoted by the section "PRIVATE KEY" (instead of PKCS1's
// "RSA PRIVATE KEY"). If a PEM block is encoded and password is not an empty string, it attempts
// to decrypt the PEM blocks using the password. This will return multiple x509 certificates,
// though this use case should have a single cert. Multiple certs are due to certificate
// chaining for use cases like TLS that sign from root to leaf.
func CertFromPEM(pemData []byte, password string) ([]*x509.Certificate, crypto.PrivateKey, error) {
	var certs []*x509.Certificate
	var priv crypto.PrivateKey
	for {
		block, rest := pem.Decode(pemData)
		if block == nil {
			break
		}

		//nolint:staticcheck // x509.IsEncryptedPEMBlock and x509.DecryptPEMBlock are deprecated. They are used here only to support a usecase.
		if x509.IsEncryptedPEMBlock(block) {
			b, err := x509.DecryptPEMBlock(block, []byte(password))
			if err != nil {
				return nil, nil, fmt.Errorf("could not decrypt encrypted PEM block: %w", err)
			}
			block, _ = pem.Decode(b)
			if block == nil {
				return nil, nil, fmt.Errorf("encounter encrypted PEM block that did not decode")
			}
		}

		switch block.Type {
		case "CERTIFICATE":
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, nil, fmt.Errorf("block labelled 'CERTIFICATE' could not be pared by x509: %w", err)
			}
			certs = append(certs, cert)
		case "PRIVATE KEY":
			if priv != nil {
				return nil, nil, fmt.Errorf("found multiple blocks labelled 'PRIVATE KEY'")
			}

			var err error
			priv, err = parsePrivateKey(block.Bytes)
			if err != nil {
				return nil, nil, fmt.Errorf("could not decode private key: %w", err)
			}
		}
		pemData = rest
	}

	if len(certs) == 0 {
		return nil, nil, fmt.Errorf("no certificates found")
	}

	if priv == nil {
		return nil, nil, fmt.Errorf("no private key found")
	}

	return certs, priv, nil
}

// parsePrivateKey is based on https://gist.github.com/ukautz/cd118e298bbd8f0a88fc . I don't *think*
// we need to do the extra decoding in the example, but *maybe*?
func parsePrivateKey(der []byte) (crypto.PrivateKey, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("problems decoding private key using PKCS8: %w", err)
	}
	return key, nil
}

// Credential represents the credential used in confidential client flows.
type Credential struct {
	secret string

	cert *x509.Certificate
	key  crypto.PrivateKey

	assertion string
}

// toInternal returns the accesstokens.Credential that is used internally. The current structure of the
// code requires that client.go, requests.go and confidential.go share a credential type without
// having import recursion. That requires the type used between is in a shared package. Therefore
// we have this.
func (c Credential) toInternal() *accesstokens.Credential {
	return &accesstokens.Credential{Secret: c.secret, Cert: c.cert, Key: c.key, Assertion: c.assertion}
}

// NewCredFromSecret creates a Credential from a secret.
func NewCredFromSecret(secret string) (Credential, error) {
	if secret == "" {
		return Credential{}, errors.New("secret can't be empty string")
	}
	return Credential{secret: secret}, nil
}

// NewCredFromAssertion creates a Credential from a signed assertion.
func NewCredFromAssertion(assertion string) (Credential, error) {
	if assertion == "" {
		return Credential{}, errors.New("assertion can't be empty string")
	}
	return Credential{assertion: assertion}, nil
}

// NewCredFromCert creates a Credential from an x509.Certificate and a PKCS8 DER encoded private key.
// CertFromPEM() can be used to get these values from a PEM file storing a PKCS8 private key.
func NewCredFromCert(cert *x509.Certificate, key crypto.PrivateKey) Credential {
	return Credential{cert: cert, key: key}
}

// Client is a representation of authentication client for confidential applications as defined in the
// package doc. A new Client should be created PER SERVICE USER.
// For more information, visit https://docs.microsoft.com/azure/active-directory/develop/msal-client-applications
type Client struct {
	base base.Client

	cred *accesstokens.Credential

	// userID is some unique identifier for a user. It actually isn't used by us at all, it
	// simply acts as another hint that a confidential.Client is for a single user.
	userID string
}

// Options are optional settings for New(). These options are set using various functions
// returning Option calls.
type Options struct {
	// Accessor controls cache persistence.
	// By default there is no cache persistence. This can be set using the WithAccessor() option.
	Accessor cache.ExportReplace

	// The host of the Azure Active Directory authority.
	// The default is https://login.microsoftonline.com/common. This can be changed using the
	// WithAuthority() option.
	Authority string

	// The HTTP client used for making requests.
	// It defaults to a shared http.Client.
	HTTPClient ops.HTTPClient

	// SendX5C specifies if x5c claim(public key of the certificate) should be sent to STS.
	SendX5C bool
}

func (o Options) validate() error {
	u, err := url.Parse(o.Authority)
	if err != nil {
		return fmt.Errorf("the Authority(%s) does not parse as a valid URL", o.Authority)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("the Authority(%s) does not appear to use https", o.Authority)
	}
	return nil
}

// Option is an optional argument to New().
type Option func(o *Options)

// WithAuthority allows you to provide a custom authority for use in the client.
func WithAuthority(authority string) Option {
	return func(o *Options) {
		o.Authority = authority
	}
}

// WithAccessor provides a cache accessor that will read and write to some externally managed cache
// that may or may not be shared with other applications.
func WithAccessor(accessor cache.ExportReplace) Option {
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

// WithX5C specifies if x5c claim(public key of the certificate) should be sent to STS to enable Subject Name Issuer Authentication.
func WithX5C() Option {
	return func(o *Options) {
		o.SendX5C = true
	}
}

// New is the constructor for Client. userID is the unique identifier of the user this client
// will store credentials for (a Client is per user). clientID is the Azure clientID and cred is
// the type of credential to use.
func New(clientID string, cred Credential, options ...Option) (Client, error) {
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

	base, err := base.New(clientID, opts.Authority, oauth.New(opts.HTTPClient), base.WithX5C(opts.SendX5C), base.WithCacheAccessor(opts.Accessor))
	if err != nil {
		return Client{}, err
	}

	return Client{
		base: base,
		cred: cred.toInternal(),
	}, nil
}

// UserID is the unique user identifier this client if for.
func (cca Client) UserID() string {
	return cca.userID
}

// AuthCodeURL creates a URL used to acquire an authorization code. Users need to call CreateAuthorizationCodeURLParameters and pass it in.
func (cca Client) AuthCodeURL(ctx context.Context, clientID, redirectURI string, scopes []string) (string, error) {
	return cca.base.AuthCodeURL(ctx, clientID, redirectURI, scopes, cca.base.AuthParams)
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
func (cca Client) AcquireTokenSilent(ctx context.Context, scopes []string, options ...AcquireTokenSilentOption) (AuthResult, error) {
	opts := AcquireTokenSilentOptions{}
	for _, o := range options {
		o(&opts)
	}
	var isAppCache bool
	if opts.Account.IsZero() {
		isAppCache = true
	}

	silentParameters := base.AcquireTokenSilentParameters{
		Scopes:      scopes,
		Account:     opts.Account,
		RequestType: accesstokens.ATConfidential,
		Credential:  cca.cred,
		IsAppCache:  isAppCache,
	}

	return cca.base.AcquireTokenSilent(ctx, silentParameters)
}

// AcquireTokenByAuthCodeOptions contains the optional parameters used to acquire an access token using the authorization code flow.
type AcquireTokenByAuthCodeOptions struct {
	Challenge string
}

// AcquireTokenByAuthCodeOption changes options inside AcquireTokenByAuthCodeOptions used in .AcquireTokenByAuthCode().
type AcquireTokenByAuthCodeOption func(a *AcquireTokenByAuthCodeOptions)

// WithChallenge allows you to provide a challenge for the .AcquireTokenByAuthCode() call.
func WithChallenge(challenge string) AcquireTokenByAuthCodeOption {
	return func(a *AcquireTokenByAuthCodeOptions) {
		a.Challenge = challenge
	}
}

// AcquireTokenByAuthCode is a request to acquire a security token from the authority, using an authorization code.
// The specified redirect URI must be the same URI that was used when the authorization code was requested.
func (cca Client) AcquireTokenByAuthCode(ctx context.Context, code string, redirectURI string, scopes []string, options ...AcquireTokenByAuthCodeOption) (AuthResult, error) {
	opts := AcquireTokenByAuthCodeOptions{}
	for _, o := range options {
		o(&opts)
	}

	params := base.AcquireTokenAuthCodeParameters{
		Scopes:      scopes,
		Code:        code,
		Challenge:   opts.Challenge,
		AppType:     accesstokens.ATConfidential,
		Credential:  cca.cred, // This setting differs from public.Client.AcquireTokenByAuthCode
		RedirectURI: redirectURI,
	}

	return cca.base.AcquireTokenByAuthCode(ctx, params)
}

// AcquireTokenByCredential acquires a security token from the authority, using the client credentials grant.
func (cca Client) AcquireTokenByCredential(ctx context.Context, scopes []string) (AuthResult, error) {
	authParams := cca.base.AuthParams
	authParams.Scopes = scopes
	authParams.AuthorizationType = authority.ATClientCredentials

	token, err := cca.base.Token.Credential(ctx, authParams, cca.cred)
	if err != nil {
		return AuthResult{}, err
	}
	return cca.base.AuthResultFromToken(ctx, authParams, token, true)
}

// AcquireTokenOnBehalfOf acquires a security token for an app using middle tier apps access token.
// Refer https://docs.microsoft.com/en-us/azure/active-directory/develop/v2-oauth2-on-behalf-of-flow.
func (cca Client) AcquireTokenOnBehalfOf(ctx context.Context, userAssertion string, scopes []string) (AuthResult, error) {
	params := base.AcquireTokenOnBehalfOfParameters{
		Scopes:        scopes,
		UserAssertion: userAssertion,
		Credential:    cca.cred,
	}
	return cca.base.AcquireTokenOnBehalfOf(ctx, params)
}

// Account gets the account in the token cache with the specified homeAccountID.
func (cca Client) Account(homeAccountID string) Account {
	return cca.base.Account(homeAccountID)
}

// RemoveAccount signs the account out and forgets account from token cache.
func (cca Client) RemoveAccount(account Account) error {
	cca.base.RemoveAccount(account)
	return nil
}
