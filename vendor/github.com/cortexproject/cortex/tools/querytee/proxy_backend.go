package querytee

import (
	"context"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/pkg/errors"
)

const orgIDHeader = "X-Scope-OrgId"

// ProxyBackend holds the information of a single backend.
type ProxyBackend struct {
	name     string
	endpoint *url.URL
	client   *http.Client
	timeout  time.Duration

	// Whether this is the preferred backend from which picking up
	// the response and sending it back to the client.
	preferred bool
}

// NewProxyBackend makes a new ProxyBackend
func NewProxyBackend(name string, endpoint *url.URL, timeout time.Duration, preferred bool) *ProxyBackend {
	return &ProxyBackend{
		name:      name,
		endpoint:  endpoint,
		timeout:   timeout,
		preferred: preferred,
		client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("the query-tee proxy does not follow redirects")
			},
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100, // see https://github.com/golang/go/issues/13801
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (b *ProxyBackend) ForwardRequest(orig *http.Request) (int, []byte, error) {
	req, err := b.createBackendRequest(orig)
	if err != nil {
		return 0, nil, err
	}

	return b.doBackendRequest(req)
}

func (b *ProxyBackend) createBackendRequest(orig *http.Request) (*http.Request, error) {
	req, err := http.NewRequest(orig.Method, orig.URL.String(), nil)
	if err != nil {
		return nil, err
	}

	// Replace the endpoint with the backend one.
	req.URL.Scheme = b.endpoint.Scheme
	req.URL.Host = b.endpoint.Host

	// Prepend the endpoint path to the request path.
	req.URL.Path = path.Join(b.endpoint.Path, req.URL.Path)

	// Replace the auth:
	// - If the endpoint has user and password, use it.
	// - If the endpoint has user only, keep it and use the request password (if any).
	// - If the endpoint has no user and no password, use the request auth (if any).
	clientUser, clientPass, clientAuth := orig.BasicAuth()
	endpointUser := b.endpoint.User.Username()
	endpointPass, _ := b.endpoint.User.Password()

	if endpointUser != "" && endpointPass != "" {
		req.SetBasicAuth(endpointUser, endpointPass)
	} else if endpointUser != "" {
		req.SetBasicAuth(endpointUser, clientPass)
	} else if clientAuth {
		req.SetBasicAuth(clientUser, clientPass)
	}

	// If there is X-Scope-OrgId header in the request, forward it. This is done even if there was username/password.
	if orgID := orig.Header.Get(orgIDHeader); orgID != "" {
		req.Header.Set(orgIDHeader, orgID)
	}

	return req, nil
}

func (b *ProxyBackend) doBackendRequest(req *http.Request) (int, []byte, error) {
	// Honor the read timeout.
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()

	// Execute the request.
	res, err := b.client.Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, errors.Wrap(err, "executing backend request")
	}

	// Read the entire response body.
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return 0, nil, errors.Wrap(err, "reading backend response")
	}

	return res.StatusCode, body, nil
}
