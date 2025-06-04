package frontend

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/grafana/dskit/user"
	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// RoundTripper that forwards requests to downstream URL.
type downstreamRoundTripper struct {
	downstreamURL *url.URL
	transport     http.RoundTripper
	codec         queryrangebase.Codec
}

func NewDownstreamRoundTripper(downstreamURL string, transport http.RoundTripper, codec queryrangebase.Codec) (queryrangebase.Handler, error) {
	u, err := url.Parse(downstreamURL)
	if err != nil {
		return nil, err
	}

	return &downstreamRoundTripper{downstreamURL: u, transport: transport, codec: codec}, nil
}

func (d downstreamRoundTripper) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	var r *http.Request

	r, err := d.codec.EncodeRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("connot convert request ot HTTP request: %w", err)
	}
	if err := user.InjectOrgIDIntoHTTPRequest(ctx, r); err != nil {
		return nil, err
	}

	otelhttptrace.Inject(ctx, r)

	r.URL.Scheme = d.downstreamURL.Scheme
	r.URL.Host = d.downstreamURL.Host
	r.URL.Path = path.Join(d.downstreamURL.Path, r.URL.Path)
	r.Host = ""

	httpResp, err := d.transport.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	resp, err := d.codec.DecodeResponse(ctx, httpResp, req)
	if err != nil {
		return nil, fmt.Errorf("cannot convert HTTP response to response: %w", err)
	}

	return resp, nil
}
