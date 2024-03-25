// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	conntrack "github.com/mwitkow/go-conntrack"
	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/http2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"gopkg.in/yaml.v2"
)

var (
	// DefaultHTTPClientConfig is the default HTTP client configuration.
	DefaultHTTPClientConfig = HTTPClientConfig{
		FollowRedirects: true,
		EnableHTTP2:     true,
	}

	// defaultHTTPClientOptions holds the default HTTP client options.
	defaultHTTPClientOptions = httpClientOptions{
		keepAlivesEnabled: true,
		http2Enabled:      true,
		// 5 minutes is typically above the maximum sane scrape interval. So we can
		// use keepalive for all configurations.
		idleConnTimeout: 5 * time.Minute,
	}
)

type closeIdler interface {
	CloseIdleConnections()
}

type TLSVersion uint16

var TLSVersions = map[string]TLSVersion{
	"TLS13": (TLSVersion)(tls.VersionTLS13),
	"TLS12": (TLSVersion)(tls.VersionTLS12),
	"TLS11": (TLSVersion)(tls.VersionTLS11),
	"TLS10": (TLSVersion)(tls.VersionTLS10),
}

func (tv *TLSVersion) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	err := unmarshal((*string)(&s))
	if err != nil {
		return err
	}
	if v, ok := TLSVersions[s]; ok {
		*tv = v
		return nil
	}
	return fmt.Errorf("unknown TLS version: %s", s)
}

func (tv TLSVersion) MarshalYAML() (interface{}, error) {
	for s, v := range TLSVersions {
		if tv == v {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unknown TLS version: %d", tv)
}

// MarshalJSON implements the json.Unmarshaler interface for TLSVersion.
func (tv *TLSVersion) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if v, ok := TLSVersions[s]; ok {
		*tv = v
		return nil
	}
	return fmt.Errorf("unknown TLS version: %s", s)
}

// MarshalJSON implements the json.Marshaler interface for TLSVersion.
func (tv TLSVersion) MarshalJSON() ([]byte, error) {
	for s, v := range TLSVersions {
		if tv == v {
			return json.Marshal(s)
		}
	}
	return nil, fmt.Errorf("unknown TLS version: %d", tv)
}

// String implements the fmt.Stringer interface for TLSVersion.
func (tv *TLSVersion) String() string {
	if tv == nil || *tv == 0 {
		return ""
	}
	for s, v := range TLSVersions {
		if *tv == v {
			return s
		}
	}
	return fmt.Sprintf("%d", tv)
}

// BasicAuth contains basic HTTP authentication credentials.
type BasicAuth struct {
	Username     string `yaml:"username" json:"username"`
	UsernameFile string `yaml:"username_file,omitempty" json:"username_file,omitempty"`
	Password     Secret `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFile string `yaml:"password_file,omitempty" json:"password_file,omitempty"`
}

// SetDirectory joins any relative file paths with dir.
func (a *BasicAuth) SetDirectory(dir string) {
	if a == nil {
		return
	}
	a.PasswordFile = JoinDir(dir, a.PasswordFile)
	a.UsernameFile = JoinDir(dir, a.UsernameFile)
}

// Authorization contains HTTP authorization credentials.
type Authorization struct {
	Type            string `yaml:"type,omitempty" json:"type,omitempty"`
	Credentials     Secret `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	CredentialsFile string `yaml:"credentials_file,omitempty" json:"credentials_file,omitempty"`
}

// SetDirectory joins any relative file paths with dir.
func (a *Authorization) SetDirectory(dir string) {
	if a == nil {
		return
	}
	a.CredentialsFile = JoinDir(dir, a.CredentialsFile)
}

// URL is a custom URL type that allows validation at configuration load time.
type URL struct {
	*url.URL
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for URLs.
func (u *URL) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	urlp, err := url.Parse(s)
	if err != nil {
		return err
	}
	u.URL = urlp
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface for URLs.
func (u URL) MarshalYAML() (interface{}, error) {
	if u.URL != nil {
		return u.Redacted(), nil
	}
	return nil, nil
}

// Redacted returns the URL but replaces any password with "xxxxx".
func (u URL) Redacted() string {
	if u.URL == nil {
		return ""
	}

	ru := *u.URL
	if _, ok := ru.User.Password(); ok {
		// We can not use secretToken because it would be escaped.
		ru.User = url.UserPassword(ru.User.Username(), "xxxxx")
	}
	return ru.String()
}

// UnmarshalJSON implements the json.Marshaler interface for URL.
func (u *URL) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	urlp, err := url.Parse(s)
	if err != nil {
		return err
	}
	u.URL = urlp
	return nil
}

// MarshalJSON implements the json.Marshaler interface for URL.
func (u URL) MarshalJSON() ([]byte, error) {
	if u.URL != nil {
		return json.Marshal(u.URL.String())
	}
	return []byte("null"), nil
}

// OAuth2 is the oauth2 client configuration.
type OAuth2 struct {
	ClientID         string            `yaml:"client_id" json:"client_id"`
	ClientSecret     Secret            `yaml:"client_secret" json:"client_secret"`
	ClientSecretFile string            `yaml:"client_secret_file" json:"client_secret_file"`
	Scopes           []string          `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	TokenURL         string            `yaml:"token_url" json:"token_url"`
	EndpointParams   map[string]string `yaml:"endpoint_params,omitempty" json:"endpoint_params,omitempty"`
	TLSConfig        TLSConfig         `yaml:"tls_config,omitempty"`
	ProxyConfig      `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface
func (o *OAuth2) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain OAuth2
	if err := unmarshal((*plain)(o)); err != nil {
		return err
	}
	return o.ProxyConfig.Validate()
}

// UnmarshalJSON implements the json.Marshaler interface for URL.
func (o *OAuth2) UnmarshalJSON(data []byte) error {
	type plain OAuth2
	if err := json.Unmarshal(data, (*plain)(o)); err != nil {
		return err
	}
	return o.ProxyConfig.Validate()
}

// SetDirectory joins any relative file paths with dir.
func (a *OAuth2) SetDirectory(dir string) {
	if a == nil {
		return
	}
	a.ClientSecretFile = JoinDir(dir, a.ClientSecretFile)
	a.TLSConfig.SetDirectory(dir)
}

// LoadHTTPConfig parses the YAML input s into a HTTPClientConfig.
func LoadHTTPConfig(s string) (*HTTPClientConfig, error) {
	cfg := &HTTPClientConfig{}
	err := yaml.UnmarshalStrict([]byte(s), cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadHTTPConfigFile parses the given YAML file into a HTTPClientConfig.
func LoadHTTPConfigFile(filename string) (*HTTPClientConfig, []byte, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := LoadHTTPConfig(string(content))
	if err != nil {
		return nil, nil, err
	}
	cfg.SetDirectory(filepath.Dir(filepath.Dir(filename)))
	return cfg, content, nil
}

// HTTPClientConfig configures an HTTP client.
type HTTPClientConfig struct {
	// The HTTP basic authentication credentials for the targets.
	BasicAuth *BasicAuth `yaml:"basic_auth,omitempty" json:"basic_auth,omitempty"`
	// The HTTP authorization credentials for the targets.
	Authorization *Authorization `yaml:"authorization,omitempty" json:"authorization,omitempty"`
	// The OAuth2 client credentials used to fetch a token for the targets.
	OAuth2 *OAuth2 `yaml:"oauth2,omitempty" json:"oauth2,omitempty"`
	// The bearer token for the targets. Deprecated in favour of
	// Authorization.Credentials.
	BearerToken Secret `yaml:"bearer_token,omitempty" json:"bearer_token,omitempty"`
	// The bearer token file for the targets. Deprecated in favour of
	// Authorization.CredentialsFile.
	BearerTokenFile string `yaml:"bearer_token_file,omitempty" json:"bearer_token_file,omitempty"`
	// TLSConfig to use to connect to the targets.
	TLSConfig TLSConfig `yaml:"tls_config,omitempty" json:"tls_config,omitempty"`
	// FollowRedirects specifies whether the client should follow HTTP 3xx redirects.
	// The omitempty flag is not set, because it would be hidden from the
	// marshalled configuration when set to false.
	FollowRedirects bool `yaml:"follow_redirects" json:"follow_redirects"`
	// EnableHTTP2 specifies whether the client should configure HTTP2.
	// The omitempty flag is not set, because it would be hidden from the
	// marshalled configuration when set to false.
	EnableHTTP2 bool `yaml:"enable_http2" json:"enable_http2"`
	// Proxy configuration.
	ProxyConfig `yaml:",inline"`
}

// SetDirectory joins any relative file paths with dir.
func (c *HTTPClientConfig) SetDirectory(dir string) {
	if c == nil {
		return
	}
	c.TLSConfig.SetDirectory(dir)
	c.BasicAuth.SetDirectory(dir)
	c.Authorization.SetDirectory(dir)
	c.OAuth2.SetDirectory(dir)
	c.BearerTokenFile = JoinDir(dir, c.BearerTokenFile)
}

// Validate validates the HTTPClientConfig to check only one of BearerToken,
// BasicAuth and BearerTokenFile is configured. It also validates that ProxyURL
// is set if ProxyConnectHeader is set.
func (c *HTTPClientConfig) Validate() error {
	// Backwards compatibility with the bearer_token field.
	if len(c.BearerToken) > 0 && len(c.BearerTokenFile) > 0 {
		return fmt.Errorf("at most one of bearer_token & bearer_token_file must be configured")
	}
	if (c.BasicAuth != nil || c.OAuth2 != nil) && (len(c.BearerToken) > 0 || len(c.BearerTokenFile) > 0) {
		return fmt.Errorf("at most one of basic_auth, oauth2, bearer_token & bearer_token_file must be configured")
	}
	if c.BasicAuth != nil && (string(c.BasicAuth.Username) != "" && c.BasicAuth.UsernameFile != "") {
		return fmt.Errorf("at most one of basic_auth username & username_file must be configured")
	}
	if c.BasicAuth != nil && (string(c.BasicAuth.Password) != "" && c.BasicAuth.PasswordFile != "") {
		return fmt.Errorf("at most one of basic_auth password & password_file must be configured")
	}
	if c.Authorization != nil {
		if len(c.BearerToken) > 0 || len(c.BearerTokenFile) > 0 {
			return fmt.Errorf("authorization is not compatible with bearer_token & bearer_token_file")
		}
		if string(c.Authorization.Credentials) != "" && c.Authorization.CredentialsFile != "" {
			return fmt.Errorf("at most one of authorization credentials & credentials_file must be configured")
		}
		c.Authorization.Type = strings.TrimSpace(c.Authorization.Type)
		if len(c.Authorization.Type) == 0 {
			c.Authorization.Type = "Bearer"
		}
		if strings.ToLower(c.Authorization.Type) == "basic" {
			return fmt.Errorf(`authorization type cannot be set to "basic", use "basic_auth" instead`)
		}
		if c.BasicAuth != nil || c.OAuth2 != nil {
			return fmt.Errorf("at most one of basic_auth, oauth2 & authorization must be configured")
		}
	} else {
		if len(c.BearerToken) > 0 {
			c.Authorization = &Authorization{Credentials: c.BearerToken}
			c.Authorization.Type = "Bearer"
			c.BearerToken = ""
		}
		if len(c.BearerTokenFile) > 0 {
			c.Authorization = &Authorization{CredentialsFile: c.BearerTokenFile}
			c.Authorization.Type = "Bearer"
			c.BearerTokenFile = ""
		}
	}
	if c.OAuth2 != nil {
		if c.BasicAuth != nil {
			return fmt.Errorf("at most one of basic_auth, oauth2 & authorization must be configured")
		}
		if len(c.OAuth2.ClientID) == 0 {
			return fmt.Errorf("oauth2 client_id must be configured")
		}
		if len(c.OAuth2.TokenURL) == 0 {
			return fmt.Errorf("oauth2 token_url must be configured")
		}
		if len(c.OAuth2.ClientSecret) > 0 && len(c.OAuth2.ClientSecretFile) > 0 {
			return fmt.Errorf("at most one of oauth2 client_secret & client_secret_file must be configured")
		}
	}
	if err := c.ProxyConfig.Validate(); err != nil {
		return err
	}
	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface
func (c *HTTPClientConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HTTPClientConfig
	*c = DefaultHTTPClientConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	return c.Validate()
}

// UnmarshalJSON implements the json.Marshaler interface for URL.
func (c *HTTPClientConfig) UnmarshalJSON(data []byte) error {
	type plain HTTPClientConfig
	*c = DefaultHTTPClientConfig
	if err := json.Unmarshal(data, (*plain)(c)); err != nil {
		return err
	}
	return c.Validate()
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (a *BasicAuth) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain BasicAuth
	return unmarshal((*plain)(a))
}

// DialContextFunc defines the signature of the DialContext() function implemented
// by net.Dialer.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

type httpClientOptions struct {
	dialContextFunc   DialContextFunc
	keepAlivesEnabled bool
	http2Enabled      bool
	idleConnTimeout   time.Duration
	userAgent         string
	host              string
}

// HTTPClientOption defines an option that can be applied to the HTTP client.
type HTTPClientOption func(options *httpClientOptions)

// WithDialContextFunc allows you to override func gets used for the actual dialing. The default is `net.Dialer.DialContext`.
func WithDialContextFunc(fn DialContextFunc) HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.dialContextFunc = fn
	}
}

// WithKeepAlivesDisabled allows to disable HTTP keepalive.
func WithKeepAlivesDisabled() HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.keepAlivesEnabled = false
	}
}

// WithHTTP2Disabled allows to disable HTTP2.
func WithHTTP2Disabled() HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.http2Enabled = false
	}
}

// WithIdleConnTimeout allows setting the idle connection timeout.
func WithIdleConnTimeout(timeout time.Duration) HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.idleConnTimeout = timeout
	}
}

// WithUserAgent allows setting the user agent.
func WithUserAgent(ua string) HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.userAgent = ua
	}
}

// WithHost allows setting the host header.
func WithHost(host string) HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.host = host
	}
}

// NewClient returns a http.Client using the specified http.RoundTripper.
func newClient(rt http.RoundTripper) *http.Client {
	return &http.Client{Transport: rt}
}

// NewClientFromConfig returns a new HTTP client configured for the
// given config.HTTPClientConfig and config.HTTPClientOption.
// The name is used as go-conntrack metric label.
func NewClientFromConfig(cfg HTTPClientConfig, name string, optFuncs ...HTTPClientOption) (*http.Client, error) {
	rt, err := NewRoundTripperFromConfig(cfg, name, optFuncs...)
	if err != nil {
		return nil, err
	}
	client := newClient(rt)
	if !cfg.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client, nil
}

// NewRoundTripperFromConfig returns a new HTTP RoundTripper configured for the
// given config.HTTPClientConfig and config.HTTPClientOption.
// The name is used as go-conntrack metric label.
func NewRoundTripperFromConfig(cfg HTTPClientConfig, name string, optFuncs ...HTTPClientOption) (http.RoundTripper, error) {
	opts := defaultHTTPClientOptions
	for _, f := range optFuncs {
		f(&opts)
	}

	var dialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	if opts.dialContextFunc != nil {
		dialContext = conntrack.NewDialContextFunc(
			conntrack.DialWithDialContextFunc((func(context.Context, string, string) (net.Conn, error))(opts.dialContextFunc)),
			conntrack.DialWithTracing(),
			conntrack.DialWithName(name))
	} else {
		dialContext = conntrack.NewDialContextFunc(
			conntrack.DialWithTracing(),
			conntrack.DialWithName(name))
	}

	newRT := func(tlsConfig *tls.Config) (http.RoundTripper, error) {
		// The only timeout we care about is the configured scrape timeout.
		// It is applied on request. So we leave out any timings here.
		var rt http.RoundTripper = &http.Transport{
			Proxy:                 cfg.ProxyConfig.Proxy(),
			ProxyConnectHeader:    cfg.ProxyConfig.GetProxyConnectHeader(),
			MaxIdleConns:          20000,
			MaxIdleConnsPerHost:   1000, // see https://github.com/golang/go/issues/13801
			DisableKeepAlives:     !opts.keepAlivesEnabled,
			TLSClientConfig:       tlsConfig,
			DisableCompression:    true,
			IdleConnTimeout:       opts.idleConnTimeout,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DialContext:           dialContext,
		}
		if opts.http2Enabled && cfg.EnableHTTP2 {
			// HTTP/2 support is golang had many problematic cornercases where
			// dead connections would be kept and used in connection pools.
			// https://github.com/golang/go/issues/32388
			// https://github.com/golang/go/issues/39337
			// https://github.com/golang/go/issues/39750

			http2t, err := http2.ConfigureTransports(rt.(*http.Transport))
			if err != nil {
				return nil, err
			}
			http2t.ReadIdleTimeout = time.Minute
		}

		// If a authorization_credentials is provided, create a round tripper that will set the
		// Authorization header correctly on each request.
		if cfg.Authorization != nil && len(cfg.Authorization.CredentialsFile) > 0 {
			rt = NewAuthorizationCredentialsFileRoundTripper(cfg.Authorization.Type, cfg.Authorization.CredentialsFile, rt)
		} else if cfg.Authorization != nil {
			rt = NewAuthorizationCredentialsRoundTripper(cfg.Authorization.Type, cfg.Authorization.Credentials, rt)
		}
		// Backwards compatibility, be nice with importers who would not have
		// called Validate().
		if len(cfg.BearerToken) > 0 {
			rt = NewAuthorizationCredentialsRoundTripper("Bearer", cfg.BearerToken, rt)
		} else if len(cfg.BearerTokenFile) > 0 {
			rt = NewAuthorizationCredentialsFileRoundTripper("Bearer", cfg.BearerTokenFile, rt)
		}

		if cfg.BasicAuth != nil {
			rt = NewBasicAuthRoundTripper(cfg.BasicAuth.Username, cfg.BasicAuth.Password, cfg.BasicAuth.UsernameFile, cfg.BasicAuth.PasswordFile, rt)
		}

		if cfg.OAuth2 != nil {
			rt = NewOAuth2RoundTripper(cfg.OAuth2, rt, &opts)
		}

		if opts.userAgent != "" {
			rt = NewUserAgentRoundTripper(opts.userAgent, rt)
		}

		if opts.host != "" {
			rt = NewHostRoundTripper(opts.host, rt)
		}

		// Return a new configured RoundTripper.
		return rt, nil
	}

	tlsConfig, err := NewTLSConfig(&cfg.TLSConfig)
	if err != nil {
		return nil, err
	}

	if len(cfg.TLSConfig.CAFile) == 0 {
		// No need for a RoundTripper that reloads the CA file automatically.
		return newRT(tlsConfig)
	}
	return NewTLSRoundTripper(tlsConfig, cfg.TLSConfig.roundTripperSettings(), newRT)
}

type authorizationCredentialsRoundTripper struct {
	authType        string
	authCredentials Secret
	rt              http.RoundTripper
}

// NewAuthorizationCredentialsRoundTripper adds the provided credentials to a
// request unless the authorization header has already been set.
func NewAuthorizationCredentialsRoundTripper(authType string, authCredentials Secret, rt http.RoundTripper) http.RoundTripper {
	return &authorizationCredentialsRoundTripper{authType, authCredentials, rt}
}

func (rt *authorizationCredentialsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(req.Header.Get("Authorization")) == 0 {
		req = cloneRequest(req)
		req.Header.Set("Authorization", fmt.Sprintf("%s %s", rt.authType, string(rt.authCredentials)))
	}
	return rt.rt.RoundTrip(req)
}

func (rt *authorizationCredentialsRoundTripper) CloseIdleConnections() {
	if ci, ok := rt.rt.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

type authorizationCredentialsFileRoundTripper struct {
	authType            string
	authCredentialsFile string
	rt                  http.RoundTripper
}

// NewAuthorizationCredentialsFileRoundTripper adds the authorization
// credentials read from the provided file to a request unless the authorization
// header has already been set. This file is read for every request.
func NewAuthorizationCredentialsFileRoundTripper(authType, authCredentialsFile string, rt http.RoundTripper) http.RoundTripper {
	return &authorizationCredentialsFileRoundTripper{authType, authCredentialsFile, rt}
}

func (rt *authorizationCredentialsFileRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(req.Header.Get("Authorization")) == 0 {
		b, err := os.ReadFile(rt.authCredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read authorization credentials file %s: %w", rt.authCredentialsFile, err)
		}
		authCredentials := strings.TrimSpace(string(b))

		req = cloneRequest(req)
		req.Header.Set("Authorization", fmt.Sprintf("%s %s", rt.authType, authCredentials))
	}

	return rt.rt.RoundTrip(req)
}

func (rt *authorizationCredentialsFileRoundTripper) CloseIdleConnections() {
	if ci, ok := rt.rt.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

type basicAuthRoundTripper struct {
	username     string
	password     Secret
	usernameFile string
	passwordFile string
	rt           http.RoundTripper
}

// NewBasicAuthRoundTripper will apply a BASIC auth authorization header to a request unless it has
// already been set.
func NewBasicAuthRoundTripper(username string, password Secret, usernameFile, passwordFile string, rt http.RoundTripper) http.RoundTripper {
	return &basicAuthRoundTripper{username, password, usernameFile, passwordFile, rt}
}

func (rt *basicAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var username string
	var password string
	if len(req.Header.Get("Authorization")) != 0 {
		return rt.rt.RoundTrip(req)
	}
	if rt.usernameFile != "" {
		usernameBytes, err := os.ReadFile(rt.usernameFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read basic auth username file %s: %w", rt.usernameFile, err)
		}
		username = strings.TrimSpace(string(usernameBytes))
	} else {
		username = rt.username
	}
	if rt.passwordFile != "" {
		passwordBytes, err := os.ReadFile(rt.passwordFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read basic auth password file %s: %w", rt.passwordFile, err)
		}
		password = strings.TrimSpace(string(passwordBytes))
	} else {
		password = string(rt.password)
	}
	req = cloneRequest(req)
	req.SetBasicAuth(username, password)
	return rt.rt.RoundTrip(req)
}

func (rt *basicAuthRoundTripper) CloseIdleConnections() {
	if ci, ok := rt.rt.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

type oauth2RoundTripper struct {
	config *OAuth2
	rt     http.RoundTripper
	next   http.RoundTripper
	secret string
	mtx    sync.RWMutex
	opts   *httpClientOptions
	client *http.Client
}

func NewOAuth2RoundTripper(config *OAuth2, next http.RoundTripper, opts *httpClientOptions) http.RoundTripper {
	return &oauth2RoundTripper{
		config: config,
		next:   next,
		opts:   opts,
	}
}

func (rt *oauth2RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		secret  string
		changed bool
	)

	if rt.config.ClientSecretFile != "" {
		data, err := os.ReadFile(rt.config.ClientSecretFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read oauth2 client secret file %s: %w", rt.config.ClientSecretFile, err)
		}
		secret = strings.TrimSpace(string(data))
		rt.mtx.RLock()
		changed = secret != rt.secret
		rt.mtx.RUnlock()
	} else {
		// Either an inline secret or nothing (use an empty string) was provided.
		secret = string(rt.config.ClientSecret)
	}

	if changed || rt.rt == nil {
		config := &clientcredentials.Config{
			ClientID:       rt.config.ClientID,
			ClientSecret:   secret,
			Scopes:         rt.config.Scopes,
			TokenURL:       rt.config.TokenURL,
			EndpointParams: mapToValues(rt.config.EndpointParams),
		}

		tlsConfig, err := NewTLSConfig(&rt.config.TLSConfig)
		if err != nil {
			return nil, err
		}

		tlsTransport := func(tlsConfig *tls.Config) (http.RoundTripper, error) {
			return &http.Transport{
				TLSClientConfig:       tlsConfig,
				Proxy:                 rt.config.ProxyConfig.Proxy(),
				ProxyConnectHeader:    rt.config.ProxyConfig.GetProxyConnectHeader(),
				DisableKeepAlives:     !rt.opts.keepAlivesEnabled,
				MaxIdleConns:          20,
				MaxIdleConnsPerHost:   1, // see https://github.com/golang/go/issues/13801
				IdleConnTimeout:       10 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}, nil
		}

		var t http.RoundTripper
		if len(rt.config.TLSConfig.CAFile) == 0 {
			t, _ = tlsTransport(tlsConfig)
		} else {
			t, err = NewTLSRoundTripper(tlsConfig, rt.config.TLSConfig.roundTripperSettings(), tlsTransport)
			if err != nil {
				return nil, err
			}
		}

		if ua := req.UserAgent(); ua != "" {
			t = NewUserAgentRoundTripper(ua, t)
		}

		client := &http.Client{Transport: t}
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
		tokenSource := config.TokenSource(ctx)

		rt.mtx.Lock()
		rt.secret = secret
		rt.rt = &oauth2.Transport{
			Base:   rt.next,
			Source: tokenSource,
		}
		if rt.client != nil {
			rt.client.CloseIdleConnections()
		}
		rt.client = client
		rt.mtx.Unlock()
	}

	rt.mtx.RLock()
	currentRT := rt.rt
	rt.mtx.RUnlock()
	return currentRT.RoundTrip(req)
}

func (rt *oauth2RoundTripper) CloseIdleConnections() {
	if rt.client != nil {
		rt.client.CloseIdleConnections()
	}
	if ci, ok := rt.next.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

func mapToValues(m map[string]string) url.Values {
	v := url.Values{}
	for name, value := range m {
		v.Set(name, value)
	}

	return v
}

// cloneRequest returns a clone of the provided *http.Request.
// The clone is a shallow copy of the struct and its Header map.
func cloneRequest(r *http.Request) *http.Request {
	// Shallow copy of the struct.
	r2 := new(http.Request)
	*r2 = *r
	// Deep copy of the Header.
	r2.Header = make(http.Header)
	for k, s := range r.Header {
		r2.Header[k] = s
	}
	return r2
}

// NewTLSConfig creates a new tls.Config from the given TLSConfig.
func NewTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         uint16(cfg.MinVersion),
		MaxVersion:         uint16(cfg.MaxVersion),
	}

	if cfg.MaxVersion != 0 && cfg.MinVersion != 0 {
		if cfg.MaxVersion < cfg.MinVersion {
			return nil, fmt.Errorf("tls_config.max_version must be greater than or equal to tls_config.min_version if both are specified")
		}
	}

	// If a CA cert is provided then let's read it in so we can validate the
	// scrape target's certificate properly.
	if len(cfg.CA) > 0 {
		if !updateRootCA(tlsConfig, []byte(cfg.CA)) {
			return nil, fmt.Errorf("unable to use inline CA cert")
		}
	} else if len(cfg.CAFile) > 0 {
		b, err := readCAFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		if !updateRootCA(tlsConfig, b) {
			return nil, fmt.Errorf("unable to use specified CA cert %s", cfg.CAFile)
		}
	}

	if len(cfg.ServerName) > 0 {
		tlsConfig.ServerName = cfg.ServerName
	}

	// If a client cert & key is provided then configure TLS config accordingly.
	if cfg.usingClientCert() && cfg.usingClientKey() {
		// Verify that client cert and key are valid.
		if _, err := cfg.getClientCertificate(nil); err != nil {
			return nil, err
		}
		tlsConfig.GetClientCertificate = cfg.getClientCertificate
	}

	return tlsConfig, nil
}

// TLSConfig configures the options for TLS connections.
type TLSConfig struct {
	// Text of the CA cert to use for the targets.
	CA string `yaml:"ca,omitempty" json:"ca,omitempty"`
	// Text of the client cert file for the targets.
	Cert string `yaml:"cert,omitempty" json:"cert,omitempty"`
	// Text of the client key file for the targets.
	Key Secret `yaml:"key,omitempty" json:"key,omitempty"`
	// The CA cert to use for the targets.
	CAFile string `yaml:"ca_file,omitempty" json:"ca_file,omitempty"`
	// The client cert file for the targets.
	CertFile string `yaml:"cert_file,omitempty" json:"cert_file,omitempty"`
	// The client key file for the targets.
	KeyFile string `yaml:"key_file,omitempty" json:"key_file,omitempty"`
	// Used to verify the hostname for the targets.
	ServerName string `yaml:"server_name,omitempty" json:"server_name,omitempty"`
	// Disable target certificate validation.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
	// Minimum TLS version.
	MinVersion TLSVersion `yaml:"min_version,omitempty" json:"min_version,omitempty"`
	// Maximum TLS version.
	MaxVersion TLSVersion `yaml:"max_version,omitempty" json:"max_version,omitempty"`
}

// SetDirectory joins any relative file paths with dir.
func (c *TLSConfig) SetDirectory(dir string) {
	if c == nil {
		return
	}
	c.CAFile = JoinDir(dir, c.CAFile)
	c.CertFile = JoinDir(dir, c.CertFile)
	c.KeyFile = JoinDir(dir, c.KeyFile)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *TLSConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain TLSConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	return c.Validate()
}

// Validate validates the TLSConfig to check that only one of the inlined or
// file-based fields for the TLS CA, client certificate, and client key are
// used.
func (c *TLSConfig) Validate() error {
	if len(c.CA) > 0 && len(c.CAFile) > 0 {
		return fmt.Errorf("at most one of ca and ca_file must be configured")
	}
	if len(c.Cert) > 0 && len(c.CertFile) > 0 {
		return fmt.Errorf("at most one of cert and cert_file must be configured")
	}
	if len(c.Key) > 0 && len(c.KeyFile) > 0 {
		return fmt.Errorf("at most one of key and key_file must be configured")
	}

	if c.usingClientCert() && !c.usingClientKey() {
		return fmt.Errorf("exactly one of key or key_file must be configured when a client certificate is configured")
	} else if c.usingClientKey() && !c.usingClientCert() {
		return fmt.Errorf("exactly one of cert or cert_file must be configured when a client key is configured")
	}

	return nil
}

func (c *TLSConfig) usingClientCert() bool {
	return len(c.Cert) > 0 || len(c.CertFile) > 0
}

func (c *TLSConfig) usingClientKey() bool {
	return len(c.Key) > 0 || len(c.KeyFile) > 0
}

func (c *TLSConfig) roundTripperSettings() TLSRoundTripperSettings {
	return TLSRoundTripperSettings{
		CA:       c.CA,
		CAFile:   c.CAFile,
		Cert:     c.Cert,
		CertFile: c.CertFile,
		Key:      string(c.Key),
		KeyFile:  c.KeyFile,
	}
}

// getClientCertificate reads the pair of client cert and key from disk and returns a tls.Certificate.
func (c *TLSConfig) getClientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	var (
		certData, keyData []byte
		err               error
	)

	if c.CertFile != "" {
		certData, err = os.ReadFile(c.CertFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read specified client cert (%s): %w", c.CertFile, err)
		}
	} else {
		certData = []byte(c.Cert)
	}

	if c.KeyFile != "" {
		keyData, err = os.ReadFile(c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read specified client key (%s): %w", c.KeyFile, err)
		}
	} else {
		keyData = []byte(c.Key)
	}

	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, fmt.Errorf("unable to use specified client cert (%s) & key (%s): %w", c.CertFile, c.KeyFile, err)
	}

	return &cert, nil
}

// readCAFile reads the CA cert file from disk.
func readCAFile(f string) ([]byte, error) {
	data, err := os.ReadFile(f)
	if err != nil {
		return nil, fmt.Errorf("unable to load specified CA cert %s: %w", f, err)
	}
	return data, nil
}

// updateRootCA parses the given byte slice as a series of PEM encoded certificates and updates tls.Config.RootCAs.
func updateRootCA(cfg *tls.Config, b []byte) bool {
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(b) {
		return false
	}
	cfg.RootCAs = caCertPool
	return true
}

// tlsRoundTripper is a RoundTripper that updates automatically its TLS
// configuration whenever the content of the CA file changes.
type tlsRoundTripper struct {
	settings TLSRoundTripperSettings

	// newRT returns a new RoundTripper.
	newRT func(*tls.Config) (http.RoundTripper, error)

	mtx          sync.RWMutex
	rt           http.RoundTripper
	hashCAData   []byte
	hashCertData []byte
	hashKeyData  []byte
	tlsConfig    *tls.Config
}

type TLSRoundTripperSettings struct {
	CA, CAFile     string
	Cert, CertFile string
	Key, KeyFile   string
}

func NewTLSRoundTripper(
	cfg *tls.Config,
	settings TLSRoundTripperSettings,
	newRT func(*tls.Config) (http.RoundTripper, error),
) (http.RoundTripper, error) {
	t := &tlsRoundTripper{
		settings:  settings,
		newRT:     newRT,
		tlsConfig: cfg,
	}

	rt, err := t.newRT(t.tlsConfig)
	if err != nil {
		return nil, err
	}
	t.rt = rt
	_, t.hashCAData, t.hashCertData, t.hashKeyData, err = t.getTLSDataWithHash()
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (t *tlsRoundTripper) getTLSDataWithHash() ([]byte, []byte, []byte, []byte, error) {
	var (
		caBytes, certBytes, keyBytes []byte

		err error
	)

	if t.settings.CAFile != "" {
		caBytes, err = os.ReadFile(t.settings.CAFile)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	} else if t.settings.CA != "" {
		caBytes = []byte(t.settings.CA)
	}

	if t.settings.CertFile != "" {
		certBytes, err = os.ReadFile(t.settings.CertFile)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	} else if t.settings.Cert != "" {
		certBytes = []byte(t.settings.Cert)
	}

	if t.settings.KeyFile != "" {
		keyBytes, err = os.ReadFile(t.settings.KeyFile)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	} else if t.settings.Key != "" {
		keyBytes = []byte(t.settings.Key)
	}

	var caHash, certHash, keyHash [32]byte

	if len(caBytes) > 0 {
		caHash = sha256.Sum256(caBytes)
	}
	if len(certBytes) > 0 {
		certHash = sha256.Sum256(certBytes)
	}
	if len(keyBytes) > 0 {
		keyHash = sha256.Sum256(keyBytes)
	}

	return caBytes, caHash[:], certHash[:], keyHash[:], nil
}

// RoundTrip implements the http.RoundTrip interface.
func (t *tlsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	caData, caHash, certHash, keyHash, err := t.getTLSDataWithHash()
	if err != nil {
		return nil, err
	}

	t.mtx.RLock()
	equal := bytes.Equal(caHash[:], t.hashCAData) &&
		bytes.Equal(certHash[:], t.hashCertData) &&
		bytes.Equal(keyHash[:], t.hashKeyData)
	rt := t.rt
	t.mtx.RUnlock()
	if equal {
		// The CA cert hasn't changed, use the existing RoundTripper.
		return rt.RoundTrip(req)
	}

	// Create a new RoundTripper.
	// The cert and key files are read separately by the client
	// using GetClientCertificate.
	tlsConfig := t.tlsConfig.Clone()
	if !updateRootCA(tlsConfig, caData) {
		return nil, fmt.Errorf("unable to use specified CA cert %s", t.settings.CAFile)
	}
	rt, err = t.newRT(tlsConfig)
	if err != nil {
		return nil, err
	}
	t.CloseIdleConnections()

	t.mtx.Lock()
	t.rt = rt
	t.hashCAData = caHash[:]
	t.hashCertData = certHash[:]
	t.hashKeyData = keyHash[:]
	t.mtx.Unlock()

	return rt.RoundTrip(req)
}

func (t *tlsRoundTripper) CloseIdleConnections() {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	if ci, ok := t.rt.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

type userAgentRoundTripper struct {
	userAgent string
	rt        http.RoundTripper
}

type hostRoundTripper struct {
	host string
	rt   http.RoundTripper
}

// NewUserAgentRoundTripper adds the user agent every request header.
func NewUserAgentRoundTripper(userAgent string, rt http.RoundTripper) http.RoundTripper {
	return &userAgentRoundTripper{userAgent, rt}
}

// NewHostRoundTripper sets the [http.Request.Host] of every request.
func NewHostRoundTripper(host string, rt http.RoundTripper) http.RoundTripper {
	return &hostRoundTripper{host, rt}
}

func (rt *userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = cloneRequest(req)
	req.Header.Set("User-Agent", rt.userAgent)
	return rt.rt.RoundTrip(req)
}

func (rt *userAgentRoundTripper) CloseIdleConnections() {
	if ci, ok := rt.rt.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

func (rt *hostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = cloneRequest(req)
	req.Host = rt.host
	req.Header.Set("Host", rt.host)
	return rt.rt.RoundTrip(req)
}

func (rt *hostRoundTripper) CloseIdleConnections() {
	if ci, ok := rt.rt.(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

func (c HTTPClientConfig) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("<error creating http client config string: %s>", err)
	}
	return string(b)
}

type ProxyConfig struct {
	// HTTP proxy server to use to connect to the targets.
	ProxyURL URL `yaml:"proxy_url,omitempty" json:"proxy_url,omitempty"`
	// NoProxy contains addresses that should not use a proxy.
	NoProxy string `yaml:"no_proxy,omitempty" json:"no_proxy,omitempty"`
	// ProxyFromEnvironment makes use of net/http ProxyFromEnvironment function
	// to determine proxies.
	ProxyFromEnvironment bool `yaml:"proxy_from_environment,omitempty" json:"proxy_from_environment,omitempty"`
	// ProxyConnectHeader optionally specifies headers to send to
	// proxies during CONNECT requests. Assume that at least _some_ of
	// these headers are going to contain secrets and use Secret as the
	// value type instead of string.
	ProxyConnectHeader Header `yaml:"proxy_connect_header,omitempty" json:"proxy_connect_header,omitempty"`

	proxyFunc func(*http.Request) (*url.URL, error)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *ProxyConfig) Validate() error {
	if len(c.ProxyConnectHeader) > 0 && (!c.ProxyFromEnvironment && (c.ProxyURL.URL == nil || c.ProxyURL.String() == "")) {
		return fmt.Errorf("if proxy_connect_header is configured, proxy_url or proxy_from_environment must also be configured")
	}
	if c.ProxyFromEnvironment && c.ProxyURL.URL != nil && c.ProxyURL.String() != "" {
		return fmt.Errorf("if proxy_from_environment is configured, proxy_url must not be configured")
	}
	if c.ProxyFromEnvironment && c.NoProxy != "" {
		return fmt.Errorf("if proxy_from_environment is configured, no_proxy must not be configured")
	}
	if c.ProxyURL.URL == nil && c.NoProxy != "" {
		return fmt.Errorf("if no_proxy is configured, proxy_url must also be configured")
	}
	return nil
}

// Proxy returns the Proxy URL for a request.
func (c *ProxyConfig) Proxy() (fn func(*http.Request) (*url.URL, error)) {
	if c == nil {
		return nil
	}
	defer func() {
		fn = c.proxyFunc
	}()
	if c.proxyFunc != nil {
		return
	}
	if c.ProxyFromEnvironment {
		proxyFn := httpproxy.FromEnvironment().ProxyFunc()
		c.proxyFunc = func(req *http.Request) (*url.URL, error) {
			return proxyFn(req.URL)
		}
		return
	}
	if c.ProxyURL.URL != nil && c.ProxyURL.URL.String() != "" {
		if c.NoProxy == "" {
			c.proxyFunc = http.ProxyURL(c.ProxyURL.URL)
			return
		}
		proxy := &httpproxy.Config{
			HTTPProxy:  c.ProxyURL.String(),
			HTTPSProxy: c.ProxyURL.String(),
			NoProxy:    c.NoProxy,
		}
		proxyFn := proxy.ProxyFunc()
		c.proxyFunc = func(req *http.Request) (*url.URL, error) {
			return proxyFn(req.URL)
		}
	}
	return
}

// ProxyConnectHeader() return the Proxy Connext Headers.
func (c *ProxyConfig) GetProxyConnectHeader() http.Header {
	return c.ProxyConnectHeader.HTTPHeader()
}
