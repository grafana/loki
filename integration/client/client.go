package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/user"
	"github.com/grafana/jsonparser"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	logcli "github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/util/unmarshal"
)

const requestTimeout = 30 * time.Second

type roundTripper struct {
	instanceID    string
	token         string
	injectHeaders map[string][]string
	next          http.RoundTripper
}

func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Scope-OrgID", r.instanceID)
	if r.token != "" {
		req.SetBasicAuth(r.instanceID, r.token)
	}

	for key, values := range r.injectHeaders {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}

	return r.next.RoundTrip(req)
}

type Option interface {
	Type() string
}

type InjectHeadersOption map[string][]string

func (n InjectHeadersOption) Type() string {
	return "headerinject"
}

// Client is a HTTP client that adds basic auth and scope
type Client struct {
	Now time.Time

	httpClient *http.Client
	baseURL    string
	instanceID string
}

// NewLogsClient creates a new client
func New(instanceID, token, baseURL string, opts ...Option) *Client {
	rt := &roundTripper{
		instanceID: instanceID,
		token:      token,
		next:       http.DefaultTransport,
	}

	for _, opt := range opts {
		switch opt.Type() {
		case "headerinject":
			rt.injectHeaders = opt.(InjectHeadersOption)
		}
	}

	return &Client{
		Now: time.Now(),
		httpClient: &http.Client{
			Transport: rt,
		},
		baseURL:    baseURL,
		instanceID: instanceID,
	}
}

func (c *Client) PushLogLine(line string, timestamp time.Time, structuredMetadata map[string]string, extraLabelList ...map[string]string) error {
	// If the structuredMetadata map is empty, labels.FromMap will allocate some empty slices.
	// Since this code is executed for every log line we receive, as an optimization
	// to avoid those allocations we'll call labels.FromMap only if the map is not empty.
	var metadata labels.Labels
	if len(structuredMetadata) > 0 {
		metadata = labels.FromMap(structuredMetadata)
	}
	return c.pushLogLine(line, timestamp, metadata, extraLabelList...)
}

func (c *Client) PushOTLPLogLine(line string, timestamp time.Time, logAttributes map[string]any) error {
	return c.pushOTLPLogLine(line, timestamp, logAttributes)
}

func FormatTS(ts time.Time) string {
	return strconv.FormatInt(ts.UnixNano(), 10)
}

type stream struct {
	Stream map[string]string `json:"stream"`
	Values [][]any           `json:"values"`
}

// pushLogLine creates a new logline
func (c *Client) pushLogLine(line string, timestamp time.Time, structuredMetadata labels.Labels, extraLabelList ...map[string]string) error {
	apiEndpoint := fmt.Sprintf("%s/loki/api/v1/push", c.baseURL)

	s := stream{
		Stream: map[string]string{
			"job": "varlog",
		},
		Values: [][]any{
			{
				FormatTS(timestamp),
				line,
				structuredMetadata,
			},
		},
	}
	// add extra labels
	for _, labelList := range extraLabelList {
		for k, v := range labelList {
			s.Stream[k] = v
		}
	}

	data, err := json.Marshal(&struct {
		Streams []stream `json:"streams"`
	}{
		Streams: []stream{s},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", apiEndpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scope-OrgID", c.instanceID)

	// Execute HTTP request
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode/100 == 2 {
		defer res.Body.Close()
		return nil
	}

	buf, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response failed with status code %v: %v", res.StatusCode, err)
	}

	return fmt.Errorf("request failed with status code %v: %s", res.StatusCode, buf)
}

// pushOTLPLogLine creates a new logline
func (c *Client) pushOTLPLogLine(line string, timestamp time.Time, logAttributes map[string]any) error {
	apiEndpoint := fmt.Sprintf("%s/otlp/v1/logs", c.baseURL)

	logs := plog.NewLogs()

	logs.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr("service.name", "varlog")
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	logRecord.SetTimestamp(pcommon.Timestamp(timestamp.UnixNano()))
	logRecord.Body().SetStr(line)
	if len(logAttributes) > 0 {
		if err := logRecord.Attributes().FromRaw(logAttributes); err != nil {
			return err
		}
	}

	ereq := plogotlp.NewExportRequestFromLogs(logs)

	data, err := ereq.MarshalJSON()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", apiEndpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scope-OrgID", c.instanceID)

	// Execute HTTP request
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode/100 == 2 {
		defer res.Body.Close()
		return nil
	}

	buf, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response failed with status code %v: %v", res.StatusCode, err)
	}

	return fmt.Errorf("request failed with status code %v: %s", res.StatusCode, buf)
}

func (c *Client) Get(path string) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

// Get all the metrics
func (c *Client) Metrics() (string, error) {
	url := fmt.Sprintf("%s/metrics", c.baseURL)
	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var sb strings.Builder
	if _, err := io.Copy(&sb, res.Body); err != nil {
		return "", err
	}

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request failed with status code %d", res.StatusCode)
	}
	return sb.String(), nil
}

// Flush all in-memory chunks held by the ingesters to the backing store
func (c *Client) Flush() error {
	req, err := c.request(context.Background(), "POST", fmt.Sprintf("%s/flush", c.baseURL))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode/100 == 2 {
		return nil
	}
	return fmt.Errorf("request failed with status code %d", res.StatusCode)
}

type DeleteRequestParams struct {
	Query string `json:"query"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// AddDeleteRequest adds a new delete request
func (c *Client) AddDeleteRequest(params DeleteRequestParams) error {
	apiEndpoint := fmt.Sprintf("%s/loki/api/v1/delete", c.baseURL)

	req, err := http.NewRequest("POST", apiEndpoint, nil)
	if err != nil {
		return err
	}

	q := req.URL.Query()
	q.Add("query", params.Query)
	q.Add("start", params.Start)
	q.Add("end", params.End)
	req.URL.RawQuery = q.Encode()
	fmt.Printf("Delete request URL: %v\n", req.URL.String())

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusNoContent {
		buf, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("reading request failed with status code %v: %w", res.StatusCode, err)
		}
		defer res.Body.Close()
		return fmt.Errorf("request failed with status code %v: %w", res.StatusCode, errors.New(string(buf)))
	}

	return nil
}

type DeleteRequests []DeleteRequest
type DeleteRequest struct {
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Query     string `json:"query"`
	Status    string `json:"status"`
}

// GetDeleteRequests returns all delete requests
func (c *Client) GetDeleteRequests() (DeleteRequests, error) {
	resp, err := c.Get("/loki/api/v1/delete")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading request failed with status code %v: %w", resp.StatusCode, err)
	}

	var deleteReqs DeleteRequests
	err = json.Unmarshal(buf, &deleteReqs)
	if err != nil {
		return nil, fmt.Errorf("parsing json output failed: %w", err)
	}

	return deleteReqs, nil
}

type Entry []string

func (e *Entry) UnmarshalJSON(data []byte) error {
	if *e == nil {
		*e = make([]string, 0, 3)
	}

	var parseError error
	_, err := jsonparser.ArrayEach(data, func(value []byte, t jsonparser.ValueType, _ int, _ error) {
		// The TS and the lines are strings. The labels are a JSON object.
		// but we will parse them as strings.
		if t != jsonparser.String && t != jsonparser.Object {
			parseError = jsonparser.MalformedStringError
			return
		}

		v, err := jsonparser.ParseString(value)
		if err != nil {
			parseError = err
			return
		}
		*e = append(*e, v)
	})

	if parseError != nil {
		return parseError
	}
	return err
}

// StreamValues holds a label key value pairs for the Stream and a list of a list of values
type StreamValues struct {
	Stream map[string]string
	Values []Entry
}

// MatrixValues holds a label key value pairs for the metric and a list of a list of values
type MatrixValues struct {
	Metric map[string]string
	Values [][]interface{}
}

// VectorValues holds a label key value pairs for the metric and single timestamp and value
type VectorValues struct {
	Metric map[string]string `json:"metric"`
	Time   time.Time
	Value  string
}

func (a *VectorValues) UnmarshalJSON(b []byte) error {
	var s struct {
		Metric map[string]string `json:"metric"`
		Value  []interface{}     `json:"value"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	a.Metric = s.Metric
	if len(s.Value) != 2 {
		return fmt.Errorf("unexpected value length %d", len(s.Value))
	}
	if ts, ok := s.Value[0].(int64); ok {
		a.Time = time.Unix(ts, 0)
	}
	if val, ok := s.Value[1].(string); ok {
		a.Value = val
	}
	return nil
}

// DataType holds the result type and a list of StreamValues
type DataType struct {
	ResultType    string
	Stream        []StreamValues
	Matrix        []MatrixValues
	Vector        []VectorValues
	EncodingFlags []string
}

func (a *DataType) UnmarshalJSON(b []byte) error {
	// get the result type
	var s struct {
		ResultType    string          `json:"resultType"`
		EncodingFlags []string        `json:"encodingFlags"`
		Result        json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s.ResultType {
	case "streams":
		if err := json.Unmarshal(s.Result, &a.Stream); err != nil {
			return err
		}
	case "matrix":
		if err := json.Unmarshal(s.Result, &a.Matrix); err != nil {
			return err
		}
	case "vector":
		if err := json.Unmarshal(s.Result, &a.Vector); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown result type %s", s.ResultType)
	}
	a.ResultType = s.ResultType
	a.EncodingFlags = s.EncodingFlags
	return nil
}

// Response holds the status and data
type Response struct {
	Status string
	Data   DataType
}

type RulesResponse struct {
	Status string
	Data   RulesData
}

type RulesData struct {
	Groups []Rules
}

type Rules struct {
	Name  string
	File  string
	Rules []interface{}
}

type Header struct {
	Name, Value string
}

// RunRangeQuery runs a 7d query and returns an error if anything went wrong
// This function is kept to keep backwards copatibility of existing tests.
// Better use (*Client).RunRangeQueryWithStartEnd()
func (c *Client) RunRangeQuery(ctx context.Context, query string, extraHeaders ...Header) (*Response, error) {
	end := c.Now.Add(time.Second)
	start := c.Now.Add(-7 * 24 * time.Hour)
	return c.RunRangeQueryWithStartEnd(ctx, query, start, end, extraHeaders...)
}

// RunRangeQuery runs a query and returns an error if anything went wrong
func (c *Client) RunRangeQueryWithStartEnd(ctx context.Context, query string, start, end time.Time, extraHeaders ...Header) (*Response, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	buf, statusCode, err := c.run(ctx, c.rangeQueryURL(query, start, end), extraHeaders...)
	if err != nil {
		return nil, err
	}

	return c.parseResponse(buf, statusCode)
}

// RunQuery runs a query and returns an error if anything went wrong
func (c *Client) RunQuery(ctx context.Context, query string, extraHeaders ...Header) (*Response, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	v := url.Values{}
	v.Set("query", query)
	v.Set("time", FormatTS(c.Now.Add(time.Second)))

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/loki/api/v1/query"
	u.RawQuery = v.Encode()

	buf, statusCode, err := c.run(ctx, u.String(), extraHeaders...)
	if err != nil {
		return nil, err
	}

	return c.parseResponse(buf, statusCode)

}

// GetRules returns the loki ruler rules
func (c *Client) GetRules(ctx context.Context) (*RulesResponse, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/prometheus/api/v1/rules"

	buf, _, err := c.run(ctx, u.String())
	if err != nil {
		return nil, err
	}

	resp := RulesResponse{}
	err = json.Unmarshal(buf, &resp)
	if err != nil {
		return nil, fmt.Errorf("error parsing response data %q: %w", buf, err)
	}

	return &resp, err
}

func (c *Client) parseResponse(buf []byte, statusCode int) (*Response, error) {
	if statusCode/100 != 2 {
		return nil, fmt.Errorf("request failed with status code %d: %w", statusCode, errors.New(string(buf)))
	}
	lokiResp := Response{}
	err := json.Unmarshal(buf, &lokiResp)
	if err != nil {
		return nil, fmt.Errorf("error parsing response data '%s': %w", string(buf), err)
	}

	return &lokiResp, nil
}

func (c *Client) rangeQueryURL(query string, start, end time.Time) string {
	v := url.Values{}
	v.Set("query", query)
	v.Set("start", FormatTS(start))
	v.Set("end", FormatTS(end))

	u, err := url.Parse(c.baseURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/loki/api/v1/query_range"
	u.RawQuery = v.Encode()

	return u.String()
}

func (c *Client) LabelNames(ctx context.Context) ([]string, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	url := fmt.Sprintf("%s/loki/api/v1/labels", c.baseURL)

	buf, statusCode, err := c.run(ctx, url)
	if err != nil {
		return nil, err
	}

	if statusCode/100 != 2 {
		return nil, fmt.Errorf("request failed with status code %d: %w", statusCode, errors.New(string(buf)))
	}

	var values struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(buf, &values); err != nil {
		return nil, err
	}

	return values.Data, nil
}

// LabelValues return a LabelValues query result
func (c *Client) LabelValues(ctx context.Context, labelName string) ([]string, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	url := fmt.Sprintf("%s/loki/api/v1/label/%s/values", c.baseURL, url.PathEscape(labelName))

	req, err := c.request(ctx, "GET", url)
	if err != nil {
		return nil, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("unexpected status code of %d", res.StatusCode)
	}

	var values struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&values); err != nil {
		return nil, err
	}

	return values.Data, nil
}

// Series return a series query result
func (c *Client) Series(ctx context.Context, matcher string) ([]map[string]string, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	v := url.Values{}
	v.Set("match[]", matcher)

	u, err := url.Parse(c.baseURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/loki/api/v1/series"
	u.RawQuery = v.Encode()

	buf, statusCode, err := c.run(ctx, u.String())
	if err != nil {
		return nil, err
	}

	if statusCode/100 != 2 {
		return nil, fmt.Errorf("request failed with status code %d: %w", statusCode, errors.New(string(buf)))
	}

	var values struct {
		Data []map[string]string `json:"data"`
	}
	if err := json.Unmarshal(buf, &values); err != nil {
		return nil, err
	}

	return values.Data, nil
}

func (c *Client) Stats(ctx context.Context, query string) ([]map[string]int, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, requestTimeout)
	defer cancelFunc()

	v := url.Values{}
	v.Set("query", query)

	u, err := url.Parse(c.baseURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/loki/api/v1/index/stats"
	u.RawQuery = v.Encode()

	buf, statusCode, err := c.run(ctx, u.String())
	if err != nil {
		return nil, err
	}

	if statusCode/100 != 2 {
		return nil, fmt.Errorf("request failed with status code %d: %w", statusCode, errors.New(string(buf)))
	}

	var values struct {
		Data []map[string]int `json:"data"`
	}
	if err := json.Unmarshal(buf, &values); err != nil {
		return nil, err
	}

	return values.Data, nil
}

type TailResult struct {
	Response loghttp.TailResponse
	Err      error
}

func (c *Client) Tail(ctx context.Context, query string, out chan TailResult) (*websocket.Conn, error) {
	client := &logcli.DefaultClient{
		Address:   c.baseURL,
		OrgID:     c.instanceID,
		TLSConfig: config.TLSConfig{},
	}
	start := time.Now().Add(-1 * time.Hour)

	wc, err := client.LiveTailQueryConn(query, time.Duration(0), 100, start, false)
	if err != nil {
		return nil, err
	}

	go func() {

		tailResponse := new(loghttp.TailResponse)

		for {
			select {
			case <-ctx.Done():
				close(out)
				return
			default:
				err := unmarshal.ReadTailResponseJSON(tailResponse, wc)
				if errors.Is(err, net.ErrClosed) {
					close(out)
					return
				}
				out <- TailResult{*tailResponse, err}
			}
		}
	}()
	return wc, nil
}

func (c *Client) request(ctx context.Context, method string, url string, extraHeaders ...Header) (*http.Request, error) {
	ctx = user.InjectOrgID(ctx, c.instanceID)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Scope-OrgID", c.instanceID)
	for _, h := range extraHeaders {
		req.Header.Add(h.Name, h.Value)
	}
	return req, nil
}

func (c *Client) run(ctx context.Context, u string, extraHeaders ...Header) ([]byte, int, error) {
	req, err := c.request(ctx, "GET", u, extraHeaders...)
	if err != nil {
		return nil, 0, err
	}

	// Execute HTTP request
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()

	buf, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed with status code %v: %w", res.StatusCode, err)
	}

	return buf, res.StatusCode, nil
}
