// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package common

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// TransportTemplateProvider defines a function that creates a new http transport
// from a given TLS client config.
type TransportTemplateProvider func(tlsClientConfig *tls.Config) (http.RoundTripper, error)

// NewOrDefault creates a new TransportTemplate
// If t is nil, then DefaultTransport is returned
func (t TransportTemplateProvider) NewOrDefault(tlsClientConfig *tls.Config) (http.RoundTripper, error) {
	if t == nil {
		return DefaultTransport(tlsClientConfig)
	}
	return t(tlsClientConfig)
}

// DefaultTransport creates a clone of http.DefaultTransport
// and applies the tlsClientConfig on top of it.
// The result is never nil, to prevent panics in client code.
// Never returns any errors, but needs to return an error
// to adhere to TransportTemplate interface.
func DefaultTransport(tlsClientConfig *tls.Config) (*http.Transport, error) {
	transport := CloneHTTPDefaultTransport()
	if isExpectHeaderDisabled := IsEnvVarFalse(UsingExpectHeaderEnvVar); !isExpectHeaderDisabled {
		transport.Proxy = http.ProxyFromEnvironment
		transport.DialContext = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext
		transport.ForceAttemptHTTP2 = true
		transport.MaxIdleConns = 100
		transport.IdleConnTimeout = 90 * time.Second
		transport.TLSHandshakeTimeout = 10 * time.Second
		transport.ExpectContinueTimeout = 3 * time.Second
	}
	transport.TLSClientConfig = tlsClientConfig
	return transport, nil
}

// CloneHTTPDefaultTransport returns a clone of http.DefaultTransport.
func CloneHTTPDefaultTransport() *http.Transport {
	return http.DefaultTransport.(*http.Transport).Clone()
}
