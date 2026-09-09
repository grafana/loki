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
func serviceFabricCertificateVerifiedHTTPClient(httpClient interface {
	Do(*http.Request) (*http.Response, error)
	CloseIdleConnections()
}) (*http.Client, string, error) {
	endpoint, err := serviceFabricEndpoint()
	if err != nil {
		return nil, "", err
	}
	pin, err := serviceFabricThumbprint(os.Getenv(identityServerThumbprintEnvVar))
	if err != nil {
		return nil, "", err
	}

	callerClient, ok := httpClient.(*http.Client)
	if !ok {
		return nil, "", errors.New("Service Fabric managed identity requires a standard *http.Client")
	}
	derivedClient := *callerClient

	var callerTransport *http.Transport
	if callerClient.Transport == nil {
		var ok bool
		callerTransport, ok = http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, "", errors.New("Service Fabric managed identity requires a standard *http.Transport")
		}
	} else {
		var ok bool
		callerTransport, ok = callerClient.Transport.(*http.Transport)
		if !ok {
			return nil, "", errors.New("Service Fabric managed identity requires a standard *http.Transport")
		}
	}
	//nolint:staticcheck // DialTLS must be rejected because it bypasses TLSClientConfig.
	if callerTransport.DialTLS != nil || callerTransport.DialTLSContext != nil {
		return nil, "", errors.New("Service Fabric managed identity does not support a transport with custom TLS dialing")
	}
	if callerTransport.TLSClientConfig != nil &&
		(callerTransport.TLSClientConfig.VerifyPeerCertificate != nil || callerTransport.TLSClientConfig.VerifyConnection != nil) {
		return nil, "", errors.New("Service Fabric managed identity does not support custom TLS verification")
	}
	if callerTransport.TLSNextProto != nil {
		return nil, "", errors.New("Service Fabric managed identity does not support custom TLS protocol handlers")
	}
	derivedTransport := callerTransport.Clone()
	tlsConfig := derivedTransport.TLSClientConfig.Clone()
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	}
	tlsConfig.InsecureSkipVerify = true // #nosec G402 -- VerifyConnection below pins the Service Fabric self-signed certificate.
	tlsConfig.VerifyConnection = func(connectionState tls.ConnectionState) error {
		if len(connectionState.PeerCertificates) == 0 {
			return errors.New("Service Fabric TLS connection did not provide a certificate")
		}
		if subtle.ConstantTimeCompare(serviceFabricCertificateThumbprint(connectionState.PeerCertificates[0]), pin) != 1 {
			return errors.New("Service Fabric TLS certificate thumbprint did not match IDENTITY_SERVER_THUMBPRINT")
		}
		return nil
	}
	derivedTransport.TLSClientConfig = tlsConfig
	derivedClient.Transport = derivedTransport
	derivedClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Service Fabric managed identity redirects are not permitted")
	}
	return &derivedClient, endpoint, nil
}

func serviceFabricEndpoint() (string, error) {
	endpoint := os.Getenv(identityEndpointEnvVar)
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if request.URL.Scheme != "https" || request.URL.Host == "" {
		return "", errors.New("Service Fabric managed identity endpoint must use HTTPS")
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
