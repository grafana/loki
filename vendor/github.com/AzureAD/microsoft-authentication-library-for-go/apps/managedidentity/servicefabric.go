// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package managedidentity

import (
	"context"

	/* #nosec */
	"crypto/sha1"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"unicode"
)

// serviceFabricCertificateVerifiedHTTPClient derives a client with Service Fabric's required certificate pinning.
// Only standard clients and transports can be safely cloned and augmented without changing the
// caller's behavior for other requests.
func serviceFabricCertificateVerifiedHTTPClient(httpClient *http.Client) (*http.Client, error) {
	if httpClient == nil {
		return nil, errors.New("managed identity on Service Fabric requires a non-nil *http.Client")
	}
	pin, err := serviceFabricThumbprint(os.Getenv(identityServerThumbprintEnvVar))
	if err != nil {
		return nil, err
	}

	derivedClient := *httpClient

	var callerTransport *http.Transport
	if httpClient.Transport == nil {
		var ok bool
		callerTransport, ok = http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("managed identity on Service Fabric requires a standard *http.Transport")
		}
	} else {
		var ok bool
		callerTransport, ok = httpClient.Transport.(*http.Transport)
		if !ok {
			return nil, errors.New("managed identity on Service Fabric requires a standard *http.Transport")
		}
	}
	//nolint:staticcheck // DialTLS must be rejected because it bypasses TLSClientConfig.
	if callerTransport.DialTLS != nil || callerTransport.DialTLSContext != nil {
		return nil, errors.New("managed identity on Service Fabric does not support a transport with custom TLS dialing")
	}
	if callerTransport.TLSClientConfig != nil &&
		(callerTransport.TLSClientConfig.VerifyPeerCertificate != nil || callerTransport.TLSClientConfig.VerifyConnection != nil) {
		return nil, errors.New("managed identity on Service Fabric does not support custom TLS verification")
	}
	derivedTransport := callerTransport.Clone()
	// Give the derived transport its own HTTP/2 state so it can never serve a Service Fabric request over a
	// connection from the caller's pool. golang.org/x/net/http2.ConfigureTransports (used by azure-sdk-for-go's
	// default transport) installs an "h2" callback that retains the original connection pool, and Transport.Clone
	// copies that callback by reference. Without clearing it, a pre-existing, unpinned connection to the same
	// authority could be reused, bypassing the certificate pin below. Resetting TLSNextProto makes the standard
	// library lazily re-establish HTTP/2 with a fresh pool governed by this transport's pinned TLS configuration.
	derivedTransport.TLSNextProto = nil
	tlsConfig := derivedTransport.TLSClientConfig.Clone()
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	}
	tlsConfig.InsecureSkipVerify = true // #nosec G402 -- VerifyConnection below pins the Service Fabric self-signed certificate.
	tlsConfig.VerifyConnection = func(connectionState tls.ConnectionState) error {
		if len(connectionState.PeerCertificates) == 0 {
			return errors.New("TLS connection to Service Fabric did not provide a certificate")
		}
		if subtle.ConstantTimeCompare(serviceFabricCertificateThumbprint(connectionState.PeerCertificates[0]), pin) != 1 {
			return errors.New("TLS certificate thumbprint from Service Fabric did not match IDENTITY_SERVER_THUMBPRINT")
		}
		return nil
	}
	derivedTransport.TLSClientConfig = tlsConfig
	derivedClient.Transport = derivedTransport
	derivedClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not permitted for managed identity on Service Fabric")
	}
	return &derivedClient, nil
}

func serviceFabricEndpoint() (string, error) {
	endpoint := os.Getenv(identityEndpointEnvVar)
	// #nosec G704 -- the URL is restricted to HTTPS and the caller pins its certificate thumbprint.
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if request.URL.Scheme != "https" || request.URL.Host == "" {
		return "", errors.New("managed identity endpoint for Service Fabric must use HTTPS")
	}
	return request.URL.String(), nil
}

func serviceFabricThumbprint(value string) ([]byte, error) {
	normalized := strings.Map(func(r rune) rune {
		if r == ':' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	if len(normalized) != 40 {
		return nil, errors.New("IDENTITY_SERVER_THUMBPRINT must be a SHA-1 certificate thumbprint")
	}
	thumbprint, err := hex.DecodeString(normalized)
	if err != nil || len(thumbprint) != 20 {
		return nil, errors.New("IDENTITY_SERVER_THUMBPRINT must be a SHA-1 certificate thumbprint")
	}
	return thumbprint, nil
}

func serviceFabricCertificateThumbprint(certificate *x509.Certificate) []byte {
	// Service Fabric exposes SHA-1 certificate thumbprints through IDENTITY_SERVER_THUMBPRINT.
	thumbprint := sha1.Sum(certificate.Raw) /* #nosec G401 -- Service Fabric publishes SHA-1 thumbprints. */ // NOSONAR -- Service Fabric defines IDENTITY_SERVER_THUMBPRINT as the SHA-1 hash of its self-signed endpoint certificate; this compares that platform-defined identifier, not a security digest.
	return thumbprint[:]
}

func createServiceFabricAuthRequest(ctx context.Context, endpoint, resource string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Secret", os.Getenv(identityHeaderEnvVar))
	q := req.URL.Query()
	q.Set("api-version", serviceFabricAPIVersion)
	q.Set("resource", resource)
	req.URL.RawQuery = q.Encode()
	return req, nil
}
