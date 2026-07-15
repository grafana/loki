package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/alibabacloud-go/debug/debug"
	credentials_go "github.com/aliyun/credentials-go"
	"github.com/aliyun/credentials-go/credentials/internal/utils"
)

var defaultUserAgent = fmt.Sprintf("AlibabaCloud (%s; %s) Golang/%s Credentials/%s TeaDSL/1", runtime.GOOS, runtime.GOARCH, strings.Trim(runtime.Version(), "go"), credentials_go.PACKAGE_VERSION)

type Request struct {
	Method         string // http request method
	URL            string // http url
	Protocol       string // http or https
	Host           string // http host
	ReadTimeout    time.Duration
	ConnectTimeout time.Duration
	Proxy          string            // http proxy
	Form           map[string]string // http form
	Body           []byte            // request body for JSON or stream
	Path           string
	Queries        map[string]string
	Headers        map[string]string
}

func (req *Request) BuildRequestURL() string {
	httpUrl := fmt.Sprintf("%s://%s%s", req.Protocol, req.Host, req.Path)
	if req.URL != "" {
		httpUrl = req.URL
	}

	querystring := utils.GetURLFormedMap(req.Queries)
	if querystring != "" {
		httpUrl = httpUrl + "?" + querystring
	}

	return fmt.Sprintf("%s %s", req.Method, httpUrl)
}

type Response struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

var newRequest = http.NewRequest

type do func(req *http.Request) (*http.Response, error)

var hookDo = func(fn do) do {
	return fn
}

var debuglog = debug.Init("credential")

func Do(req *Request) (res *Response, err error) {
	querystring := utils.GetURLFormedMap(req.Queries)
	// do request
	httpUrl := fmt.Sprintf("%s://%s%s?%s", req.Protocol, req.Host, req.Path, querystring)
	if req.URL != "" {
		httpUrl = req.URL
	}

	var body io.Reader
	if req.Method == "GET" {
		body = strings.NewReader("")
	} else if req.Body != nil {
		body = bytes.NewReader(req.Body)
	} else {
		body = strings.NewReader(utils.GetURLFormedMap(req.Form))
	}

	httpRequest, err := newRequest(req.Method, httpUrl, body)
	if err != nil {
		return
	}

	httpRequest.Header["User-Agent"] = []string{defaultUserAgent}

	if req.Form != nil {
		httpRequest.Header["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	}

	for key, value := range req.Headers {
		if value != "" {
			debuglog("> %s: %s", key, value)
			httpRequest.Header.Set(key, value)
		}
	}

	httpClient := &http.Client{}

	if req.ReadTimeout != 0 {
		httpClient.Timeout = req.ReadTimeout + req.ConnectTimeout
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if req.Proxy != "" {
		var proxy *url.URL
		proxy, err = url.Parse(req.Proxy)
		if err != nil {
			return
		}
		transport.Proxy = http.ProxyURL(proxy)
	}

	if req.ConnectTimeout != 0 {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{
				Timeout:   req.ConnectTimeout,
				DualStack: true,
			}).DialContext(ctx, network, address)
		}
	}

	httpClient.Transport = transport

	httpResponse, err := hookDo(httpClient.Do)(httpRequest)
	if err != nil {
		return
	}

	defer httpResponse.Body.Close()

	responseBody, err := ioutil.ReadAll(httpResponse.Body)
	if err != nil {
		return
	}
	res = &Response{
		StatusCode: httpResponse.StatusCode,
		Headers:    make(map[string]string),
		Body:       responseBody,
	}
	for key, v := range httpResponse.Header {
		res.Headers[key] = v[0]
	}

	return
}
