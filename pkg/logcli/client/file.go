package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	logqllog "github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"
)

const (
	defaultLabelKey          = "source"
	defaultLabelValue        = "logcli"
	defaultOrgID             = "logcli"
	defaultMetricSeriesLimit = 1024
	defaultMaxFileSize       = 20 * (1 << 20) // 20MB
)

var (
	ErrNotSupported = errors.New("not supported")
)

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

	eng := logql.NewEngine(logql.EngineOpts{}, &querier{r: r, labels: lbs}, &limiter{n: defaultMetricSeriesLimit})
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

func (l *limiter) MaxQuerySeries(userID string) int {
	return l.n
}

type querier struct {
	r      io.Reader
	labels labels.Labels
}

func (q *querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	expr, err := params.LogSelector()
	if err != nil {
		return nil, fmt.Errorf("failed to extract selector for logs: %w", err)
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, fmt.Errorf("failed to extract pipeline for logs: %w", err)
	}
	return newFileIterator(ctx, q.r, q.labels, params, pipeline.ForStream(q.labels))
}

func (q *querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, fmt.Errorf("Metrics Query: %w", ErrNotSupported)
	// expr, err := params.Expr()
	// if err != nil {
	// 	panic(err)
	// }

	// sampleExtractor, err := expr.Extractor()
	// if err != nil {
	// 	panic(err)
	// }

	// streamSample := sampleExtractor.ForStream(labels.Labels{
	// 	labels.Label{Name: "foo", Value: "bar"},
	// })

	// it := NewFileSampleIterator(q.r, params.Start, params.End, "source:logcli", streamSample)

	// return it, nil
}

// type FileSampleIterator struct {
// 	s          *bufio.Scanner
// 	labels     string
// 	err        error
// 	sp         logqllog.StreamSampleExtractor
// 	curr       logproto.Sample
// 	start, end time.Time
// }

// func NewFileSampleIterator(r io.Reader, start, end time.Time, labels string, sp logqllog.StreamSampleExtractor) *FileSampleIterator {
// 	s := bufio.NewScanner(r)
// 	s.Split(bufio.ScanLines)
// 	return &FileSampleIterator{
// 		s:      s,
// 		labels: labels,
// 		sp:     sp,
// 		start:  start,
// 		end:    end,
// 	}
// }

// func (f *FileSampleIterator) Next() bool {
// 	for f.s.Scan() {
// 		value, _, ok := f.sp.Process([]byte(f.s.Text()))
// 		ts := f.start.Add(2 * time.Minute)
// 		if ok {
// 			f.curr = logproto.Sample{
// 				Timestamp: ts.UnixNano(),
// 				Value:     value,
// 				Hash:      xxhash.Sum64(f.s.Bytes()),
// 			}
// 			return true
// 		}
// 	}
// 	return false
// }

// func (f *FileSampleIterator) Sample() logproto.Sample {
// 	return f.curr
// }

// func (f *FileSampleIterator) Labels() string {
// 	return f.labels
// }

// func (f *FileSampleIterator) Error() error {
// 	return f.err
// }

// func (f *FileSampleIterator) Close() error {
// 	return nil
// }

// this is the generated timestamp for each input log line based on start and end
// of the query.
// start and end are unix timestamps in secs.
func assignTimestamps(lines []string, start, end int64) map[string]int64 {
	res := make(map[string]int64)

	n := int64(len(lines))

	if end < start {
		panic("`start` cannot be after `end`")
	}

	step := (end - start) / n

	for i, line := range lines {
		fmt.Println("line", line, "ts", (step*int64(i+1) + start))
		res[line] = (step * int64(i+1)) + start
	}

	return res
}

func newFileIterator(
	ctx context.Context,
	r io.Reader,
	labels labels.Labels,
	params logql.SelectLogParams,
	pipeline logqllog.StreamPipeline,
) (iter.EntryIterator, error) {

	lr := io.LimitReader(r, defaultMaxFileSize)
	b, err := ioutil.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	lines := strings.FieldsFunc(string(b), func(r rune) bool {
		if r == '\n' {
			return true
		}
		return false
	})

	if len(lines) == 0 {
		return iter.NoopIterator, nil
	}

	stream := logproto.Stream{
		Labels: labels.String(),
	}

	// fmt.Println("directions", params.Direction)

	// reverse all the input lines if direction == FORWARD
	if params.Direction == logproto.FORWARD {
		sort.Slice(lines, func(i, j int) bool {
			return i > j
		})
	}

	// timestamps := assignTimestamps(lines, params.Start.Unix(), params.End.Unix())

	for _, line := range lines {
		parsedLine, _, ok := pipeline.ProcessString(line)
		if !ok {
			continue
		}
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: time.Now(),
			// Timestamp: time.Unix(timestamps[line], 0),
			Line: parsedLine,
		})
	}

	return iter.NewHeapIterator(
		ctx,
		[]iter.EntryIterator{iter.NewStreamIterator(stream)},
		params.Direction,
	), nil
}
