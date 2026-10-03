package correctness

import (
	"context"
	"fmt"
	"io"
	"math/rand" //#nosec G404 -- Cycle sampling is not security-sensitive. -- nosemgrep: math-random-used
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	lokiclient "github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

const (
	skipBeforeStart   = "before_start"
	skipNoLabels      = "no_labels"
	skipNoLabelValues = "no_label_values"
	skipNoLogs        = "no_logs"
	skipNoNeedle      = "no_needle"
)

var tokenCandidateRE = regexp.MustCompile(`[A-Za-z0-9._:/@%?-]{6,128}`)

// Line is one log line sampled from a query response.
type Line struct {
	Timestamp time.Time
	Line      string
	Labels    string
}

// Querier is the subset of the Loki HTTP API this checker uses.
type Querier interface {
	LabelNames(ctx context.Context, start, end time.Time) ([]string, error)
	LabelValues(ctx context.Context, name string, start, end time.Time) ([]string, error)
	Query(ctx context.Context, query string, start, end time.Time, limit int) ([]Line, error)
}

// Report is one verification cycle.
type Report struct {
	Selector      string
	Needle        string
	Sample        Line
	Correct       bool
	SkippedReason string
}

// Service samples the stable write path and checks the experimental querier.
type Service struct {
	services.Service

	cfg     Config
	stable  Querier
	exp     Querier
	logger  log.Logger
	metrics *Metrics

	startedAt time.Time
	nowFn     func() time.Time
	sleep     func(context.Context, time.Duration) error

	rand *rand.Rand
}

// New creates a correctness service that queries the configured endpoints.
func New(cfg Config, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	stable, err := newHTTPQuerier(cfg.StableEndpoint, cfg)
	if err != nil {
		return nil, fmt.Errorf("stable querier: %w", err)
	}
	exp, err := newHTTPQuerier(cfg.ExpEndpoint, cfg)
	if err != nil {
		return nil, fmt.Errorf("exp querier: %w", err)
	}
	return newService(cfg, stable, exp, logger, reg), nil
}

func newService(cfg Config, stable, exp Querier, logger log.Logger, reg prometheus.Registerer) *Service {
	startedAt := time.Now().UTC()
	if !cfg.StartedAt.IsZero() {
		startedAt = cfg.StartedAt.UTC()
	}
	s := &Service{
		cfg:       cfg,
		stable:    stable,
		exp:       exp,
		logger:    logger,
		metrics:   NewMetrics(reg),
		startedAt: startedAt,
		nowFn:     func() time.Time { return time.Now().UTC() },
		rand:      rand.New(rand.NewSource(time.Now().UnixNano())), //#nosec G404 -- Cycle sampling is not security-sensitive. -- nosemgrep: math-random-used
	}
	s.sleep = s.wait
	s.Service = services.NewBasicService(nil, s.running, nil)
	return s
}

func (s *Service) running(ctx context.Context) error {
	level.Info(s.logger).Log(
		"msg", "chunk exp correctness starting",
		"stable_endpoint", s.cfg.StableEndpoint,
		"exp_endpoint", s.cfg.ExpEndpoint,
		"tenant", s.cfg.TenantID,
		"started_at", s.startedAt,
		"lag_grace", s.cfg.LagGrace,
	)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}

		s.metrics.cyclesTotal.Inc()
		start := s.nowFn()
		cycleCtx, cancel := context.WithTimeout(ctx, s.cfg.CycleTimeout)
		report, err := s.runCycle(cycleCtx)
		cancel()
		s.metrics.cycleDuration.Observe(s.nowFn().Sub(start).Seconds())

		if err != nil {
			s.metrics.cycleErrorsTotal.Inc()
			level.Error(s.logger).Log("msg", "correctness cycle failed", "err", err, "selector", report.Selector, "needle", report.Needle)
		} else if report.SkippedReason != "" {
			s.metrics.cycleSkippedTotal.WithLabelValues(report.SkippedReason).Inc()
			level.Info(s.logger).Log("msg", "correctness cycle skipped", "reason", report.SkippedReason)
		} else if report.Correct {
			s.metrics.testsCorrectTotal.Inc()
			s.metrics.lastCorrectTestTs.Set(float64(s.nowFn().Unix()))
			level.Info(s.logger).Log("msg", "correctness cycle completed", "selector", report.Selector, "needle", report.Needle, "correct", true)
		} else {
			s.metrics.testsIncorrectTotal.Inc()
			s.metrics.lastIncorrectTestTs.Set(float64(s.nowFn().Unix()))
			level.Warn(s.logger).Log(
				"msg", "correctness failure",
				"selector", report.Selector,
				"needle", report.Needle,
				"timestamp", report.Sample.Timestamp.Format(time.RFC3339Nano),
				"labels", report.Sample.Labels,
			)
		}

		timer.Reset(s.cfg.QueryInterval)
	}
}

func (s *Service) runCycle(ctx context.Context) (Report, error) {
	var report Report
	now := s.nowFn()
	start, end, skip := s.window(now)
	if skip != "" {
		report.SkippedReason = skip
		return report, nil
	}

	names, err := s.stable.LabelNames(ctx, start, end)
	if err != nil {
		return report, err
	}
	names = filterLabels(names)
	if len(names) == 0 {
		report.SkippedReason = skipNoLabels
		return report, nil
	}
	s.shuffle(names)

	var (
		selector    string
		lines       []Line
		foundValues bool
	)
	for _, name := range names {
		values, valuesErr := s.stable.LabelValues(ctx, name, start, end)
		if valuesErr != nil {
			return report, valuesErr
		}
		if len(values) == 0 {
			continue
		}
		foundValues = true
		value := values[s.randIntn(len(values))]
		selector = buildSelector(name, value)
		lines, err = s.stable.Query(ctx, selector, start, end, s.cfg.LogQueryLimit)
		if err != nil {
			return report, err
		}
		if len(lines) > 0 {
			break
		}
		selector = ""
	}
	if selector == "" {
		if !foundValues {
			report.SkippedReason = skipNoLabelValues
		} else {
			report.SkippedReason = skipNoLogs
		}
		return report, nil
	}
	report.Selector = selector
	if len(lines) == 0 {
		report.SkippedReason = skipNoLogs
		return report, nil
	}

	sample, needle, ok := s.pickSample(lines)
	if !ok {
		report.SkippedReason = skipNoNeedle
		return report, nil
	}
	report.Sample = sample
	report.Needle = needle
	query := selector + " |= " + fmt.Sprintf("%q", needle)

	deadline := s.nowFn().Add(s.cfg.LagGrace)
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		// Search only the sampled instant. A window query is oldest-first and
		// truncated at LogQueryLimit, so a common needle can hide the sample.
		got, err := s.exp.Query(ctx, query, sample.Timestamp, sample.Timestamp.Add(time.Millisecond), s.cfg.LogQueryLimit)
		if err != nil {
			return report, err
		}
		if containsLine(got, sample) {
			report.Correct = true
			return report, nil
		}
		if !s.nowFn().Before(deadline) {
			return report, nil
		}
		if err := s.sleep(ctx, s.cfg.RetryInterval); err != nil {
			return report, err
		}
	}
}

func (s *Service) window(now time.Time) (time.Time, time.Time, string) {
	end := now.Add(-s.cfg.Settle)
	earliest := s.startedAt
	if lookback := now.Add(-s.cfg.MaxLookback); lookback.After(earliest) {
		earliest = lookback
	}
	if !end.After(earliest) {
		return time.Time{}, time.Time{}, skipBeforeStart
	}
	return earliest, end, ""
}

func (s *Service) pickSample(lines []Line) (Line, string, bool) {
	order := append([]Line(nil), lines...)
	s.rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	var best Line
	var bestNeedle string
	for _, line := range order {
		tokens := tokenCandidateRE.FindAllString(line.Line, -1)
		for _, token := range tokens {
			if len(token) > len(bestNeedle) {
				best = line
				bestNeedle = token
			}
		}
		if bestNeedle != "" {
			return best, bestNeedle, true
		}
	}
	return Line{}, "", false
}

func (s *Service) wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) shuffle(values []string) {
	s.rand.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
}

func (s *Service) randIntn(n int) int {
	if n <= 1 {
		return 0
	}
	return s.rand.Intn(n)
}

func containsLine(lines []Line, want Line) bool {
	for _, line := range lines {
		if line.Line == want.Line && line.Labels == want.Labels && line.Timestamp.Equal(want.Timestamp) {
			return true
		}
	}
	return false
}

func filterLabels(names []string) []string {
	out := names[:0]
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "__") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func buildSelector(label, value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return fmt.Sprintf(`{%s="%s"}`, label, escaped)
}

type httpQuerier struct {
	client *lokiclient.DefaultClient
}

func newHTTPQuerier(address string, cfg Config) (*httpQuerier, error) {
	client := &lokiclient.DefaultClient{
		Address: address,
		OrgID:   cfg.TenantID,
		Retries: 0,
		BackoffConfig: lokiclient.BackoffConfig{
			MinBackoff: 1,
			MaxBackoff: 1,
		},
		Tripperware: func(next http.RoundTripper) http.RoundTripper {
			return timeoutRoundTripper{next: next, timeout: cfg.RequestTimeout}
		},
	}
	return &httpQuerier{client: client}, nil
}

func (q *httpQuerier) LabelNames(_ context.Context, start, end time.Time) ([]string, error) {
	resp, err := q.client.ListLabelNames(true, start, end)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("nil label response")
	}
	return resp.Data, nil
}

func (q *httpQuerier) LabelValues(_ context.Context, name string, start, end time.Time) ([]string, error) {
	resp, err := q.client.ListLabelValues(name, true, start, end)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("nil label values response")
	}
	return resp.Data, nil
}

func (q *httpQuerier) Query(_ context.Context, query string, start, end time.Time, limit int) ([]Line, error) {
	resp, err := q.client.QueryRange(query, limit, start, end, logproto.FORWARD, 0, 0, true)
	if err != nil {
		return nil, err
	}
	return linesFromResponse(resp)
}

func linesFromResponse(resp *loghttp.QueryResponse) ([]Line, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil query response")
	}
	if resp.Data.Result == nil {
		return nil, nil
	}
	streams, ok := resp.Data.Result.(loghttp.Streams)
	if !ok {
		return nil, fmt.Errorf("unexpected query result type %T", resp.Data.Result)
	}
	var lines []Line
	for _, stream := range streams {
		labels := stream.Labels.String()
		for _, entry := range stream.Entries {
			lines = append(lines, Line{
				Timestamp: entry.Timestamp.UTC(),
				Line:      entry.Line,
				Labels:    labels,
			})
		}
	}
	return lines, nil
}

type timeoutRoundTripper struct {
	next    http.RoundTripper
	timeout time.Duration
}

func (t timeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	cancel := func() {}
	if t.timeout > 0 {
		var cancelFn context.CancelFunc
		ctx, cancelFn = context.WithTimeout(ctx, t.timeout)
		cancel = cancelFn
	}
	req = req.Clone(ctx)
	req.Header.Set(httpreq.LokiEncodingFlagsHeader, string(httpreq.FlagCategorizeLabels))
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel func()
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
