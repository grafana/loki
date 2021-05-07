// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package http is a wrapper around github.com/prometheus/common/config.
package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sync"

	"github.com/go-kit/kit/log"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"
	"github.com/prometheus/prometheus/discovery/file"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/thanos/pkg/discovery/cache"
)

// ClientConfig configures an HTTP client.
type ClientConfig struct {
	// The HTTP basic authentication credentials for the targets.
	BasicAuth BasicAuth `yaml:"basic_auth"`
	// The bearer token for the targets.
	BearerToken string `yaml:"bearer_token"`
	// The bearer token file for the targets.
	BearerTokenFile string `yaml:"bearer_token_file"`
	// HTTP proxy server to use to connect to the targets.
	ProxyURL string `yaml:"proxy_url"`
	// TLSConfig to use to connect to the targets.
	TLSConfig TLSConfig `yaml:"tls_config"`
}

// TLSConfig configures TLS connections.
type TLSConfig struct {
	// The CA cert to use for the targets.
	CAFile string `yaml:"ca_file"`
	// The client cert file for the targets.
	CertFile string `yaml:"cert_file"`
	// The client key file for the targets.
	KeyFile string `yaml:"key_file"`
	// Used to verify the hostname for the targets.
	ServerName string `yaml:"server_name"`
	// Disable target certificate validation.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// BasicAuth configures basic authentication for HTTP clients.
type BasicAuth struct {
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	PasswordFile string `yaml:"password_file"`
}

// IsZero returns false if basic authentication isn't enabled.
func (b BasicAuth) IsZero() bool {
	return b.Username == "" && b.Password == "" && b.PasswordFile == ""
}

// NewHTTPClient returns a new HTTP client.
func NewHTTPClient(cfg ClientConfig, name string) (*http.Client, error) {
	httpClientConfig := config_util.HTTPClientConfig{
		BearerToken:     config_util.Secret(cfg.BearerToken),
		BearerTokenFile: cfg.BearerTokenFile,
		TLSConfig: config_util.TLSConfig{
			CAFile:             cfg.TLSConfig.CAFile,
			CertFile:           cfg.TLSConfig.CertFile,
			KeyFile:            cfg.TLSConfig.KeyFile,
			ServerName:         cfg.TLSConfig.ServerName,
			InsecureSkipVerify: cfg.TLSConfig.InsecureSkipVerify,
		},
	}
	if cfg.ProxyURL != "" {
		var proxy config_util.URL
		err := yaml.Unmarshal([]byte(cfg.ProxyURL), &proxy)
		if err != nil {
			return nil, err
		}
		httpClientConfig.ProxyURL = proxy
	}
	if !cfg.BasicAuth.IsZero() {
		httpClientConfig.BasicAuth = &config_util.BasicAuth{
			Username:     cfg.BasicAuth.Username,
			Password:     config_util.Secret(cfg.BasicAuth.Password),
			PasswordFile: cfg.BasicAuth.PasswordFile,
		}
	}
	if err := httpClientConfig.Validate(); err != nil {
		return nil, err
	}

	client, err := config_util.NewClientFromConfig(httpClientConfig, name, config_util.WithHTTP2Disabled())
	if err != nil {
		return nil, err
	}
	client.Transport = &userAgentRoundTripper{name: ThanosUserAgent, rt: client.Transport}
	return client, nil
}

var ThanosUserAgent = fmt.Sprintf("Thanos/%s", version.Version)

type userAgentRoundTripper struct {
	name string
	rt   http.RoundTripper
}

// RoundTrip implements the http.RoundTripper interface.
func (u userAgentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.UserAgent() == "" {
		// The specification of http.RoundTripper says that it shouldn't mutate
		// the request so make a copy of req.Header since this is all that is
		// modified.
		r2 := new(http.Request)
		*r2 = *r
		r2.Header = make(http.Header)
		for k, s := range r.Header {
			r2.Header[k] = s
		}
		r2.Header.Set("User-Agent", u.name)
		r = r2
	}
	return u.rt.RoundTrip(r)
}

// EndpointsConfig configures a cluster of HTTP endpoints from static addresses and
// file service discovery.
type EndpointsConfig struct {
	// List of addresses with DNS prefixes.
	StaticAddresses []string `yaml:"static_configs"`
	// List of file  configurations (our FileSD supports different DNS lookups).
	FileSDConfigs []FileSDConfig `yaml:"file_sd_configs"`

	// The URL scheme to use when talking to targets.
	Scheme string `yaml:"scheme"`

	// Path prefix to add in front of the endpoint path.
	PathPrefix string `yaml:"path_prefix"`
}

// FileSDConfig represents a file service discovery configuration.
type FileSDConfig struct {
	Files           []string       `yaml:"files"`
	RefreshInterval model.Duration `yaml:"refresh_interval"`
}

func (c FileSDConfig) convert() (file.SDConfig, error) {
	var fileSDConfig file.SDConfig
	b, err := yaml.Marshal(c)
	if err != nil {
		return fileSDConfig, err
	}
	err = yaml.Unmarshal(b, &fileSDConfig)
	if err != nil {
		return fileSDConfig, err
	}
	return fileSDConfig, nil
}

type AddressProvider interface {
	Resolve(context.Context, []string) error
	Addresses() []string
}

// Client represents a client that can send requests to a cluster of HTTP-based endpoints.
type Client struct {
	logger log.Logger

	httpClient *http.Client
	scheme     string
	prefix     string

	staticAddresses []string
	fileSDCache     *cache.Cache
	fileDiscoverers []*file.Discovery

	provider AddressProvider
}

// NewClient returns a new Client.
func NewClient(logger log.Logger, cfg EndpointsConfig, client *http.Client, provider AddressProvider) (*Client, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	var discoverers []*file.Discovery
	for _, sdCfg := range cfg.FileSDConfigs {
		fileSDCfg, err := sdCfg.convert()
		if err != nil {
			return nil, err
		}
		discoverers = append(discoverers, file.NewDiscovery(&fileSDCfg, logger))
	}
	return &Client{
		logger:          logger,
		httpClient:      client,
		scheme:          cfg.Scheme,
		prefix:          cfg.PathPrefix,
		staticAddresses: cfg.StaticAddresses,
		fileSDCache:     cache.New(),
		fileDiscoverers: discoverers,
		provider:        provider,
	}, nil
}

// Do executes an HTTP request with the underlying HTTP client.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req)
}

// Endpoints returns the list of known endpoints.
func (c *Client) Endpoints() []*url.URL {
	var urls []*url.URL
	for _, addr := range c.provider.Addresses() {
		urls = append(urls,
			&url.URL{
				Scheme: c.scheme,
				Host:   addr,
				Path:   path.Join("/", c.prefix),
			},
		)
	}
	return urls
}

// Discover runs the service to discover endpoints until the given context is done.
func (c *Client) Discover(ctx context.Context) {
	var wg sync.WaitGroup
	ch := make(chan []*targetgroup.Group)

	for _, d := range c.fileDiscoverers {
		wg.Add(1)
		go func(d *file.Discovery) {
			d.Run(ctx, ch)
			wg.Done()
		}(d)
	}

	func() {
		for {
			select {
			case update := <-ch:
				// Discoverers sometimes send nil updates so need to check for it to avoid panics.
				if update == nil {
					continue
				}
				c.fileSDCache.Update(update)
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
}

// Resolve refreshes and resolves the list of targets.
func (c *Client) Resolve(ctx context.Context) error {
	return c.provider.Resolve(ctx, append(c.fileSDCache.Addresses(), c.staticAddresses...))
}
