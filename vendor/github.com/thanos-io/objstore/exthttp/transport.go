// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package exthttp

import (
	"net"
	"net/http"
	"time"

	"github.com/prometheus/common/model"
)

var DefaultHTTPConfig = HTTPConfig{
	IdleConnTimeout:       model.Duration(90 * time.Second),
	ResponseHeaderTimeout: model.Duration(2 * time.Minute),
	TLSHandshakeTimeout:   model.Duration(10 * time.Second),
	ExpectContinueTimeout: model.Duration(1 * time.Second),
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   100,
	MaxConnsPerHost:       0,
}

// HTTPConfig stores the http.Transport configuration for the cos and s3 minio client.
type HTTPConfig struct {
	IdleConnTimeout       model.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout model.Duration `yaml:"response_header_timeout"`
	InsecureSkipVerify    bool           `yaml:"insecure_skip_verify"`

	TLSHandshakeTimeout   model.Duration `yaml:"tls_handshake_timeout"`
	ExpectContinueTimeout model.Duration `yaml:"expect_continue_timeout"`
	MaxIdleConns          int            `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost   int            `yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost       int            `yaml:"max_conns_per_host"`

	// Transport field allows upstream callers to inject a custom round tripper.
	Transport http.RoundTripper `yaml:"-"`

	TLSConfig          TLSConfig `yaml:"tls_config"`
	DisableCompression bool      `yaml:"disable_compression"`
}

// DefaultTransport - this default transport is based on the Minio
// DefaultTransport up until the following commit:
// https://github.com/minio/minio-go/commit/008c7aa71fc17e11bf980c209a4f8c4d687fc884
// The values have since diverged.
func DefaultTransport(config HTTPConfig) (*http.Transport, error) {
	tlsConfig, err := NewTLSConfig(&config.TLSConfig)
	if err != nil {
		return nil, err
	}
	tlsConfig.InsecureSkipVerify = config.InsecureSkipVerify

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,

		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(config.IdleConnTimeout),
		MaxConnsPerHost:       config.MaxConnsPerHost,
		TLSHandshakeTimeout:   time.Duration(config.TLSHandshakeTimeout),
		ExpectContinueTimeout: time.Duration(config.ExpectContinueTimeout),
		// A custom ResponseHeaderTimeout was introduced
		// to cover cases where the tcp connection works but
		// the server never answers. Defaults to 2 minutes.
		ResponseHeaderTimeout: time.Duration(config.ResponseHeaderTimeout),
		// Set this value so that the underlying transport round-tripper
		// doesn't try to auto decode the body of objects with
		// content-encoding set to `gzip`.
		//
		// Refer: https://golang.org/src/net/http/transport.go?h=roundTrip#L1843.
		TLSClientConfig: tlsConfig,
	}, nil
}
