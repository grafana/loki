package frontend

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/opentracing/opentracing-go"
)

// RoundTripper that forwards requests to downstream URL.
type downstreamRoundTripper struct {
	downstreamURL *url.URL
	transport     http.RoundTripper
}

func NewDownstreamRoundTripper(downstreamURL string, transport http.RoundTripper) (queryrangebase.Handler, error) {
	u, err := url.Parse(downstreamURL)
	if err != nil {
		return nil, err
	}

	return &downstreamRoundTripper{downstreamURL: u, transport: transport}, nil
}

func (d downstreamRoundTripper) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	// TODO: r := EncodeRequest(req)
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(ctx)
	var r *http.Request
	if tracer != nil && span != nil {
		carrier := opentracing.HTTPHeadersCarrier(r.Header)
		err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, carrier)
		if err != nil {
			return nil, err
		}
	}

	r.URL.Scheme = d.downstreamURL.Scheme
	r.URL.Host = d.downstreamURL.Host
	r.URL.Path = path.Join(d.downstreamURL.Path, r.URL.Path)
	r.Host = ""
	d.transport.RoundTrip(r)
	// TODO
	return nil, fmt.Errorf("no implemented")
}
