package frontend

import (
	"net/http"
	"net/url"
	"path"

	"github.com/opentracing/opentracing-go"
)

// RoundTripper that forwards requests to downstream URL.
type downstreamRoundTripper struct {
	downstreamURL *url.URL
}

func NewDownstreamRoundTripper(downstreamURL string) (http.RoundTripper, error) {
	u, err := url.Parse(downstreamURL)
	if err != nil {
		return nil, err
	}

	return &downstreamRoundTripper{downstreamURL: u}, nil
}

func (d downstreamRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(r.Context())
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
	return http.DefaultTransport.RoundTrip(r)
}
