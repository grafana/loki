// Package cloudflare implements the Cloudflare v4 API.
package cloudflare

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/time/rate"
)

const apiURL = "https://api.cloudflare.com/client/v4"

const (
	originCARootCertEccURL = "https://developers.cloudflare.com/ssl/0d2cd0f374da0fb6dbf53128b60bbbf7/origin_ca_ecc_root.pem"
	originCARootCertRsaURL = "https://developers.cloudflare.com/ssl/e2b9968022bf23b071d95229b5678452/origin_ca_rsa_root.pem"
)

const (
	// AuthKeyEmail specifies that we should authenticate with API key and email address
	AuthKeyEmail = 1 << iota
	// AuthUserService specifies that we should authenticate with a User-Service key
	AuthUserService
	// AuthToken specifies that we should authenticate with an API Token
	AuthToken
)

// API holds the configuration for the current API client. A client should not
// be modified concurrently.
type API struct {
	APIKey            string
	APIEmail          string
	APIUserServiceKey string
	APIToken          string
	BaseURL           string
	AccountID         string
	UserAgent         string
	headers           http.Header
	httpClient        *http.Client
	authType          int
	rateLimiter       *rate.Limiter
	retryPolicy       RetryPolicy
	logger            Logger
}

// newClient provides shared logic for New and NewWithUserServiceKey
func newClient(opts ...Option) (*API, error) {
	silentLogger := log.New(ioutil.Discard, "", log.LstdFlags)

	api := &API{
		BaseURL:     apiURL,
		headers:     make(http.Header),
		rateLimiter: rate.NewLimiter(rate.Limit(4), 1), // 4rps equates to default api limit (1200 req/5 min)
		retryPolicy: RetryPolicy{
			MaxRetries:    3,
			MinRetryDelay: time.Duration(1) * time.Second,
			MaxRetryDelay: time.Duration(30) * time.Second,
		},
		logger: silentLogger,
	}

	err := api.parseOptions(opts...)
	if err != nil {
		return nil, errors.Wrap(err, "options parsing failed")
	}

	// Fall back to http.DefaultClient if the package user does not provide
	// their own.
	if api.httpClient == nil {
		api.httpClient = http.DefaultClient
	}

	return api, nil
}

// New creates a new Cloudflare v4 API client.
func New(key, email string, opts ...Option) (*API, error) {
	if key == "" || email == "" {
		return nil, errors.New(errEmptyCredentials)
	}

	api, err := newClient(opts...)
	if err != nil {
		return nil, err
	}

	api.APIKey = key
	api.APIEmail = email
	api.authType = AuthKeyEmail

	return api, nil
}

// NewWithAPIToken creates a new Cloudflare v4 API client using API Tokens
func NewWithAPIToken(token string, opts ...Option) (*API, error) {
	if token == "" {
		return nil, errors.New(errEmptyAPIToken)
	}

	api, err := newClient(opts...)
	if err != nil {
		return nil, err
	}

	api.APIToken = token
	api.authType = AuthToken

	return api, nil
}

// NewWithUserServiceKey creates a new Cloudflare v4 API client using service key authentication.
func NewWithUserServiceKey(key string, opts ...Option) (*API, error) {
	if key == "" {
		return nil, errors.New(errEmptyCredentials)
	}

	api, err := newClient(opts...)
	if err != nil {
		return nil, err
	}

	api.APIUserServiceKey = key
	api.authType = AuthUserService

	return api, nil
}

// SetAuthType sets the authentication method (AuthKeyEmail, AuthToken, or AuthUserService).
func (api *API) SetAuthType(authType int) {
	api.authType = authType
}

// ZoneIDByName retrieves a zone's ID from the name.
func (api *API) ZoneIDByName(zoneName string) (string, error) {
	zoneName = normalizeZoneName(zoneName)
	res, err := api.ListZonesContext(context.Background(), WithZoneFilters(zoneName, api.AccountID, ""))
	if err != nil {
		return "", errors.Wrap(err, "ListZonesContext command failed")
	}

	switch len(res.Result) {
	case 0:
		return "", errors.New("zone could not be found")
	case 1:
		return res.Result[0].ID, nil
	default:
		return "", errors.New("ambiguous zone name; an account ID might help")
	}
}

// makeRequest makes a HTTP request and returns the body as a byte slice,
// closing it before returning. params will be serialized to JSON.
func (api *API) makeRequest(method, uri string, params interface{}) ([]byte, error) {
	return api.makeRequestWithAuthType(context.Background(), method, uri, params, api.authType)
}

func (api *API) makeRequestContext(ctx context.Context, method, uri string, params interface{}) ([]byte, error) {
	return api.makeRequestWithAuthType(ctx, method, uri, params, api.authType)
}

func (api *API) makeRequestContextWithHeaders(ctx context.Context, method, uri string, params interface{}, headers http.Header) ([]byte, error) {
	return readAllClose(api.makeRequestWithAuthTypeAndHeaders(ctx, method, uri, params, api.authType, headers))
}

func (api *API) makeRequestWithHeaders(method, uri string, params interface{}, headers http.Header) ([]byte, error) {
	return readAllClose(api.makeRequestWithAuthTypeAndHeaders(context.Background(), method, uri, params, api.authType, headers))
}

func (api *API) makeRequestWithAuthType(ctx context.Context, method, uri string, params interface{}, authType int) ([]byte, error) {
	return readAllClose(api.makeRequestWithAuthTypeAndHeaders(ctx, method, uri, params, authType, nil))
}

func readAllClose(r io.ReadCloser, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (api *API) makeRequestWithAuthTypeAndHeaders(ctx context.Context, method, uri string, params interface{}, authType int, headers http.Header) (io.ReadCloser, error) {
	// Replace nil with a JSON object if needed
	var jsonBody []byte
	var err error

	if params != nil {
		if paramBytes, ok := params.([]byte); ok {
			jsonBody = paramBytes
		} else {
			jsonBody, err = json.Marshal(params)
			if err != nil {
				return nil, errors.Wrap(err, "error marshalling params to JSON")
			}
		}
	} else {
		jsonBody = nil
	}

	var resp *http.Response
	var respErr error
	var reqBody io.Reader
	var respBody []byte
	for i := 0; i <= api.retryPolicy.MaxRetries; i++ {
		if jsonBody != nil {
			reqBody = bytes.NewReader(jsonBody)
		}
		if i > 0 {
			// expect the backoff introduced here on errored requests to dominate the effect of rate limiting
			// don't need a random component here as the rate limiter should do something similar
			// nb time duration could truncate an arbitrary float. Since our inputs are all ints, we should be ok
			sleepDuration := time.Duration(math.Pow(2, float64(i-1)) * float64(api.retryPolicy.MinRetryDelay))

			if sleepDuration > api.retryPolicy.MaxRetryDelay {
				sleepDuration = api.retryPolicy.MaxRetryDelay
			}
			// useful to do some simple logging here, maybe introduce levels later
			api.logger.Printf("Sleeping %s before retry attempt number %d for request %s %s", sleepDuration.String(), i, method, uri)

			select {
			case <-time.After(sleepDuration):
			case <-ctx.Done():
				return nil, errors.Wrap(ctx.Err(), "operation aborted during backoff")
			}

		}
		err = api.rateLimiter.Wait(context.Background())
		if err != nil {
			return nil, errors.Wrap(err, "Error caused by request rate limiting")
		}
		resp, respErr = api.request(ctx, method, uri, reqBody, authType, headers)

		// retry if the server is rate limiting us or if it failed
		// assumes server operations are rolled back on failure
		if respErr != nil || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			// if we got a valid http response, try to read body so we can reuse the connection
			// see https://golang.org/pkg/net/http/#Client.Do
			if respErr == nil {
				respBody, err = readBody(resp)

				respErr = errors.Wrap(err, "could not read response body")

				api.logger.Printf("Request: %s %s got an error response %d: %s\n", method, uri, resp.StatusCode,
					strings.Replace(strings.Replace(string(respBody), "\n", "", -1), "\t", "", -1))
			} else {
				api.logger.Printf("Error performing request: %s %s : %s \n", method, uri, respErr.Error())
			}
			continue
		} else {
			break
		}
	}
	if respErr != nil {
		return nil, respErr
	}

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, err = readBody(resp)
		if err != nil {
			return nil, errors.Wrap(err, "could not read response body")
		}
		if strings.HasSuffix(resp.Request.URL.Path, "/filters/validate-expr") {
			return nil, errors.Errorf("%s", respBody)
		}

		if resp.StatusCode > http.StatusInternalServerError {
			return nil, errors.Errorf("HTTP status %d: service failure", resp.StatusCode)
		}

		// Logpull/received error response are not in json.
		if len(respBody) > 0 && respBody[0] == '{' {
			errBody := &Response{}
			err = json.Unmarshal(respBody, &errBody)
			if err != nil {
				return nil, errors.Wrap(err, errUnmarshalErrorBody)
			}

			return nil, &APIRequestError{
				StatusCode: resp.StatusCode,
				Errors:     errBody.Errors,
			}
		}
		return nil, &APIRequestError{
			StatusCode: resp.StatusCode,
			Errors:     []ResponseInfo{{Message: string(respBody)}},
		}

	}
	return getBodyReader(resp)
}

var gzipPool sync.Pool

type gzipResponseBody struct {
	body io.ReadCloser
	gzip *gzip.Reader
}

type closeDiscardBody struct {
	body io.ReadCloser
}

func (b *closeDiscardBody) Read(p []byte) (n int, err error) {
	return b.body.Read(p)
}

func (b *closeDiscardBody) Close() error {
	_, _ = io.Copy(ioutil.Discard, b.body)
	return b.body.Close()
}

func newGzipResponseBody(body io.ReadCloser) (io.ReadCloser, error) {
	gz := gzipPool.Get()
	if gz == nil {
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		return &gzipResponseBody{body: body, gzip: gzipReader}, nil
	}
	res := gz.(*gzipResponseBody)
	err := res.Reset(body)
	return res, err
}

func (b *gzipResponseBody) Read(p []byte) (int, error) {
	return b.gzip.Read(p)
}

func (b *gzipResponseBody) Reset(r io.ReadCloser) error {
	b.body = r
	return b.gzip.Reset(r)
}

func (b *gzipResponseBody) Close() error {
	err := b.gzip.Close()
	if errBody := b.body.Close(); errBody != nil {
		if err == nil {
			err = errBody
		} else {
			err = errors.Wrap(err, errBody.Error())
		}
	}
	gzipPool.Put(b)
	return err
}

func getBodyReader(resp *http.Response) (io.ReadCloser, error) {
	body := &closeDiscardBody{body: resp.Body}
	if resp.Header.Get("Content-Encoding") == "gzip" {
		return newGzipResponseBody(body)
	}
	return body, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	var (
		body io.ReadCloser
		err  error
	)
	if body, err = getBodyReader(resp); err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

// request makes a HTTP request to the given API endpoint, returning the raw
// *http.Response, or an error if one occurred. The caller is responsible for
// closing the response body.
func (api *API) request(ctx context.Context, method, uri string, reqBody io.Reader, authType int, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, api.BaseURL+uri, reqBody)
	if err != nil {
		return nil, errors.Wrap(err, "HTTP request creation failed")
	}

	combinedHeaders := make(http.Header)
	copyHeader(combinedHeaders, api.headers)
	copyHeader(combinedHeaders, headers)
	req.Header = combinedHeaders

	if authType&AuthKeyEmail != 0 {
		req.Header.Set("X-Auth-Key", api.APIKey)
		req.Header.Set("X-Auth-Email", api.APIEmail)
	}
	if authType&AuthUserService != 0 {
		req.Header.Set("X-Auth-User-Service-Key", api.APIUserServiceKey)
	}
	if authType&AuthToken != 0 {
		req.Header.Set("Authorization", "Bearer "+api.APIToken)
	}

	if api.UserAgent != "" {
		req.Header.Set("User-Agent", api.UserAgent)
	}

	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := api.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "HTTP request failed")
	}

	return resp, nil
}

// Returns the base URL to use for API endpoints that exist for accounts.
// If an account option was used when creating the API instance, returns
// the account URL.
//
// accountBase is the base URL for endpoints referring to the current user.
// It exists as a parameter because it is not consistent across APIs.
func (api *API) userBaseURL(accountBase string) string {
	if api.AccountID != "" {
		return "/accounts/" + api.AccountID
	}
	return accountBase
}

// copyHeader copies all headers for `source` and sets them on `target`.
// based on https://godoc.org/github.com/golang/gddo/httputil/header#Copy
func copyHeader(target, source http.Header) {
	for k, vs := range source {
		target[k] = vs
	}
}

// ResponseInfo contains a code and message returned by the API as errors or
// informational messages inside the response.
type ResponseInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Response is a template.  There will also be a result struct.  There will be a
// unique response type for each response, which will include this type.
type Response struct {
	Success  bool           `json:"success"`
	Errors   []ResponseInfo `json:"errors"`
	Messages []ResponseInfo `json:"messages"`
}

// ResultInfoCursors contains information about cursors.
type ResultInfoCursors struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// ResultInfo contains metadata about the Response.
type ResultInfo struct {
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
	TotalPages int               `json:"total_pages"`
	Count      int               `json:"count"`
	Total      int               `json:"total_count"`
	Cursor     string            `json:"cursor"`
	Cursors    ResultInfoCursors `json:"cursors"`
}

// RawResponse keeps the result as JSON form
type RawResponse struct {
	Response
	Result json.RawMessage `json:"result"`
}

// Raw makes a HTTP request with user provided params and returns the
// result as untouched JSON.
func (api *API) Raw(method, endpoint string, data interface{}) (json.RawMessage, error) {
	res, err := api.makeRequest(method, endpoint, data)
	if err != nil {
		return nil, err
	}

	var r RawResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// PaginationOptions can be passed to a list request to configure paging
// These values will be defaulted if omitted, and PerPage has min/max limits set by resource
type PaginationOptions struct {
	Page    int `json:"page,omitempty"`
	PerPage int `json:"per_page,omitempty"`
}

// RetryPolicy specifies number of retries and min/max retry delays
// This config is used when the client exponentially backs off after errored requests
type RetryPolicy struct {
	MaxRetries    int
	MinRetryDelay time.Duration
	MaxRetryDelay time.Duration
}

// Logger defines the interface this library needs to use logging
// This is a subset of the methods implemented in the log package
type Logger interface {
	Printf(format string, v ...interface{})
}

// ReqOption is a functional option for configuring API requests
type ReqOption func(opt *reqOption)

type reqOption struct {
	params url.Values
}

// WithZoneFilters applies a filter based on zone properties.
func WithZoneFilters(zoneName, accountID, status string) ReqOption {
	return func(opt *reqOption) {
		if zoneName != "" {
			opt.params.Set("name", normalizeZoneName(zoneName))
		}

		if accountID != "" {
			opt.params.Set("account.id", accountID)
		}

		if status != "" {
			opt.params.Set("status", status)
		}
	}
}

// WithPagination configures the pagination for a response.
func WithPagination(opts PaginationOptions) ReqOption {
	return func(opt *reqOption) {
		if opts.Page > 0 {
			opt.params.Set("page", strconv.Itoa(opts.Page))
		}

		if opts.PerPage > 0 {
			opt.params.Set("per_page", strconv.Itoa(opts.PerPage))
		}
	}
}

// checkResultInfo checks whether ResultInfo is reasonable except that it currently
// ignores the cursor information. perPage, page, and count are the requested #items
// per page, the requested page number, and the actual length of the Result array.
//
// Responses from the actual Cloudflare servers should pass all these checks (or we
// discover a serious bug in the Cloudflare servers). However, the unit tests can
// easily violate these constraints and this utility function can help debugging.
// Correct pagination information is crucial for more advanced List* functions that
// handle pagination automatically and fetch different pages in parallel.
//
// TODO: check cursors as well.
func checkResultInfo(perPage, page, count int, info *ResultInfo) bool {
	if info.Cursor != "" || info.Cursors.Before != "" || info.Cursors.After != "" {
		panic("checkResultInfo could not handle cursors yet.")
	}

	switch {
	case info.PerPage != perPage || info.Page != page || info.Count != count:
		return false

	case info.PerPage <= 0:
		return false

	case info.Total == 0 && info.TotalPages == 0 && info.Page == 1 && info.Count == 0:
		return true

	case info.Total <= 0 || info.TotalPages <= 0:
		return false

	case info.Total > info.PerPage*info.TotalPages || info.Total <= info.PerPage*(info.TotalPages-1):
		return false
	}

	switch {
	case info.Page > info.TotalPages || info.Page <= 0:
		return false

	case info.Page < info.TotalPages:
		return info.Count == info.PerPage

	case info.Page == info.TotalPages:
		return info.Count == info.Total-info.PerPage*(info.TotalPages-1)

	default:
		// This is actually impossible, but Go compiler does not know trichotomy
		panic("checkResultInfo: impossible")
	}
}
