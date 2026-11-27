// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package customtokenproxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

const (
	AzureKubernetesCAData  = "AZURE_KUBERNETES_CA_DATA"
	AzureKubernetesCAFile  = "AZURE_KUBERNETES_CA_FILE"
	AzureKubernetesSNIName = "AZURE_KUBERNETES_SNI_NAME"

	AzureKubernetesTokenProxy = "AZURE_KUBERNETES_TOKEN_PROXY"
)

func parseAndValidate(endpoint string) (*url.URL, error) {
	tokenProxy, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse custom token proxy URL %q: %s", endpoint, err)
	}
	if tokenProxy.Scheme != "https" {
		return nil, fmt.Errorf("custom token endpoint must use https scheme, got %q", tokenProxy.Scheme)
	}
	if tokenProxy.User != nil {
		return nil, fmt.Errorf("custom token endpoint URL %q must not contain user info", tokenProxy)
	}
	if tokenProxy.RawQuery != "" {
		return nil, fmt.Errorf("custom token endpoint URL %q must not contain a query", tokenProxy)
	}
	if tokenProxy.EscapedFragment() != "" {
		return nil, fmt.Errorf("custom token endpoint URL %q must not contain a fragment", tokenProxy)
	}
	if tokenProxy.EscapedPath() == "" {
		// if the path is empty, set it to "/" to avoid stripping the path from req.URL
		tokenProxy.Path = "/"
	}
	return tokenProxy, nil
}

var (
	errCustomEndpointEnvSetWithoutTokenProxy = errors.New(
		"AZURE_KUBERNETES_TOKEN_PROXY is not set but other custom endpoint-related environment variables are present",
	)
	errCustomEndpointMultipleCASourcesSet = errors.New(
		"only one of AZURE_KUBERNETES_CA_FILE and AZURE_KUBERNETES_CA_DATA can be specified",
	)
)

func createTransport(sniName string, caPool *x509.CertPool) *http.Transport {
	var transport *http.Transport
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = tr.Clone()
	} else {
		// this should not happen, but if the user mutates the net/http.DefaultTransport
		// to something else, we fall back to a sane default
		transport = &http.Transport{
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}

	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.ServerName = sniName
	transport.TLSClientConfig.RootCAs = caPool

	return transport
}

// Configure configures custom token endpoint mode if the required environment variables are present.
func Configure(clientOptions *policy.ClientOptions) error {
	kubernetesTokenProxyStr := os.Getenv(AzureKubernetesTokenProxy)

	kubernetesSNIName := os.Getenv(AzureKubernetesSNIName)
	kubernetesCAFile := os.Getenv(AzureKubernetesCAFile)
	kubernetesCAData := os.Getenv(AzureKubernetesCAData)

	if kubernetesTokenProxyStr == "" {
		// custom token proxy is not set, while other Kubernetes-related environment variables are present,
		// this is likely a configuration issue so erroring out to avoid misconfiguration
		if kubernetesSNIName != "" || kubernetesCAFile != "" || kubernetesCAData != "" {
			return errCustomEndpointEnvSetWithoutTokenProxy
		}

		return nil
	}
	tokenProxy, err := parseAndValidate(kubernetesTokenProxyStr)
	if err != nil {
		return err
	}

	// CAFile and CAData are mutually exclusive, at most one can be set.
	// If none of CAFile or CAData are set, the default system CA pool will be used.
	if kubernetesCAFile != "" && kubernetesCAData != "" {
		return errCustomEndpointMultipleCASourcesSet
	}

	// preload the transport
	t := &transport{
		caFile:     kubernetesCAFile,
		caData:     []byte(kubernetesCAData),
		sniName:    kubernetesSNIName,
		tokenProxy: tokenProxy,
	}
	if _, err := t.getTokenTransporter(); err != nil {
		return err
	}

	clientOptions.Transport = t
	return nil
}

// transport redirects requests to the configured proxy.
//
// Lock is not needed for internal caData as this transport is called under confidentialClient's lock.
type transport struct {
	caFile     string
	caData     []byte
	sniName    string
	tokenProxy *url.URL
	transport  *http.Transport
}

func (t *transport) Do(req *http.Request) (*http.Response, error) {
	tr, err := t.getTokenTransporter()
	if err != nil {
		return nil, err
	}

	rewriteProxyRequestURL(req, t.tokenProxy)

	resp, err := tr.RoundTrip(req)
	if err == nil && resp == nil {
		// transports must handle this rare case.
		// Returning an error makes the retry policy try the request again
		err = errors.New("received nil response")
	}
	return resp, err
}

// getTokenTransporter provides the token transport to use for the request.
//
// There are a few scenarios need to be handled:
//  1. no CA overrides, use default transport. The transport is fixed after set.
//  2. CA data override provided, use a transport with custom CA pool.
//     This transport is fixed after set.
//  3. CA file override is provided, use a transport with custom CA pool.
//     This transport needs to be recreated if the CA file content changes.
func (t *transport) getTokenTransporter() (*http.Transport, error) {
	if len(t.caData) == 0 && t.caFile == "" {
		// no custom CA overrides
		if t.transport == nil {
			t.transport = createTransport(t.sniName, nil)
		}
		return t.transport, nil
	}

	if t.caFile == "" {
		// host provided CA bytes in AZURE_KUBERNETES_CA_DATA and can't change
		// them now, so we need to create a client only if we haven't done so yet
		if t.transport != nil {
			return t.transport, nil
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM([]byte(t.caData)) {
			return nil, fmt.Errorf("parse CA data: no valid certificates found")
		}

		t.transport = createTransport(t.sniName, caPool)
		return t.transport, nil
	}

	// host provided the CA bytes in a file whose contents it can change,
	// so we must read that file and maybe create a new client
	b, err := os.ReadFile(t.caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %q: %s", t.caFile, err)
	}
	if len(b) == 0 {
		// this can happen during the middle of CA rotation on the host.
		if t.transport == nil {
			// if the transport was never created, error out here to force retrying the call later
			return nil, fmt.Errorf("CA file %q is empty", t.caFile)
		}
		// if the transport was already created, just keep using it
		return t.transport, nil
	}
	if !bytes.Equal(b, t.caData) {
		// CA has changed, rebuild the transport with new CA pool
		// invariant: t.transport is nil when t.caData is nil (initial call)
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM([]byte(b)) {
			return nil, fmt.Errorf("parse CA file %q: no valid certificates found", t.caFile)
		}
		if t.transport != nil {
			t.transport.CloseIdleConnections()
		}
		t.transport = createTransport(t.sniName, caPool)
		t.caData = b
	}

	return t.transport, nil
}

// rewriteProxyRequestURL updates the request URL to target the specified URL.
// Target is the token proxy URL in custom token endpoint mode.
//
// proxyURL should be parsed and validated by parseAndValidate before calling.
func rewriteProxyRequestURL(req *http.Request, proxyURL *url.URL) {
	reqRawQuery := req.URL.RawQuery
	// preserve the original path and append it to the proxy URL's path.
	// proxyURL path is guaranteed to be non-empty.
	req.URL = proxyURL.JoinPath(req.URL.EscapedPath())
	// NOTE: proxyURL doesn't include query, req might include query
	// we just retain the raw query from req.URL
	req.URL.RawQuery = reqRawQuery
}
