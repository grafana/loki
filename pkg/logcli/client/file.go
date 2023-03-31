package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	logqllog "github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/validation"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"
)

const (
	defaultLabelKey          = "source"
	defaultLabelValue        = "logcli"
	defaultOrgID             = "logcli"
	defaultMetricSeriesLimit = 1024
	defaultMaxFileSize       = 20 * (1 << 20) // 20MB
)

var ErrNotSupported = errors.New("not supported")

// FileClient is a type of LogCLI client that do LogQL on log lines from
// the given file directly, instead get log lines from Loki servers.
type FileClient struct {
	r           io.ReadCloser
	labels      []string
	labelValues []string
	orgID       string
	engine      *logql.Engine
}

// NewFileClient returns the new instance of FileClient for the given `io.ReadCloser`
func NewFileClient(r io.ReadCloser) *FileClient {
	lbs := []labels.Label{
		{
			Name:  defaultLabelKey,
			Value: defaultLabelValue,
		},
	}

	eng := logql.NewEngine(logql.EngineOpts{}, &querier{r: r, labels: lbs}, &limiter{n: defaultMetricSeriesLimit}, log.Logger)
	return &FileClient{
		r:           r,
		orgID:       defaultOrgID,
		engine:      eng,
		labels:      []string{defaultLabelKey},
		labelValues: []string{defaultLabelValue},
	}
}

func (f *FileClient) Query(q string, limit int, t time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	ctx := context.Background()

	ctx = user.InjectOrgID(ctx, f.orgID)

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

	ctx = user.InjectOrgID(ctx, f.orgID)

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
	return &loghttp.LabelResponse{
		Status: loghttp.QueryStatusSuccess,
		Data:   f.labels,
	}, nil
}

func (f *FileClient) ListLabelValues(name string, quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	i := sort.SearchStrings(f.labels, name)
	if i < 0 {
		return &loghttp.LabelResponse{}, nil
	}

	return &loghttp.LabelResponse{
		Status: loghttp.QueryStatusSuccess,
		Data:   []string{f.labelValues[i]},
	}, nil
}

func (f *FileClient) Series(matchers []string, start, end time.Time, quiet bool) (*loghttp.SeriesResponse, error) {
	m := len(f.labels)
	if m > len(f.labelValues) {
		m = len(f.labelValues)
	}

	lbs := make(loghttp.LabelSet)
	for i := 0; i < m; i++ {
		lbs[f.labels[i]] = f.labelValues[i]
	}

	return &loghttp.SeriesResponse{
		Status: loghttp.QueryStatusSuccess,
		Data:   []loghttp.LabelSet{lbs},
	}, nil
}

func (f *FileClient) LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error) {
	return nil, fmt.Errorf("LiveTailQuery: %w", ErrNotSupported)
}

func (f *FileClient) GetOrgID() string {
	return f.orgID
}

type limiter struct {
	n int
}

func (l *limiter) MaxQuerySeries(ctx context.Context, userID string) int {
	return l.n
}

func (l *limiter) MaxQueryRange(ctx context.Context, userID string) time.Duration {
	return 0 * time.Second
}

func (l *limiter) QueryTimeout(ctx context.Context, userID string) time.Duration {
	return time.Minute * 5
}

func (l *limiter) BlockedQueries(ctx context.Context, userID string) []*validation.BlockedQuery {
	return []*validation.BlockedQuery{}
}

func (l *limiter) RequiredLabels(ctx context.Context, userID string) []string {
	return nil
}

type querier struct {
	r      io.Reader
	labels labels.Labels
}

func (q *querier) SelectLogs(_ context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	expr, err := params.LogSelector()
	if err != nil {
		return nil, fmt.Errorf("failed to extract selector for logs: %w", err)
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, fmt.Errorf("failed to extract pipeline for logs: %w", err)
	}
	return newFileIterator(q.r, params, pipeline.ForStream(q.labels))
}

func (q *querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, fmt.Errorf("Metrics Query: %w", ErrNotSupported)
}

func newFileIterator(
	r io.Reader,
	params logql.SelectLogParams,
	pipeline logqllog.StreamPipeline,
) (iter.EntryIterator, error) {

	lr := io.LimitReader(r, defaultMaxFileSize)
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	lines := strings.FieldsFunc(string(b), func(r rune) bool {
		return r == '\n'
	})

	if len(lines) == 0 {
		return iter.NoopIterator, nil
	}

	streams := map[uint64]*logproto.Stream{}

	processLine := func(line string) {
		ts := time.Now()
		parsedLine, parsedLabels, matches := pipeline.ProcessString(ts.UnixNano(), line)
		if !matches {
			return
		}

		var stream *logproto.Stream
		lhash := parsedLabels.Hash()
		var ok bool
		if stream, ok = streams[lhash]; !ok {
			stream = &logproto.Stream{
				Labels: parsedLabels.String(),
			}
			streams[lhash] = stream
		}

		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: ts,
			Line:      parsedLine,
		})
	}

	if params.Direction == logproto.FORWARD {
		for _, line := range lines {
			processLine(line)
		}
	} else {
		for i := len(lines) - 1; i >= 0; i-- {
			processLine(lines[i])
		}
	}

	if len(streams) == 0 {
		return iter.NoopIterator, nil
	}

	streamResult := make([]logproto.Stream, 0, len(streams))

	for _, stream := range streams {
		streamResult = append(streamResult, *stream)
	}

	return iter.NewStreamsIterator(
		streamResult,
		params.Direction,
	), nil
}
