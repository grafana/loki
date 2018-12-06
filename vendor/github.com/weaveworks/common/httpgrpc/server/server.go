package server

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/sercand/kuberesolver"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/roundrobin"

	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/middleware"
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

// Handle implements HTTPServer.
func (s Server) Handle(ctx context.Context, r *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error) {
	req, err := http.NewRequest(r.Method, r.Url, ioutil.NopCloser(bytes.NewReader(r.Body)))
	if err != nil {
		return nil, err
	}
	toHeader(r.Headers, req.Header)
	req = req.WithContext(ctx)
	req.RequestURI = r.Url

	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, req)
	resp := &httpgrpc.HTTPResponse{
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
	mtx       sync.RWMutex
	service   string
	namespace string
	port      string
	client    httpgrpc.HTTPClient
	conn      *grpc.ClientConn
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
		service, namespace, domain := parts[0], "default", ""
		if len(parts) > 1 {
			namespace = parts[1]
			domain = "." + namespace
		}
		if len(parts) > 2 {
			domain = domain + "." + parts[2]
		}
		address := fmt.Sprintf("kubernetes://%s%s:%s", service, domain, port)
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

	dialOptions := []grpc.DialOption{
		grpc.WithBalancerName(roundrobin.Name),
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
		)),
	}

	conn, err := grpc.Dial(address, dialOptions...)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: httpgrpc.NewHTTPClient(conn),
		conn:   conn,
	}, nil
}

// HTTPRequest wraps an ordinary HTTPRequest with a gRPC one
func HTTPRequest(r *http.Request) (*httpgrpc.HTTPRequest, error) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return &httpgrpc.HTTPRequest{
		Method:  r.Method,
		Url:     r.RequestURI,
		Body:    body,
		Headers: fromHeader(r.Header),
	}, nil
}

// WriteResponse converts an httpgrpc response to an HTTP one
func WriteResponse(w http.ResponseWriter, resp *httpgrpc.HTTPResponse) error {
	toHeader(resp.Headers, w.Header())
	w.WriteHeader(int(resp.Code))
	_, err := w.Write(resp.Body)
	return err
}

// WriteError converts an httpgrpc error to an HTTP one
func WriteError(w http.ResponseWriter, err error) {
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		WriteResponse(w, resp)
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ServeHTTP implements http.Handler
func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tracer := opentracing.GlobalTracer(); tracer != nil {
		if span := opentracing.SpanFromContext(r.Context()); span != nil {
			if err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header)); err != nil {
				logging.Global().Warnf("Failed to inject tracing headers into request: %v", err)
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

func toHeader(hs []*httpgrpc.Header, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}

func fromHeader(hs http.Header) []*httpgrpc.Header {
	result := make([]*httpgrpc.Header, 0, len(hs))
	for k, vs := range hs {
		result = append(result, &httpgrpc.Header{
			Key:    k,
			Values: vs,
		})
	}
	return result
}
