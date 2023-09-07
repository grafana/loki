// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/httpgrpc/server/server.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/go-kit/log/level"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/sercand/kuberesolver/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/log"
	"github.com/grafana/dskit/middleware"
)

// Server implements HTTPServer.  HTTPServer is a generated interface that gRPC
// servers must implement.
type Server struct {
	handler http.Handler
}

// NewServer makes a new Server.
func NewServer(handler http.Handler) *Server {
	return &Server{
		handler: handler,
	}
}

type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }

// BytesBuffer returns the underlaying `bytes.buffer` used to build this io.ReadCloser.
func (n nopCloser) BytesBuffer() *bytes.Buffer { return n.Buffer }

// Handle implements HTTPServer.
func (s Server) Handle(ctx context.Context, r *httpgrpc.DHTTPRequest) (*httpgrpc.DHTTPResponse, error) {
	req, err := http.NewRequest(r.Method, r.Url, nopCloser{Buffer: bytes.NewBuffer(r.Body)})
	if err != nil {
		return nil, err
	}
	toHeader(r.Headers, req.Header)
	req = req.WithContext(ctx)
	req.RequestURI = r.Url
	req.ContentLength = int64(len(r.Body))

	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, req)
	resp := &httpgrpc.DHTTPResponse{
		Code:    int32(recorder.Code),
		Headers: fromHeader(recorder.Header()),
		Body:    recorder.Body.Bytes(),
	}
	if recorder.Code/100 == 5 {
		return nil, httpgrpc.ErrorFromHTTPResponse(resp)
	}
	return resp, nil
}

// Client is a http.Handler that forwards the request over gRPC.
type Client struct {
	client httpgrpc.DHTTPClient
	conn   *grpc.ClientConn
}

// ParseURL deals with direct:// style URLs, as well as kubernetes:// urls.
// For backwards compatibility it treats URLs without schems as kubernetes://.
func ParseURL(unparsed string) (string, error) {
	// if it has :///, this is the kuberesolver v2 URL. Return it as it is.
	if strings.Contains(unparsed, ":///") {
		return unparsed, nil
	}

	parsed, err := url.Parse(unparsed)
	if err != nil {
		return "", err
	}

	scheme, host := parsed.Scheme, parsed.Host
	if !strings.Contains(unparsed, "://") {
		scheme, host = "kubernetes", unparsed
	}

	switch scheme {
	case "direct":
		return host, err

	case "kubernetes":
		host, port, err := net.SplitHostPort(host)
		if err != nil {
			return "", err
		}
		parts := strings.SplitN(host, ".", 3)
		service, domain := parts[0], ""
		if len(parts) > 1 {
			namespace := parts[1]
			domain = "." + namespace
		}
		if len(parts) > 2 {
			domain = domain + "." + parts[2]
		}
		address := fmt.Sprintf("kubernetes:///%s%s:%s", service, domain, port)
		return address, nil

	default:
		return "", fmt.Errorf("unrecognised scheme: %s", parsed.Scheme)
	}
}

// NewClient makes a new Client, given a kubernetes service address.
func NewClient(address string) (*Client, error) {
	kuberesolver.RegisterInCluster()

	address, err := ParseURL(address)
	if err != nil {
		return nil, err
	}
	const grpcServiceConfig = `{"loadBalancingPolicy":"round_robin"}`

	dialOptions := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(grpcServiceConfig),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
		),
	}

	conn, err := grpc.Dial(address, dialOptions...)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: httpgrpc.NewDHTTPClient(conn),
		conn:   conn,
	}, nil
}

// HTTPRequest wraps an ordinary HTTPRequest with a gRPC one
func HTTPRequest(r *http.Request) (*httpgrpc.DHTTPRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return &httpgrpc.DHTTPRequest{
		Method:  r.Method,
		Url:     r.RequestURI,
		Body:    body,
		Headers: fromHeader(r.Header),
	}, nil
}

// WriteResponse converts an httpgrpc response to an HTTP one
func WriteResponse(w http.ResponseWriter, resp *httpgrpc.DHTTPResponse) error {
	toHeader(resp.Headers, w.Header())
	w.WriteHeader(int(resp.Code))
	_, err := w.Write(resp.Body)
	return err
}

// WriteError converts an httpgrpc error to an HTTP one
func WriteError(w http.ResponseWriter, err error) {
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		_ = WriteResponse(w, resp)
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ServeHTTP implements http.Handler
func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tracer := opentracing.GlobalTracer(); tracer != nil {
		if span := opentracing.SpanFromContext(r.Context()); span != nil {
			if err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header)); err != nil {
				level.Warn(log.Global()).Log("msg", "failed to inject tracing headers into request", "err", err)
			}
		}
	}

	req, err := HTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := c.client.Handle(r.Context(), req)
	if err != nil {
		// Some errors will actually contain a valid resp, just need to unpack it
		var ok bool
		resp, ok = httpgrpc.HTTPResponseFromError(err)

		if !ok {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := WriteResponse(w, resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func toHeader(hs []*httpgrpc.DHeader, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}

func fromHeader(hs http.Header) []*httpgrpc.DHeader {
	result := make([]*httpgrpc.DHeader, 0, len(hs))
	for k, vs := range hs {
		result = append(result, &httpgrpc.DHeader{
			Key:    k,
			Values: vs,
		})
	}
	return result
}
