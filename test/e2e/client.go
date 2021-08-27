// +build requires_docker

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type roundTripper struct {
	instanceID    string
	injectHeaders map[string][]string
	next          http.RoundTripper
}

func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Scope-OrgID", r.instanceID)

	for key, values := range r.injectHeaders {
		for _, v := range values {
			req.Header.Add(key, v)
		}

		fmt.Println(req.Header.Values(key))
	}

	return r.next.RoundTrip(req)
}

type CortexClientOption interface {
	Type() string
}

type InjectHeadersOption map[string][]string

func (n InjectHeadersOption) Type() string {
	return "headerinject"
}

// Client is a HTTP client that adds basic auth and scope
type Client struct {
	httpClient *http.Client
	address    string
	instanceID string
}

// NewLogsClient creates a new client
func NewLogsClient(instanceID, address string, opts ...CortexClientOption) *Client {
	rt := &roundTripper{
		instanceID: instanceID,
		next:       http.DefaultTransport,
	}

	for _, opt := range opts {
		switch opt.Type() {
		case "headerinject":
			rt.injectHeaders = opt.(InjectHeadersOption)
		}
	}

	return &Client{
		httpClient: &http.Client{
			Transport: rt,
		},
		address:    address,
		instanceID: instanceID,
	}
}

// PushLogLine creates a new logline with the current time as timestamp
func (c *Client) PushLogLine(line string, extra_label_list ...map[string]string) error {
	timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	return c.pushLogLine(line, timestamp, extra_label_list...)
}

// PushLogLineWithTimestamp creates a new logline at the given timestamp
// The timestamp has to be a Unix timestamp (epoch seconds)
func (c *Client) PushLogLineWithTimestamp(line, timestamp string, extraLabelList ...map[string]string) error {
	return c.pushLogLine(line, timestamp, extraLabelList...)
}

// pushLogLine creates a new logline
func (c *Client) pushLogLine(line, timestamp string, extraLabelList ...map[string]string) error {
	apiEndpoint := fmt.Sprintf("http://%s/loki/api/v1/push", c.address)

	extra_labels := []string{}
	if len(extraLabelList) > 0 {
		for _, extra_label_map := range extraLabelList {
			for label, value := range extra_label_map {
				l := "\"" + label + "\" : \"" + value + "\""
				extra_labels = append(extra_labels, l)
			}
		}
	}
	labels_string := ""
	if len(extra_labels) > 0 {
		labels_string = ", " + strings.Join(extra_labels, ",")
	}

	data := fmt.Sprintf(`{"streams": [{ "stream": { "job": "varlog" %v }, "values": [ [ "%v", "%v" ] ] }]}`, labels_string, timestamp, line)
	req, err := http.NewRequest("POST", apiEndpoint, strings.NewReader(data))
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

	buf, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading request failed with status code %v: %w", res.StatusCode, err)
	}

	return fmt.Errorf("request failed with status code %v: %w", res.StatusCode, errors.New(string(buf)))
}

// Get all the metrics
func (c *Client) Metrics() (string, error) {
	url := fmt.Sprintf("http://%s/metrics", c.address)
	res, err := http.Get(url)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	io.Copy(&sb, res.Body)

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request failed with status code %d", res.StatusCode)
	}
	return sb.String(), nil
}

// Flush all in-memory chunks held by the ingesters to the backing store
func (c *Client) Flush() error {
	url := fmt.Sprintf("http://%s/flush", c.address)
	res, err := http.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return nil
	}
	return fmt.Errorf("request failed with status code %d", res.StatusCode)
}

// StreamValues holds a label key value pairs for the Stream and a list of a list of values
type StreamValues struct {
	Stream map[string]string
	Values [][]string
}

// DataType holds the result type and a list of StreamValues
type DataType struct {
	ResultType string
	Result     []StreamValues
}

// Response holds the status and data
type Response struct {
	Status string
	Data   DataType
}

// MatrixValues holds a label key value pairs for the metric and a list of a list of values
type MatrixValues struct {
	Metric map[string]string
	Values [][]interface{}
}

// DataType holds the result type and a list of StreamValues
type MatrixDataType struct {
	ResultType string
	Result     []MatrixValues
}

// Response holds the status and data
type MetricsResponse struct {
	Status string
	Data   MatrixDataType
}

// RunRangeQuery runs a query and returns an error if anything went wrong
func (c *Client) RunRangeQuery(query string) (*Response, error) {
	apiEndpoint := fmt.Sprintf("http://%v/loki/api/v1/query_range", c.address)

	buf, statusCode, err := c.run(query, apiEndpoint)
	if err != nil {
		return nil, err
	}

	return c.parseLogResponse(buf, statusCode)
}

// RunQuery runs a query and returns an error if anything went wrong
func (c *Client) RunQuery(query string) (*Response, error) {
	apiEndpoint := fmt.Sprintf("http://%v/loki/api/v1/query", c.address)

	buf, statusCode, err := c.run(query, apiEndpoint)
	if err != nil {
		return nil, err
	}

	return c.parseLogResponse(buf, statusCode)
}

func (c *Client) parseLogResponse(buf []byte, statusCode int) (*Response, error) {
	lokiResp := Response{}
	err := json.Unmarshal(buf, &lokiResp)
	if err != nil {
		return nil, fmt.Errorf("error parsing response data: %w", err)
	}

	if statusCode/100 == 2 {
		return &lokiResp, nil
	}
	return nil, fmt.Errorf("request failed with status code %d: %w", statusCode, errors.New(string(buf)))
}

// RunMetricsRangeQuery runs a query and returns an error if anything went wrong
func (c *Client) RunMetricsRangeQuery(query string) (*MetricsResponse, error) {
	apiEndpoint := fmt.Sprintf("http://%s/loki/api/v1/query_range", c.address)

	buf, statusCode, err := c.run(query, apiEndpoint)
	if err != nil {
		return nil, err
	}

	return c.parse_metrics_response(buf, statusCode)
}

func (c *Client) parse_metrics_response(buf []byte, statusCode int) (*MetricsResponse, error) {
	lokiResp := MetricsResponse{}
	err := json.Unmarshal(buf, &lokiResp)
	if err != nil {
		return nil, fmt.Errorf("error parsing response data: %w", err)
	}

	if statusCode/100 == 2 {
		return &lokiResp, nil
	}
	return nil, fmt.Errorf("request failed with status code %d: %w", statusCode, errors.New(string(buf)))
}

func (c *Client) run(query, endpoint string) ([]byte, int, error) {
	v := url.Values{}
	v.Set("query", query)
	url, err := url.Parse(endpoint)
	if err != nil {
		return nil, 0, err
	}
	url.RawQuery = v.Encode()

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Scope-OrgID", c.instanceID)

	// Execute HTTP request
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()

	buf, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed with status code %v: %w", res.StatusCode, err)
	}

	return buf, res.StatusCode, nil
}
