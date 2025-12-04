// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

package obs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
)

type urlHolder struct {
	scheme string
	host   string
	port   int
}

type config struct {
	securityProviders    []securityProvider
	urlHolder            *urlHolder
	pathStyle            bool
	cname                bool
	sslVerify            bool
	disableKeepAlive     bool
	endpoint             string
	signature            SignatureType
	region               string
	connectTimeout       int
	socketTimeout        int
	headerTimeout        int
	idleConnTimeout      int
	finalTimeout         int
	maxRetryCount        int
	proxyURL             string
	noProxyURL           string
	proxyFromEnv         bool
	maxConnsPerHost      int
	pemCerts             []byte
	transport            *http.Transport
	roundTripper         http.RoundTripper
	httpClient           *http.Client
	ctx                  context.Context
	maxRedirectCount     int
	userAgent            string
	enableCompression    bool
	progressListener     ProgressListener
	customProxyOnce      sync.Once
	customProxyFuncValue func(*url.URL) (*url.URL, error)
}

func (conf config) String() string {
	return fmt.Sprintf("[endpoint:%s, signature:%s, pathStyle:%v, region:%s,"+
		"\nconnectTimeout:%d, socketTimeout:%d, headerTimeout:%d, idleConnTimeout:%d,"+
		"\nmaxRetryCount:%d, maxConnsPerHost:%d, sslVerify:%v, maxRedirectCount:%d,"+
		"\ncname:%v, userAgent:%s, disableKeepAlive:%v, proxyFromEnv:%v]",
		conf.endpoint, conf.signature, conf.pathStyle, conf.region,
		conf.connectTimeout, conf.socketTimeout, conf.headerTimeout, conf.idleConnTimeout,
		conf.maxRetryCount, conf.maxConnsPerHost, conf.sslVerify, conf.maxRedirectCount,
		conf.cname, conf.userAgent, conf.disableKeepAlive, conf.proxyFromEnv,
	)
}

type configurer func(conf *config)

func WithSecurityProviders(sps ...securityProvider) configurer {
	return func(conf *config) {
		for _, sp := range sps {
			if sp != nil {
				conf.securityProviders = append(conf.securityProviders, sp)
			}
		}
	}
}

// WithSslVerify is a wrapper for WithSslVerifyAndPemCerts.
func WithSslVerify(sslVerify bool) configurer {
	return WithSslVerifyAndPemCerts(sslVerify, nil)
}

// WithSslVerifyAndPemCerts is a configurer for ObsClient to set conf.sslVerify and conf.pemCerts.
func WithSslVerifyAndPemCerts(sslVerify bool, pemCerts []byte) configurer {
	return func(conf *config) {
		conf.sslVerify = sslVerify
		conf.pemCerts = pemCerts
	}
}

// WithHeaderTimeout is a configurer for ObsClient to set the timeout period of obtaining the response headers.
func WithHeaderTimeout(headerTimeout int) configurer {
	return func(conf *config) {
		conf.headerTimeout = headerTimeout
	}
}

// WithProxyUrl is a configurer for ObsClient to set HTTP proxy.
func WithProxyUrl(proxyURL string) configurer {
	return func(conf *config) {
		conf.proxyURL = proxyURL
	}
}

// WithNoProxyUrl is a configurer for ObsClient to set HTTP no_proxy.
func WithNoProxyUrl(noProxyURL string) configurer {
	return func(conf *config) {
		conf.noProxyURL = noProxyURL
	}
}

// WithProxyFromEnv is a configurer for ObsClient to get proxy from evironment.
func WithProxyFromEnv(proxyFromEnv bool) configurer {
	return func(conf *config) {
		conf.proxyFromEnv = proxyFromEnv
	}
}

// WithMaxConnections is a configurer for ObsClient to set the maximum number of idle HTTP connections.
func WithMaxConnections(maxConnsPerHost int) configurer {
	return func(conf *config) {
		conf.maxConnsPerHost = maxConnsPerHost
	}
}

// WithPathStyle is a configurer for ObsClient.
func WithPathStyle(pathStyle bool) configurer {
	return func(conf *config) {
		conf.pathStyle = pathStyle
	}
}

// WithSignature is a configurer for ObsClient.
func WithSignature(signature SignatureType) configurer {
	return func(conf *config) {
		conf.signature = signature
	}
}

// WithRegion is a configurer for ObsClient.
func WithRegion(region string) configurer {
	return func(conf *config) {
		conf.region = region
	}
}

// WithConnectTimeout is a configurer for ObsClient to set timeout period for establishing
// an http/https connection, in seconds.
func WithConnectTimeout(connectTimeout int) configurer {
	return func(conf *config) {
		conf.connectTimeout = connectTimeout
	}
}

// WithSocketTimeout is a configurer for ObsClient to set the timeout duration for transmitting data at
// the socket layer, in seconds.
func WithSocketTimeout(socketTimeout int) configurer {
	return func(conf *config) {
		conf.socketTimeout = socketTimeout
	}
}

// WithIdleConnTimeout is a configurer for ObsClient to set the timeout period of an idle HTTP connection
// in the connection pool, in seconds.
func WithIdleConnTimeout(idleConnTimeout int) configurer {
	return func(conf *config) {
		conf.idleConnTimeout = idleConnTimeout
	}
}

// WithMaxRetryCount is a configurer for ObsClient to set the maximum number of retries when an HTTP/HTTPS connection is abnormal.
func WithMaxRetryCount(maxRetryCount int) configurer {
	return func(conf *config) {
		conf.maxRetryCount = maxRetryCount
	}
}

// WithSecurityToken is a configurer for ObsClient to set the security token in the temporary access keys.
func WithSecurityToken(securityToken string) configurer {
	return func(conf *config) {
		for _, sp := range conf.securityProviders {
			if bsp, ok := sp.(*BasicSecurityProvider); ok {
				sh := bsp.getSecurity()
				bsp.refresh(sh.ak, sh.sk, securityToken)
				break
			}
		}
	}
}

// WithHttpTransport is a configurer for ObsClient to set the customized http Transport.
func WithHttpTransport(transport *http.Transport) configurer {
	return func(conf *config) {
		conf.transport = transport
	}
}

func WithHttpClient(httpClient *http.Client) configurer {
	return func(conf *config) {
		conf.httpClient = httpClient
	}
}

// WithRequestContext is a configurer for ObsClient to set the context for each HTTP request.
func WithRequestContext(ctx context.Context) configurer {
	return func(conf *config) {
		conf.ctx = ctx
	}
}

// WithCustomDomainName is a configurer for ObsClient.
func WithCustomDomainName(cname bool) configurer {
	return func(conf *config) {
		conf.cname = cname
	}
}

// WithDisableKeepAlive is a configurer for ObsClient to disable the keep-alive for http.
func WithDisableKeepAlive(disableKeepAlive bool) configurer {
	return func(conf *config) {
		conf.disableKeepAlive = disableKeepAlive
	}
}

// WithMaxRedirectCount is a configurer for ObsClient to set the maximum number of times that the request is redirected.
func WithMaxRedirectCount(maxRedirectCount int) configurer {
	return func(conf *config) {
		conf.maxRedirectCount = maxRedirectCount
	}
}

// WithUserAgent is a configurer for ObsClient to set the User-Agent.
func WithUserAgent(userAgent string) configurer {
	return func(conf *config) {
		conf.userAgent = userAgent
	}
}

// WithEnableCompression is a configurer for ObsClient to set the Transport.DisableCompression.
func WithEnableCompression(enableCompression bool) configurer {
	return func(conf *config) {
		conf.enableCompression = enableCompression
	}
}

func (conf *config) prepareConfig() {
	if conf.connectTimeout <= 0 {
		conf.connectTimeout = DEFAULT_CONNECT_TIMEOUT
	}

	if conf.socketTimeout <= 0 {
		conf.socketTimeout = DEFAULT_SOCKET_TIMEOUT
	}

	conf.finalTimeout = conf.socketTimeout * 10

	if conf.headerTimeout <= 0 {
		conf.headerTimeout = DEFAULT_HEADER_TIMEOUT
	}

	if conf.idleConnTimeout < 0 {
		conf.idleConnTimeout = DEFAULT_IDLE_CONN_TIMEOUT
	}

	if conf.maxRetryCount < 0 {
		conf.maxRetryCount = DEFAULT_MAX_RETRY_COUNT
	}

	if conf.maxConnsPerHost <= 0 {
		conf.maxConnsPerHost = DEFAULT_MAX_CONN_PER_HOST
	}

	if conf.maxRedirectCount < 0 {
		conf.maxRedirectCount = DEFAULT_MAX_REDIRECT_COUNT
	}

	if conf.pathStyle && conf.signature == SignatureObs {
		conf.signature = SignatureV2
	}
}

func (conf *config) initConfigWithDefault() error {
	conf.endpoint = strings.TrimSpace(conf.endpoint)
	if conf.endpoint == "" {
		return errors.New("endpoint is not set")
	}

	if index := strings.Index(conf.endpoint, "?"); index > 0 {
		conf.endpoint = conf.endpoint[:index]
	}

	for strings.LastIndex(conf.endpoint, "/") == len(conf.endpoint)-1 {
		conf.endpoint = conf.endpoint[:len(conf.endpoint)-1]
	}

	if conf.signature == "" {
		conf.signature = DEFAULT_SIGNATURE
	}

	urlHolder := &urlHolder{}
	var address string
	if strings.HasPrefix(conf.endpoint, "https://") {
		urlHolder.scheme = "https"
		address = conf.endpoint[len("https://"):]
	} else if strings.HasPrefix(conf.endpoint, "http://") {
		urlHolder.scheme = "http"
		address = conf.endpoint[len("http://"):]
	} else {
		urlHolder.scheme = "https"
		address = conf.endpoint
	}

	addr := strings.Split(address, ":")
	if len(addr) == 2 {
		if port, err := strconv.Atoi(addr[1]); err == nil {
			urlHolder.port = port
		}
	}
	urlHolder.host = addr[0]
	if urlHolder.port == 0 {
		if urlHolder.scheme == "https" {
			urlHolder.port = 443
		} else {
			urlHolder.port = 80
		}
	}

	if IsIP(urlHolder.host) {
		conf.pathStyle = true
	}

	conf.urlHolder = urlHolder

	conf.region = strings.TrimSpace(conf.region)
	if conf.region == "" {
		conf.region = DEFAULT_REGION
	}

	conf.prepareConfig()
	conf.proxyURL = strings.TrimSpace(conf.proxyURL)
	return nil
}

func (conf *config) getTransport() error {
	if conf.transport == nil {
		conf.transport = &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				conn, err := net.DialTimeout(network, addr, time.Second*time.Duration(conf.connectTimeout))
				if err != nil {
					return nil, err
				}
				return getConnDelegate(conn, conf.socketTimeout, conf.finalTimeout), nil
			},
			MaxIdleConns:          conf.maxConnsPerHost,
			MaxIdleConnsPerHost:   conf.maxConnsPerHost,
			ResponseHeaderTimeout: time.Second * time.Duration(conf.headerTimeout),
			IdleConnTimeout:       time.Second * time.Duration(conf.idleConnTimeout),
			DisableKeepAlives:     conf.disableKeepAlive,
		}
		if conf.proxyURL != "" {
			conf.transport.Proxy = conf.customProxyFromEnvironment
		} else if conf.proxyFromEnv {
			conf.transport.Proxy = http.ProxyFromEnvironment
		}

		tlsConfig := &tls.Config{InsecureSkipVerify: !conf.sslVerify}
		if conf.sslVerify && conf.pemCerts != nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(conf.pemCerts)
			tlsConfig.RootCAs = pool
		}

		conf.transport.TLSClientConfig = tlsConfig
		conf.transport.DisableCompression = !conf.enableCompression
	}

	return nil
}

func (conf *config) customProxyFromEnvironment(req *http.Request) (*url.URL, error) {
	url, err := conf.customProxyFunc()(req.URL)
	return url, err
}

func (conf *config) customProxyFunc() func(*url.URL) (*url.URL, error) {
	conf.customProxyOnce.Do(func() {
		customhttpproxy := &httpproxy.Config{
			HTTPProxy:  conf.proxyURL,
			HTTPSProxy: conf.proxyURL,
			NoProxy:    conf.noProxyURL,
			CGI:        os.Getenv("REQUEST_METHOD") != "",
		}
		conf.customProxyFuncValue = customhttpproxy.ProxyFunc()
	})
	return conf.customProxyFuncValue
}

func checkRedirectFunc(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

// DummyQueryEscape return the input string.
func DummyQueryEscape(s string) string {
	return s
}

func (conf *config) prepareBaseURL(bucketName string) (requestURL string, canonicalizedURL string) {
	urlHolder := conf.urlHolder
	if conf.cname {
		requestURL = fmt.Sprintf("%s://%s:%d", urlHolder.scheme, urlHolder.host, urlHolder.port)
		if conf.signature == "v4" {
			canonicalizedURL = "/"
		} else {
			canonicalizedURL = "/" + urlHolder.host + "/"
		}
	} else {
		if bucketName == "" {
			requestURL = fmt.Sprintf("%s://%s:%d", urlHolder.scheme, urlHolder.host, urlHolder.port)
			canonicalizedURL = "/"
		} else {
			if conf.pathStyle {
				requestURL = fmt.Sprintf("%s://%s:%d/%s", urlHolder.scheme, urlHolder.host, urlHolder.port, bucketName)
				canonicalizedURL = "/" + bucketName
			} else {
				requestURL = fmt.Sprintf("%s://%s.%s:%d", urlHolder.scheme, bucketName, urlHolder.host, urlHolder.port)
				if conf.signature == "v2" || conf.signature == "OBS" {
					canonicalizedURL = "/" + bucketName + "/"
				} else {
					canonicalizedURL = "/"
				}
			}
		}
	}
	return
}

func (conf *config) prepareObjectKey(escape bool, objectKey string, escapeFunc func(s string) string) (encodeObjectKey string) {
	if escape {
		tempKey := []rune(objectKey)
		result := make([]string, 0, len(tempKey))
		for _, value := range tempKey {
			if string(value) == "/" {
				result = append(result, string(value))
			} else {
				if string(value) == " " {
					result = append(result, url.PathEscape(string(value)))
				} else {
					result = append(result, url.QueryEscape(string(value)))
				}
			}
		}
		encodeObjectKey = strings.Join(result, "")
	} else {
		encodeObjectKey = escapeFunc(objectKey)
	}
	return
}

func (conf *config) prepareEscapeFunc(escape bool) (escapeFunc func(s string) string) {
	if escape {
		return url.QueryEscape
	}
	return DummyQueryEscape
}

func (conf *config) formatUrls(bucketName, objectKey string, params map[string]string, escape bool) (requestURL string, canonicalizedURL string) {

	requestURL, canonicalizedURL = conf.prepareBaseURL(bucketName)
	var escapeFunc func(s string) string
	escapeFunc = conf.prepareEscapeFunc(escape)

	if objectKey != "" {
		var encodeObjectKey string
		encodeObjectKey = conf.prepareObjectKey(escape, objectKey, escapeFunc)
		requestURL += "/" + encodeObjectKey
		if !strings.HasSuffix(canonicalizedURL, "/") {
			canonicalizedURL += "/"
		}
		canonicalizedURL += encodeObjectKey
	}

	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, strings.TrimSpace(key))
	}
	sort.Strings(keys)
	i := 0

	for index, key := range keys {
		if index == 0 {
			requestURL += "?"
		} else {
			requestURL += "&"
		}
		_key := url.QueryEscape(key)
		requestURL += _key

		_value := params[key]
		if conf.signature == "v4" {
			requestURL += "=" + url.QueryEscape(_value)
		} else {
			if _value != "" {
				requestURL += "=" + url.QueryEscape(_value)
				_value = "=" + _value
			} else {
				_value = ""
			}
			lowerKey := strings.ToLower(key)
			_, ok := allowedResourceParameterNames[lowerKey]
			prefixHeader := HEADER_PREFIX
			isObs := conf.signature == SignatureObs
			if isObs {
				prefixHeader = HEADER_PREFIX_OBS
			}
			ok = ok || strings.HasPrefix(lowerKey, prefixHeader)
			if ok {
				if i == 0 {
					canonicalizedURL += "?"
				} else {
					canonicalizedURL += "&"
				}
				canonicalizedURL += getQueryURL(_key, _value)
				i++
			}
		}
	}
	return
}

func getQueryURL(key, value string) string {
	queryURL := ""
	queryURL += key
	queryURL += value
	return queryURL
}

func (obsClient ObsClient) getProgressListener(extensions []extensionOptions) ProgressListener {
	for _, extension := range extensions {
		if configure, ok := extension.(extensionProgressListener); ok {
			return configure()
		}
	}
	return nil
}
