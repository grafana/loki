package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	json "github.com/json-iterator/go"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	queryPath       = "/loki/api/v1/query"
	queryRangePath  = "/loki/api/v1/query_range"
	labelsPath      = "/loki/api/v1/labels"
	labelValuesPath = "/loki/api/v1/label/%s/values"
)

// QueryClient contains the methods of the query API.
// It is an interface to allow other implementations, such as FileClient.
type QueryClient interface {
	Query(queryStr string, limit int, time time.Time, direction logproto.Direction) (*loghttp.QueryResponse, error)
	QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration) (*loghttp.QueryResponse, error)
	ListLabelNames(start, end time.Time) (*loghttp.LabelResponse, error)
	ListLabelValues(name string, start, end time.Time) (*loghttp.LabelResponse, error)
}

var _ QueryClient = (*Client)(nil)

// Query uses the /api/v1/query endpoint to execute an instant query.
func (c *Client) Query(queryStr string, limit int, time time.Time, direction logproto.Direction) (*loghttp.QueryResponse, error) {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", queryStr)
	qsb.SetInt("limit", int64(limit))
	qsb.SetInt("time", time.UnixNano())
	qsb.SetString("direction", direction.String())

	return c.doQuery(queryPath, qsb.Encode())
}

// QueryRange uses the /api/v1/query_range endpoint to execute a range query.
func (c *Client) QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration) (*loghttp.QueryResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetString("query", queryStr)
	params.SetInt32("limit", limit)
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	params.SetString("direction", direction.String())

	// The step is optional, so we do set it only if provided,
	// otherwise we do leverage on the API defaults
	if step != 0 {
		params.SetFloat("step", step.Seconds())
	}

	if interval != 0 {
		params.SetFloat("interval", interval.Seconds())
	}

	return c.doQuery(queryRangePath, params.Encode())
}

// ListLabelNames uses the /api/v1/label endpoint to list label names.
func (c *Client) ListLabelNames(start, end time.Time) (*loghttp.LabelResponse, error) {
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())

	if err := c.getJSON(labelsPath, params.Encode(), &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// ListLabelValues uses the /api/v1/label endpoint to list label values.
func (c *Client) ListLabelValues(name string, start, end time.Time) (*loghttp.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())

	if err := c.getJSON(path, params.Encode(), &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *Client) doQuery(path, query string) (*loghttp.QueryResponse, error) {
	var r loghttp.QueryResponse
	if err := c.getJSON(path, query, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// getJSON sends a GET request and decodes the JSON response into out.
func (c *Client) getJSON(path, query string, out any) error {
	res, err := c.Do(context.Background(), http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return json.NewDecoder(res.Body).Decode(out)
}
