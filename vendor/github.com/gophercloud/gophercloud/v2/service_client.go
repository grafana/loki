package gophercloud

import (
	"context"
	"io"
	"net/http"
	"strings"
)

// ServiceClient stores details required to interact with a specific service API implemented by a provider.
// Generally, you'll acquire these by calling the appropriate `New` method on a ProviderClient.
type ServiceClient struct {
	// ProviderClient is a reference to the provider that implements this service.
	*ProviderClient

	// Endpoint is the base URL of the service's API, acquired from a service catalog.
	// It MUST end with a /.
	Endpoint string

	// ResourceBase is the base URL shared by the resources within a service's API. It should include
	// the API version and, like Endpoint, MUST end with a / if set. If not set, the Endpoint is used
	// as-is, instead.
	ResourceBase string

	// This is the service client type (e.g. compute, sharev2).
	// NOTE: FOR INTERNAL USE ONLY. DO NOT SET. GOPHERCLOUD WILL SET THIS.
	// It is only exported because it gets set in a different package.
	Type string

	// The microversion of the service to use. Set this to use a particular microversion.
	Microversion string

	// MoreHeaders allows users (or Gophercloud) to set service-wide headers on requests. Put another way,
	// values set in this field will be set on all the HTTP requests the service client sends.
	MoreHeaders map[string]string
}

// ResourceBaseURL returns the base URL of any resources used by this service. It MUST end with a /.
func (client *ServiceClient) ResourceBaseURL() string {
	if client.ResourceBase != "" {
		return client.ResourceBase
	}
	return client.Endpoint
}

// ServiceURL constructs a URL for a resource belonging to this provider.
func (client *ServiceClient) ServiceURL(parts ...string) string {
	return client.ResourceBaseURL() + strings.Join(parts, "/")
}

func (client *ServiceClient) initReqOpts(JSONBody any, JSONResponse any, opts *RequestOpts) {
	if v, ok := (JSONBody).(io.Reader); ok {
		opts.RawBody = v
	} else if JSONBody != nil {
		opts.JSONBody = JSONBody
	}

	if JSONResponse != nil {
		opts.JSONResponse = JSONResponse
	}
}

// Get calls `Request` with the "GET" HTTP verb.
func (client *ServiceClient) Get(ctx context.Context, url string, JSONResponse any, opts *RequestOpts) (*http.Response, error) {
	if opts == nil {
		opts = new(RequestOpts)
	}
	client.initReqOpts(nil, JSONResponse, opts)
	return client.Request(ctx, "GET", url, opts)
}

// Post calls `Request` with the "POST" HTTP verb.
func (client *ServiceClient) Post(ctx context.Context, url string, JSONBody any, JSONResponse any, opts *RequestOpts) (*http.Response, error) {
	if opts == nil {
		opts = new(RequestOpts)
	}
	client.initReqOpts(JSONBody, JSONResponse, opts)
	return client.Request(ctx, "POST", url, opts)
}

// Put calls `Request` with the "PUT" HTTP verb.
func (client *ServiceClient) Put(ctx context.Context, url string, JSONBody any, JSONResponse any, opts *RequestOpts) (*http.Response, error) {
	if opts == nil {
		opts = new(RequestOpts)
	}
	client.initReqOpts(JSONBody, JSONResponse, opts)
	return client.Request(ctx, "PUT", url, opts)
}

// Patch calls `Request` with the "PATCH" HTTP verb.
func (client *ServiceClient) Patch(ctx context.Context, url string, JSONBody any, JSONResponse any, opts *RequestOpts) (*http.Response, error) {
	if opts == nil {
		opts = new(RequestOpts)
	}
	client.initReqOpts(JSONBody, JSONResponse, opts)
	return client.Request(ctx, "PATCH", url, opts)
}

// Delete calls `Request` with the "DELETE" HTTP verb.
func (client *ServiceClient) Delete(ctx context.Context, url string, opts *RequestOpts) (*http.Response, error) {
	if opts == nil {
		opts = new(RequestOpts)
	}
	client.initReqOpts(nil, nil, opts)
	return client.Request(ctx, "DELETE", url, opts)
}

// Head calls `Request` with the "HEAD" HTTP verb.
func (client *ServiceClient) Head(ctx context.Context, url string, opts *RequestOpts) (*http.Response, error) {
	if opts == nil {
		opts = new(RequestOpts)
	}
	client.initReqOpts(nil, nil, opts)
	return client.Request(ctx, "HEAD", url, opts)
}

func (client *ServiceClient) setMicroversionHeader(opts *RequestOpts) {
	serviceType := client.Type

	switch client.Type {
	case "compute":
		opts.MoreHeaders["X-OpenStack-Nova-API-Version"] = client.Microversion
	case "shared-file-system", "sharev2", "share":
		opts.MoreHeaders["X-OpenStack-Manila-API-Version"] = client.Microversion
	case "block-storage", "block-store", "volume", "volumev3":
		opts.MoreHeaders["X-OpenStack-Volume-API-Version"] = client.Microversion
		// cinder should accept block-storage but (as of Dalmatian) does not
		serviceType = "volume"
	case "baremetal":
		opts.MoreHeaders["X-OpenStack-Ironic-API-Version"] = client.Microversion
	case "baremetal-introspection":
		opts.MoreHeaders["X-OpenStack-Ironic-Inspector-API-Version"] = client.Microversion
	}

	if client.Type != "" {
		opts.MoreHeaders["OpenStack-API-Version"] = serviceType + " " + client.Microversion
	}
}

// Request carries out the HTTP operation for the service client
func (client *ServiceClient) Request(ctx context.Context, method, url string, options *RequestOpts) (*http.Response, error) {
	if options.MoreHeaders == nil {
		options.MoreHeaders = make(map[string]string)
	}

	if client.Microversion != "" {
		client.setMicroversionHeader(options)
	}

	if len(client.MoreHeaders) > 0 {
		if options == nil {
			options = new(RequestOpts)
		}

		for k, v := range client.MoreHeaders {
			options.MoreHeaders[k] = v
		}
	}
	return client.ProviderClient.Request(ctx, method, url, options)
}

// ParseResponse is a helper function to parse http.Response to constituents.
func ParseResponse(resp *http.Response, err error) (io.ReadCloser, http.Header, error) {
	if resp != nil {
		return resp.Body, resp.Header, err
	}
	return nil, nil, err
}
