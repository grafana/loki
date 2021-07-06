package client

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	json "github.com/json-iterator/go"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	logqllog "github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/marshal"
)

const (
	queryPath       = "/loki/api/v1/query"
	queryRangePath  = "/loki/api/v1/query_range"
	labelsPath      = "/loki/api/v1/labels"
	labelValuesPath = "/loki/api/v1/label/%s/values"
	seriesPath      = "/loki/api/v1/series"
	tailPath        = "/loki/api/v1/tail"
)

var userAgent = fmt.Sprintf("loki-logcli/%s", build.Version)

// Client contains all the methods to query a Loki instance, it's an interface to allow multiple implementations.
type Client interface {
	Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error)
	QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error)
	ListLabelNames(quiet bool, start, end time.Time) (*loghttp.LabelResponse, error)
	ListLabelValues(name string, quiet bool, start, end time.Time) (*loghttp.LabelResponse, error)
	Series(matchers []string, start, end time.Time, quiet bool) (*loghttp.SeriesResponse, error)
	LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error)
	GetOrgID() string
}

type limiter struct {
	n int
}

func (l *limiter) MaxQuerySeries(userID string) int {
	return l.n
}

type querier struct {
	r io.Reader
}

func (q *querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	expr, err := params.LogSelector()
	if err != nil {
		panic(err)
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		panic(err)
	}
	streampipe := pipeline.ForStream(labels.Labels{
		labels.Label{Name: "foo", Value: "bar"},
	})
	it := NewFileIterator(q.r, "source:logcli", streampipe)

	return it, nil
}

func (q *querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	expr, err := params.Expr()
	if err != nil {
		panic(err)
	}

	fmt.Println("expr", expr.String())
	sampleExtractor, err := expr.Extractor()
	if err != nil {
		panic(err)
	}

	streamSample := sampleExtractor.ForStream(labels.Labels{
		labels.Label{Name: "foo", Value: "bar"},
	})

	it := NewFileSampleIterator(q.r, "source:logcli", streamSample)

	return it, nil
}

type FileSampleIterator struct {
	s      *bufio.Scanner
	labels string
	err    error
	sp     logqllog.StreamSampleExtractor
	curr   logproto.Sample
}

func NewFileSampleIterator(r io.Reader, labels string, sp logqllog.StreamSampleExtractor) *FileSampleIterator {
	s := bufio.NewScanner(r)
	s.Split(bufio.ScanLines)
	return &FileSampleIterator{
		s:      s,
		labels: labels,
		sp:     sp,
	}
}

func (f *FileSampleIterator) Next() bool {
	for f.s.Scan() {
		fmt.Println("input", f.s.Text())
		value, labels, ok := f.sp.Process([]byte(f.s.Text()))
		fmt.Println("output", "value", value, "labels", labels, "skip", ok)

		if ok {
			f.curr = logproto.Sample{
				Timestamp: time.Now().Unix(),
				Value:     value,
				Hash:      labels.Hash(),
			}
			return true
		}
	}
	return false
}

func (f *FileSampleIterator) Sample() logproto.Sample {
	return logproto.Sample{}
}

func (f *FileSampleIterator) Labels() string {
	return f.labels
}

func (f *FileSampleIterator) Error() error {
	return f.err
}

func (f *FileSampleIterator) Close() error {
	// TODO: accept io.ReadCloser()?
	return nil
}

type FileIterator struct {
	s      *bufio.Scanner
	labels string
	err    error
	sp     logqllog.StreamPipeline
	curr   logproto.Entry
}

func NewFileIterator(r io.Reader, labels string, sp logqllog.StreamPipeline) *FileIterator {
	s := bufio.NewScanner(r)
	s.Split(bufio.ScanLines)
	return &FileIterator{
		s:      s,
		labels: labels,
		sp:     sp,
	}
}

func (f *FileIterator) Next() bool {
	for f.s.Scan() {
		_, _, ok := f.sp.Process([]byte(f.s.Text()))
		if ok {
			f.curr = logproto.Entry{
				Timestamp: time.Now(),
				Line:      f.s.Text(),
			}
			return true
		}
	}
	return false
}

func (f *FileIterator) Entry() logproto.Entry {
	return f.curr
}

func (f *FileIterator) Labels() string {
	return f.labels
}

func (f *FileIterator) Error() error {
	return f.err
}

func (f *FileIterator) Close() error {
	return nil
}

type FileClient struct {
	r           io.Reader
	labels      []string
	labelValues []string
	orgID       string
	engine      *logql.Engine
}

func NewFileClient(r io.Reader) *FileClient {
	eng := logql.NewEngine(logql.EngineOpts{}, &querier{r: r}, &limiter{})
	return &FileClient{
		r:           r,
		orgID:       "fake",
		engine:      eng,
		labels:      []string{"foo"},
		labelValues: []string{"bar"},
	}

}

func (f *FileClient) Query(q string, limit int, t time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	ctx := context.Background()

	ctx = user.InjectOrgID(ctx, "fake")

	params := logql.NewLiteralParams(
		q,
		t, t,
		0,
		0,
		direction,
		uint32(limit),
		nil,
	)

	query := f.engine.Query(params)

	result, err := query.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to exec query: %w", err)
	}

	value, err := marshal.NewResultValue(result.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result data: %w", err)
	}

	return &loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: value.Type(),
			Result:     value,
			Statistics: result.Statistics,
		},
	}, nil
}

func (f *FileClient) QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error) {
	ctx := context.Background()

	ctx = user.InjectOrgID(ctx, "fake")

	params := logql.NewLiteralParams(
		queryStr,
		start,
		end,
		step,
		interval,
		direction,
		uint32(limit),
		nil,
	)

	query := f.engine.Query(params)

	result, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	value, err := marshal.NewResultValue(result.Data)
	if err != nil {
		return nil, err
	}

	return &loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: value.Type(),
			Result:     value,
			Statistics: result.Statistics,
		},
	}, nil
}

func (f *FileClient) ListLabelNames(quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	return &loghttp.LabelResponse{}, nil
}

func (f *FileClient) ListLabelValues(name string, quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	return &loghttp.LabelResponse{}, nil
}

func (f *FileClient) Series(matchers []string, start, end time.Time, quiet bool) (*loghttp.SeriesResponse, error) {
	return &loghttp.SeriesResponse{}, nil
}
func (f *FileClient) LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error) {
	panic("Not supported")
}

func (f *FileClient) GetOrgID() string {
	return f.orgID
}

// Tripperware can wrap a roundtripper.
type Tripperware func(http.RoundTripper) http.RoundTripper

// Client contains fields necessary to query a Loki instance
type DefaultClient struct {
	TLSConfig       config.TLSConfig
	Username        string
	Password        string
	Address         string
	OrgID           string
	Tripperware     Tripperware
	BearerToken     string
	BearerTokenFile string
	Retries         int
}

// Query uses the /api/v1/query endpoint to execute an instant query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *DefaultClient) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", queryStr)
	qsb.SetInt("limit", int64(limit))
	qsb.SetInt("time", time.UnixNano())
	qsb.SetString("direction", direction.String())

	return c.doQuery(queryPath, qsb.Encode(), quiet)
}

// QueryRange uses the /api/v1/query_range endpoint to execute a range query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *DefaultClient) QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error) {
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

	return c.doQuery(queryRangePath, params.Encode(), quiet)
}

// ListLabelNames uses the /api/v1/label endpoint to list label names
func (c *DefaultClient) ListLabelNames(quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())

	if err := c.doRequest(labelsPath, params.Encode(), quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// ListLabelValues uses the /api/v1/label endpoint to list label values
func (c *DefaultClient) ListLabelValues(name string, quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	if err := c.doRequest(path, params.Encode(), quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *DefaultClient) Series(matchers []string, start, end time.Time, quiet bool) (*loghttp.SeriesResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	params.SetStringArray("match", matchers)

	var seriesResponse loghttp.SeriesResponse
	if err := c.doRequest(seriesPath, params.Encode(), quiet, &seriesResponse); err != nil {
		return nil, err
	}
	return &seriesResponse, nil
}

// LiveTailQueryConn uses /api/prom/tail to set up a websocket connection and returns it
func (c *DefaultClient) LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error) {
	params := util.NewQueryStringBuilder()
	params.SetString("query", queryStr)
	if delayFor != 0 {
		params.SetInt("delay_for", int64(delayFor.Seconds()))
	}
	params.SetInt("limit", int64(limit))
	params.SetInt("start", start.UnixNano())

	return c.wsConnect(tailPath, params.Encode(), quiet)
}

func (c *DefaultClient) GetOrgID() string {
	return c.OrgID
}

func (c *DefaultClient) doQuery(path string, query string, quiet bool) (*loghttp.QueryResponse, error) {
	var err error
	var r loghttp.QueryResponse

	if err = c.doRequest(path, query, quiet, &r); err != nil {
		return nil, err
	}

	return &r, nil
}

func (c *DefaultClient) doRequest(path, query string, quiet bool, out interface{}) error {
	us, err := buildURL(c.Address, path, query)
	if err != nil {
		return err
	}
	if !quiet {
		log.Print(us)
	}

	req, err := http.NewRequest("GET", us, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("User-Agent", userAgent)

	if c.OrgID != "" {
		req.Header.Set("X-Scope-OrgID", c.OrgID)
	}

	if (c.Username != "" || c.Password != "") && (len(c.BearerToken) > 0 || len(c.BearerTokenFile) > 0) {
		return fmt.Errorf("at most one of HTTP basic auth (username/password), bearer-token & bearer-token-file is allowed to be configured")
	}

	if len(c.BearerToken) > 0 && len(c.BearerTokenFile) > 0 {
		return fmt.Errorf("at most one of the options bearer-token & bearer-token-file is allowed to be configured")
	}

	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}

	if c.BearerTokenFile != "" {
		b, err := ioutil.ReadFile(c.BearerTokenFile)
		if err != nil {
			return fmt.Errorf("unable to read authorization credentials file %s: %s", c.BearerTokenFile, err)
		}
		bearerToken := strings.TrimSpace(string(b))
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	// Parse the URL to extract the host
	clientConfig := config.HTTPClientConfig{
		TLSConfig: c.TLSConfig,
	}

	client, err := config.NewClientFromConfig(clientConfig, "promtail", config.WithHTTP2Disabled())
	if err != nil {
		return err
	}
	if c.Tripperware != nil {
		client.Transport = c.Tripperware(client.Transport)
	}

	var resp *http.Response
	attempts := c.Retries + 1
	success := false

	for attempts > 0 {
		attempts--

		resp, err = client.Do(req)
		if err != nil {
			log.Println("error sending request", err)
			continue
		}
		if resp.StatusCode/100 != 2 {
			buf, _ := ioutil.ReadAll(resp.Body) // nolint
			log.Printf("Error response from server: %s (%v) attempts remaining: %d", string(buf), err, attempts)
			if err := resp.Body.Close(); err != nil {
				log.Println("error closing body", err)
			}
			continue
		}
		success = true
		break
	}
	if !success {
		return fmt.Errorf("Run out of attempts while querying the server")
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("error closing body", err)
		}
	}()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *DefaultClient) wsConnect(path, query string, quiet bool) (*websocket.Conn, error) {
	us, err := buildURL(c.Address, path, query)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := config.NewTLSConfig(&c.TLSConfig)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(us, "https") {
		us = strings.Replace(us, "https", "wss", 1)
	} else if strings.HasPrefix(us, "http") {
		us = strings.Replace(us, "http", "ws", 1)
	}
	if !quiet {
		log.Println(us)
	}

	h := http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password))}}

	if c.OrgID != "" {
		h.Set("X-Scope-OrgID", c.OrgID)
	}

	if (c.Username != "" || c.Password != "") && (len(c.BearerToken) > 0 || len(c.BearerTokenFile) > 0) {
		return nil, fmt.Errorf("at most one of HTTP basic auth (username/password), bearer-token & bearer-token-file is allowed to be configured")
	}

	if len(c.BearerToken) > 0 && len(c.BearerTokenFile) > 0 {
		return nil, fmt.Errorf("at most one of the options bearer-token & bearer-token-file is allowed to be configured")
	}

	if c.BearerToken != "" {
		h.Set("Authorization", "Bearer "+c.BearerToken)
	}

	if c.BearerTokenFile != "" {
		b, err := ioutil.ReadFile(c.BearerTokenFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read authorization credentials file %s: %s", c.BearerTokenFile, err)
		}
		bearerToken := strings.TrimSpace(string(b))
		h.Set("Authorization", "Bearer "+bearerToken)
	}

	ws := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	conn, resp, err := ws.Dial(us, h)
	if err != nil {
		if resp == nil {
			return nil, err
		}
		buf, _ := ioutil.ReadAll(resp.Body) // nolint
		return nil, fmt.Errorf("Error response from server: %s (%v)", string(buf), err)
	}

	return conn, nil
}

// buildURL concats a url `http://foo/bar` with a path `/buzz`.
func buildURL(u, p, q string) (string, error) {
	url, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	url.Path = path.Join(url.Path, p)
	url.RawQuery = q
	return url.String(), nil
}
