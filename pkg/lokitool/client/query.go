package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/json-iterator/go"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/lokitool/volume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	queryPath       = "/loki/api/v1/query"
	queryRangePath  = "/loki/api/v1/query_range"
	labelsPath      = "/loki/api/v1/labels"
	labelValuesPath = "/loki/api/v1/label/%s/values"
	seriesPath      = "/loki/api/v1/series"
	volumePath      = "/loki/api/v1/index/volume"

	detectedFieldsPath      = "/loki/api/v1/detected_fields"
	detectedFieldValuesPath = "/loki/api/v1/detected_field/%s/values"
)

// QueryClient contains the methods of the query API.
// It is an interface to allow other implementations, such as FileClient.
type QueryClient interface {
	Query(queryStr string, limit int, time time.Time, direction logproto.Direction) (*loghttp.QueryResponse, error)
	QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration) (*loghttp.QueryResponse, error)
	ListLabelNames(start, end time.Time) (*loghttp.LabelResponse, error)
	ListLabelValues(name string, start, end time.Time) (*loghttp.LabelResponse, error)
	Series(matchers []string, start, end time.Time) (*loghttp.SeriesResponse, error)
	GetVolume(query *volume.Query) (*loghttp.QueryResponse, error)
	GetDetectedFields(queryStr, fieldName string, fieldLimit, lineLimit int, start, end time.Time, step time.Duration) (*loghttp.DetectedFieldsResponse, error)
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

// Series uses the /api/v1/series endpoint to list the streams that match the matchers.
func (c *Client) Series(matchers []string, start, end time.Time) (*loghttp.SeriesResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	params.SetStringArray("match", matchers)

	var seriesResponse loghttp.SeriesResponse
	if err := c.getJSON(seriesPath, params.Encode(), &seriesResponse); err != nil {
		return nil, err
	}
	return &seriesResponse, nil
}

// GetVolume uses the /api/v1/index/volume endpoint to get the volume of the streams that match the query.
func (c *Client) GetVolume(query *volume.Query) (*loghttp.QueryResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetInt("start", query.Start.UnixNano())
	params.SetInt("end", query.End.UnixNano())
	params.SetString("query", query.QueryString)
	params.SetString("limit", strconv.Itoa(query.Limit))

	if len(query.TargetLabels) > 0 {
		params.SetString("targetLabels", strings.Join(query.TargetLabels, ","))
	}

	if query.AggregateByLabels {
		params.SetString("aggregateBy", seriesvolume.Labels)
	}

	var resp loghttp.QueryResponse
	if err := c.getJSON(volumePath, params.Encode(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetDetectedFields uses the /api/v1/detected_fields endpoint to list the fields that Loki detects in the log lines
// that match the query. It uses the /api/v1/detected_field/{name}/values endpoint to list the values of a field if the field name is set.
func (c *Client) GetDetectedFields(queryStr, fieldName string, fieldLimit, lineLimit int, start, end time.Time, step time.Duration) (*loghttp.DetectedFieldsResponse, error) {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", queryStr)
	qsb.SetInt("limit", int64(fieldLimit))
	qsb.SetInt("line_limit", int64(lineLimit))
	qsb.SetInt("start", start.UnixNano())
	qsb.SetInt("end", end.UnixNano())
	// The step is optional, so we only set it if provided,
	// otherwise we leverage the API defaults.
	if step != 0 {
		qsb.SetString("step", step.String())
	}

	path := detectedFieldsPath
	if fieldName != "" {
		path = fmt.Sprintf(detectedFieldValuesPath, url.PathEscape(fieldName))
	}

	var r loghttp.DetectedFieldsResponse
	if err := c.getJSON(path, qsb.Encode(), &r); err != nil {
		return nil, err
	}
	return &r, nil
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
