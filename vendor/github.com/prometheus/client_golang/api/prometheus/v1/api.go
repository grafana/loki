// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package v1 provides bindings to the Prometheus HTTP API v1:
// http://prometheus.io/docs/querying/api/
package v1

import (
	"context"
	gojson "encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"github.com/prometheus/client_golang/api"
)

const (
	apiPrefix = "/api/v1"

	epAlerts          = apiPrefix + "/alerts"
	epAlertManagers   = apiPrefix + "/alertmanagers"
	epQuery           = apiPrefix + "/query"
	epQueryRange      = apiPrefix + "/query_range"
	epQueryExemplars  = apiPrefix + "/query_exemplars"
	epLabels          = apiPrefix + "/labels"
	epLabelValues     = apiPrefix + "/label/:name/values"
	epSeries          = apiPrefix + "/series"
	epTargets         = apiPrefix + "/targets"
	epTargetsMetadata = apiPrefix + "/targets/metadata"
	epMetadata        = apiPrefix + "/metadata"
	epRules           = apiPrefix + "/rules"
	epSnapshot        = apiPrefix + "/admin/tsdb/snapshot"
	epDeleteSeries    = apiPrefix + "/admin/tsdb/delete_series"
	epCleanTombstones = apiPrefix + "/admin/tsdb/clean_tombstones"
	epConfig          = apiPrefix + "/status/config"
	epFlags           = apiPrefix + "/status/flags"
	epBuildinfo       = apiPrefix + "/status/buildinfo"
	epRuntimeinfo     = apiPrefix + "/status/runtimeinfo"
	epTSDB            = apiPrefix + "/status/tsdb"
	epTSDBBlocks      = apiPrefix + "/status/tsdb/blocks"
	epWalReplay       = apiPrefix + "/status/walreplay"
	epFormatQuery     = apiPrefix + "/format_query"
)

// AlertState models the state of an alert.
type AlertState string

// ErrorType models the different API error types.
type ErrorType string

// HealthStatus models the health status of a scrape target.
type HealthStatus string

// RuleType models the type of a rule.
type RuleType string

// RuleHealth models the health status of a rule.
type RuleHealth string

// MetricType models the type of a metric.
type MetricType string

const (
	// Possible values for AlertState.
	AlertStateFiring   AlertState = "firing"
	AlertStateInactive AlertState = "inactive"
	AlertStatePending  AlertState = "pending"

	// Possible values for ErrorType.
	ErrBadData     ErrorType = "bad_data"
	ErrTimeout     ErrorType = "timeout"
	ErrCanceled    ErrorType = "canceled"
	ErrExec        ErrorType = "execution"
	ErrBadResponse ErrorType = "bad_response"
	ErrServer      ErrorType = "server_error"
	ErrClient      ErrorType = "client_error"

	// Possible values for HealthStatus.
	HealthGood    HealthStatus = "up"
	HealthUnknown HealthStatus = "unknown"
	HealthBad     HealthStatus = "down"

	// Possible values for RuleType.
	RuleTypeRecording RuleType = "recording"
	RuleTypeAlerting  RuleType = "alerting"

	// Possible values for RuleHealth.
	RuleHealthGood    = "ok"
	RuleHealthUnknown = "unknown"
	RuleHealthBad     = "err"

	// Possible values for MetricType
	MetricTypeCounter        MetricType = "counter"
	MetricTypeGauge          MetricType = "gauge"
	MetricTypeHistogram      MetricType = "histogram"
	MetricTypeGaugeHistogram MetricType = "gaugehistogram"
	MetricTypeSummary        MetricType = "summary"
	MetricTypeInfo           MetricType = "info"
	MetricTypeStateset       MetricType = "stateset"
	MetricTypeUnknown        MetricType = "unknown"
)

// Error is an error returned by the API.
type Error struct {
	Type   ErrorType
	Msg    string
	Detail string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Msg)
}

// Range represents a sliced time range.
type Range struct {
	// The boundaries of the time range.
	Start, End time.Time
	// The maximum time between two slices within the boundaries.
	Step time.Duration
}

// API provides bindings for Prometheus's v1 API.
type API interface {
	// Alerts returns a list of all active alerts.
	Alerts(ctx context.Context) (AlertsResult, error)
	// AlertManagers returns an overview of the current state of the Prometheus alert manager discovery.
	AlertManagers(ctx context.Context) (AlertManagersResult, error)
	// CleanTombstones removes the deleted data from disk and cleans up the existing tombstones.
	CleanTombstones(ctx context.Context) error
	// Config returns the current Prometheus configuration.
	Config(ctx context.Context) (ConfigResult, error)
	// DeleteSeries deletes data for a selection of series in a time range.
	DeleteSeries(ctx context.Context, matches []string, startTime, endTime time.Time) error
	// Flags returns the flag values that Prometheus was launched with.
	Flags(ctx context.Context) (FlagsResult, error)
	// LabelNames returns the unique label names present in the block in sorted order by given time range and matchers.
	LabelNames(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...Option) (model.LabelNames, Warnings, Infos, error)
	// LabelValues performs a query for the values of the given label, time range and matchers.
	LabelValues(ctx context.Context, label string, matches []string, startTime, endTime time.Time, opts ...Option) (model.LabelValues, Warnings, Infos, error)
	// Query performs a query for the given time.
	Query(ctx context.Context, query string, ts time.Time, opts ...Option) (model.Value, Warnings, Infos, error)
	// QueryRange performs a query for the given range.
	QueryRange(ctx context.Context, query string, r Range, opts ...Option) (model.Value, Warnings, Infos, error)
	// QueryExemplars performs a query for exemplars by the given query and time range.
	QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]ExemplarQueryResult, error)
	// Buildinfo returns various build information properties about the Prometheus server
	Buildinfo(ctx context.Context) (BuildinfoResult, error)
	// Runtimeinfo returns the various runtime information properties about the Prometheus server.
	Runtimeinfo(ctx context.Context) (RuntimeinfoResult, error)
	// Series finds series by label matchers.
	Series(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...Option) ([]model.LabelSet, Warnings, Infos, error)
	// Snapshot creates a snapshot of all current data into snapshots/<datetime>-<rand>
	// under the TSDB's data directory and returns the directory as response.
	Snapshot(ctx context.Context, skipHead bool) (SnapshotResult, error)
	// Rules returns a list of alerting and recording rules that are currently loaded.
	Rules(ctx context.Context, matches []string) (RulesResult, error)
	// Targets returns an overview of the current state of the Prometheus target discovery.
	Targets(ctx context.Context) (TargetsResult, error)
	// TargetsMetadata returns metadata about metrics currently scraped by the target.
	TargetsMetadata(ctx context.Context, matchTarget, metric, limit string) ([]MetricMetadata, error)
	// Metadata returns metadata about metrics currently scraped by the metric name.
	Metadata(ctx context.Context, metric, limit string) (map[string][]Metadata, error)
	// TSDB returns the cardinality statistics.
	TSDB(ctx context.Context, opts ...Option) (TSDBResult, error)
	// TSDBBlocks returns the list of currently loaded TSDB blocks and their metadata.
	TSDBBlocks(ctx context.Context) (TSDBBlocksResult, error)
	// WalReplay returns the current replay status of the wal.
	WalReplay(ctx context.Context) (WalReplayStatus, error)
	// FormatQuery formats a PromQL expression in a prettified way.
	FormatQuery(ctx context.Context, query string) (string, error)
}

// AlertsResult contains the result from querying the alerts endpoint.
type AlertsResult struct {
	Alerts []Alert `json:"alerts"`
}

// AlertManagersResult contains the result from querying the alertmanagers endpoint.
type AlertManagersResult struct {
	Active  []AlertManager `json:"activeAlertManagers"`
	Dropped []AlertManager `json:"droppedAlertManagers"`
}

// AlertManager models a configured Alert Manager.
type AlertManager struct {
	URL string `json:"url"`
}

// ConfigResult contains the result from querying the config endpoint.
type ConfigResult struct {
	YAML string `json:"yaml"`
}

// FlagsResult contains the result from querying the flag endpoint.
type FlagsResult map[string]string

// BuildinfoResult contains the results from querying the buildinfo endpoint.
type BuildinfoResult struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Branch    string `json:"branch"`
	BuildUser string `json:"buildUser"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}

// RuntimeinfoResult contains the result from querying the runtimeinfo endpoint.
type RuntimeinfoResult struct {
	StartTime           time.Time `json:"startTime"`
	CWD                 string    `json:"CWD"`
	ReloadConfigSuccess bool      `json:"reloadConfigSuccess"`
	LastConfigTime      time.Time `json:"lastConfigTime"`
	CorruptionCount     int       `json:"corruptionCount"`
	GoroutineCount      int       `json:"goroutineCount"`
	GOMAXPROCS          int       `json:"GOMAXPROCS"`
	GOGC                string    `json:"GOGC"`
	GODEBUG             string    `json:"GODEBUG"`
	StorageRetention    string    `json:"storageRetention"`
}

// SnapshotResult contains the result from querying the snapshot endpoint.
type SnapshotResult struct {
	Name string `json:"name"`
}

// RulesResult contains the result from querying the rules endpoint.
type RulesResult struct {
	Groups []RuleGroup `json:"groups"`
}

// RuleGroup models a rule group that contains a set of recording and alerting rules.
type RuleGroup struct {
	Name     string  `json:"name"`
	File     string  `json:"file"`
	Interval float64 `json:"interval"`
	Rules    Rules   `json:"rules"`
}

// Recording and alerting rules are stored in the same slice to preserve the order
// that rules are returned in by the API.
//
// Rule types can be determined using a type switch:
//
//	switch v := rule.(type) {
//	case RecordingRule:
//		fmt.Print("got a recording rule")
//	case AlertingRule:
//		fmt.Print("got a alerting rule")
//	default:
//		fmt.Printf("unknown rule type %s", v)
//	}
type Rules []any

// AlertingRule models a alerting rule.
type AlertingRule struct {
	Name           string         `json:"name"`
	Query          string         `json:"query"`
	Duration       float64        `json:"duration"`
	Labels         model.LabelSet `json:"labels"`
	Annotations    model.LabelSet `json:"annotations"`
	Alerts         []*Alert       `json:"alerts"`
	Health         RuleHealth     `json:"health"`
	LastError      string         `json:"lastError,omitempty"`
	EvaluationTime float64        `json:"evaluationTime"`
	LastEvaluation time.Time      `json:"lastEvaluation"`
	State          string         `json:"state"`
}

// RecordingRule models a recording rule.
type RecordingRule struct {
	Name           string         `json:"name"`
	Query          string         `json:"query"`
	Labels         model.LabelSet `json:"labels,omitempty"`
	Health         RuleHealth     `json:"health"`
	LastError      string         `json:"lastError,omitempty"`
	EvaluationTime float64        `json:"evaluationTime"`
	LastEvaluation time.Time      `json:"lastEvaluation"`
}

// Alert models an active alert.
type Alert struct {
	ActiveAt    time.Time `json:"activeAt"`
	Annotations model.LabelSet
	Labels      model.LabelSet
	State       AlertState
	Value       string
}

// TargetsResult contains the result from querying the targets endpoint.
type TargetsResult struct {
	Active  []ActiveTarget  `json:"activeTargets"`
	Dropped []DroppedTarget `json:"droppedTargets"`
}

// ActiveTarget models an active Prometheus scrape target.
type ActiveTarget struct {
	DiscoveredLabels   map[string]string `json:"discoveredLabels"`
	Labels             model.LabelSet    `json:"labels"`
	ScrapePool         string            `json:"scrapePool"`
	ScrapeURL          string            `json:"scrapeUrl"`
	GlobalURL          string            `json:"globalUrl"`
	LastError          string            `json:"lastError"`
	LastScrape         time.Time         `json:"lastScrape"`
	LastScrapeDuration float64           `json:"lastScrapeDuration"`
	Health             HealthStatus      `json:"health"`
}

// DroppedTarget models a dropped Prometheus scrape target.
type DroppedTarget struct {
	DiscoveredLabels map[string]string `json:"discoveredLabels"`
}

// MetricMetadata models the metadata of a metric with its scrape target and name.
type MetricMetadata struct {
	Target map[string]string `json:"target"`
	Metric string            `json:"metric,omitempty"`
	Type   MetricType        `json:"type"`
	Help   string            `json:"help"`
	Unit   string            `json:"unit"`
}

// Metadata models the metadata of a metric.
type Metadata struct {
	Type MetricType `json:"type"`
	Help string     `json:"help"`
	Unit string     `json:"unit"`
}

// queryResult contains result data for a query.
type queryResult struct {
	Type   model.ValueType `json:"resultType"`
	Result any             `json:"result"`

	// The decoded value.
	v model.Value
}

// TSDBResult contains the result from querying the tsdb endpoint.
type TSDBResult struct {
	HeadStats                   TSDBHeadStats `json:"headStats"`
	SeriesCountByMetricName     []Stat        `json:"seriesCountByMetricName"`
	LabelValueCountByLabelName  []Stat        `json:"labelValueCountByLabelName"`
	MemoryInBytesByLabelName    []Stat        `json:"memoryInBytesByLabelName"`
	SeriesCountByLabelValuePair []Stat        `json:"seriesCountByLabelValuePair"`
}

// TSDBHeadStats contains TSDB stats
type TSDBHeadStats struct {
	NumSeries     int `json:"numSeries"`
	NumLabelPairs int `json:"numLabelPairs"`
	ChunkCount    int `json:"chunkCount"`
	MinTime       int `json:"minTime"`
	MaxTime       int `json:"maxTime"`
}

// TSDBBlocksResult contains the result from querying the tsdb blocks endpoint.
//
// Note: TSDB block types are defined locally rather than importing from
// prometheus/prometheus to avoid circular dependencies and minimize the
// dependency footprint. These types match the HTTP API wire format and use
// string for ULID fields (since JSON serializes ULIDs as strings). See the
// individual type comments for links to upstream Prometheus definitions.
type TSDBBlocksResult struct {
	Blocks []TSDBBlockMeta `json:"blocks"`
}

// TSDBBlockMeta contains the metadata for a single TSDB block.
// Counterpart to prometheus/prometheus tsdb.BlockMeta:
// https://github.com/prometheus/prometheus/blob/v0.313.2/tsdb/block.go#L164
type TSDBBlockMeta struct {
	ULID       string                  `json:"ulid"`
	MinTime    int64                   `json:"minTime"`
	MaxTime    int64                   `json:"maxTime"`
	Stats      TSDBBlockStats          `json:"stats,omitempty"` //nolint:modernize
	Compaction TSDBBlockMetaCompaction `json:"compaction"`
	Version    int                     `json:"version"`
}

// TSDBBlockStats contains block stats for a single TSDB block.
// Counterpart to prometheus/prometheus tsdb.BlockStats:
// https://github.com/prometheus/prometheus/blob/v0.313.2/tsdb/block.go#L184
type TSDBBlockStats struct {
	NumSamples          uint64 `json:"numSamples,omitempty"`
	NumFloatSamples     uint64 `json:"numFloatSamples,omitempty"`
	NumHistogramSamples uint64 `json:"numHistogramSamples,omitempty"`
	NumSeries           uint64 `json:"numSeries,omitempty"`
	NumChunks           uint64 `json:"numChunks,omitempty"`
	NumTombstones       uint64 `json:"numTombstones,omitempty"`
}

// TSDBBlockDesc describes a TSDB block reference, used in compaction parent tracking.
// Counterpart to prometheus/prometheus tsdb.BlockDesc:
// https://github.com/prometheus/prometheus/blob/v0.313.2/tsdb/block.go#L194
type TSDBBlockDesc struct {
	ULID    string `json:"ulid"`
	MinTime int64  `json:"minTime"`
	MaxTime int64  `json:"maxTime"`
}

// TSDBBlockMetaCompaction contains block compaction details for a single TSDB block.
// Counterpart to prometheus/prometheus tsdb.BlockMetaCompaction:
// https://github.com/prometheus/prometheus/blob/v0.313.2/tsdb/block.go#L201
type TSDBBlockMetaCompaction struct {
	Level     int             `json:"level"`
	Sources   []string        `json:"sources,omitempty"`
	Deletable bool            `json:"deletable,omitempty"`
	Parents   []TSDBBlockDesc `json:"parents,omitempty"`
	Failed    bool            `json:"failed,omitempty"`
	Hints     []string        `json:"hints,omitempty"`
}

// WalReplayStatus represents the wal replay status.
type WalReplayStatus struct {
	Min     int `json:"min"`
	Max     int `json:"max"`
	Current int `json:"current"`
}

// Stat models information about statistic value.
type Stat struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
}

func (rg *RuleGroup) UnmarshalJSON(b []byte) error {
	v := struct {
		Name     string              `json:"name"`
		File     string              `json:"file"`
		Interval float64             `json:"interval"`
		Rules    []gojson.RawMessage `json:"rules"`
	}{}

	if err := gojson.Unmarshal(b, &v); err != nil {
		return err
	}

	rg.Name = v.Name
	rg.File = v.File
	rg.Interval = v.Interval

	for _, rule := range v.Rules {
		ruleType, err := unmarshalRuleType(rule)
		if err != nil {
			return err
		}
		switch ruleType {
		case RuleTypeAlerting:
			alertingRule := AlertingRule{}
			if err := alertingRule.unmarshalTypeCheckedJSON(rule); err != nil {
				return err
			}
			rg.Rules = append(rg.Rules, alertingRule)
		case RuleTypeRecording:
			recordingRule := RecordingRule{}
			if err := recordingRule.unmarshalTypeCheckedJSON(rule); err != nil {
				return err
			}
			rg.Rules = append(rg.Rules, recordingRule)
		default:
			return errors.New("failed to decode JSON into an alerting or recording rule")
		}
	}

	return nil
}

func unmarshalRuleType(b []byte) (RuleType, error) {
	v := struct {
		Type string `json:"type"`
	}{}
	if err := gojson.Unmarshal(b, &v); err != nil {
		return RuleType(""), err
	}
	if v.Type == "" {
		return RuleType(""), errors.New("type field not present in rule")
	}
	return RuleType(v.Type), nil
}

func (r *AlertingRule) UnmarshalJSON(b []byte) error {
	ruleType, err := unmarshalRuleType(b)
	if err != nil {
		return err
	}
	if ruleType != RuleTypeAlerting {
		return fmt.Errorf("expected rule of type %s but got %s", string(RuleTypeAlerting), ruleType)
	}
	return r.unmarshalTypeCheckedJSON(b)
}

type alertingRuleInternal AlertingRule

// unmarshalTypeCheckedJSON unmarshals json with the type field already verified to be RuleTypeAlerting
func (r *AlertingRule) unmarshalTypeCheckedJSON(b []byte) error {
	return gojson.Unmarshal(b, (*alertingRuleInternal)(r))
}

func (r *RecordingRule) UnmarshalJSON(b []byte) error {
	ruleType, err := unmarshalRuleType(b)
	if err != nil {
		return err
	}
	if ruleType != RuleTypeRecording {
		return fmt.Errorf("expected rule of type %s but got %s", string(RuleTypeRecording), ruleType)
	}
	return r.unmarshalTypeCheckedJSON(b)
}

type recordingRuleInternal RecordingRule

// unmarshalTypeCheckedJSON unmarshals json with the type field already verified to be RuleTypeRecording
func (r *RecordingRule) unmarshalTypeCheckedJSON(b []byte) error {
	return gojson.Unmarshal(b, (*recordingRuleInternal)(r))
}

// Exemplar is additional information associated with a time series.
type Exemplar struct {
	Labels    model.LabelSet    `json:"labels"`
	Value     model.SampleValue `json:"value"`
	Timestamp model.Time        `json:"timestamp"`
}

type ExemplarQueryResult struct {
	SeriesLabels model.LabelSet `json:"seriesLabels"`
	Exemplars    []Exemplar     `json:"exemplars"`
}

// NewAPI returns a new API for the client.
//
// It is safe to use the returned API from multiple goroutines.
func NewAPI(c api.Client) API {
	return &httpAPI{
		client: &apiClientImpl{
			client: c,
		},
	}
}

type httpAPI struct {
	client apiClient
}

func (h *httpAPI) Alerts(ctx context.Context) (AlertsResult, error) {
	u := h.client.URL(epAlerts, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return AlertsResult{}, err
	}

	var res AlertsResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) AlertManagers(ctx context.Context) (AlertManagersResult, error) {
	u := h.client.URL(epAlertManagers, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return AlertManagersResult{}, err
	}

	var res AlertManagersResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) CleanTombstones(ctx context.Context) error {
	u := h.client.URL(epCleanTombstones, nil)

	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	_, _, _, err = h.client.Do(ctx, req, nil)
	return err
}

func (h *httpAPI) Config(ctx context.Context) (ConfigResult, error) {
	u := h.client.URL(epConfig, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return ConfigResult{}, err
	}

	var res ConfigResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) DeleteSeries(ctx context.Context, matches []string, startTime, endTime time.Time) error {
	u := h.client.URL(epDeleteSeries, nil)
	q := u.Query()

	for _, m := range matches {
		q.Add("match[]", m)
	}

	if !startTime.IsZero() {
		q.Set("start", formatTime(startTime))
	}
	if !endTime.IsZero() {
		q.Set("end", formatTime(endTime))
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}

	_, _, _, err = h.client.Do(ctx, req, nil)
	return err
}

func (h *httpAPI) Flags(ctx context.Context) (FlagsResult, error) {
	u := h.client.URL(epFlags, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return FlagsResult{}, err
	}

	var res FlagsResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) Buildinfo(ctx context.Context) (BuildinfoResult, error) {
	u := h.client.URL(epBuildinfo, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return BuildinfoResult{}, err
	}

	var res BuildinfoResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) Runtimeinfo(ctx context.Context) (RuntimeinfoResult, error) {
	u := h.client.URL(epRuntimeinfo, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return RuntimeinfoResult{}, err
	}

	var res RuntimeinfoResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) LabelNames(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...Option) (model.LabelNames, Warnings, Infos, error) {
	u := h.client.URL(epLabels, nil)
	q := addOptionalURLParams(u.Query(), opts)

	if !startTime.IsZero() {
		q.Set("start", formatTime(startTime))
	}
	if !endTime.IsZero() {
		q.Set("end", formatTime(endTime))
	}
	for _, m := range matches {
		q.Add("match[]", m)
	}

	var labelNames model.LabelNames
	_, w, i, err := h.client.DoGetFallback(ctx, u, q, &labelNames)
	return labelNames, w, i, err
}

func (h *httpAPI) LabelValues(ctx context.Context, label string, matches []string, startTime, endTime time.Time, opts ...Option) (model.LabelValues, Warnings, Infos, error) {
	u := h.client.URL(epLabelValues, map[string]string{"name": label})
	q := addOptionalURLParams(u.Query(), opts)

	if !startTime.IsZero() {
		q.Set("start", formatTime(startTime))
	}
	if !endTime.IsZero() {
		q.Set("end", formatTime(endTime))
	}
	for _, m := range matches {
		q.Add("match[]", m)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	var labelValues model.LabelValues
	_, w, i, err := h.client.Do(ctx, req, &labelValues)
	return labelValues, w, i, err
}

// StatsValue is a type for `stats` query parameter.
type StatsValue string

// AllStatsValue is the query parameter value to return all the query statistics.
const (
	AllStatsValue StatsValue = "all"
)

type apiOptions struct {
	timeout       time.Duration
	lookbackDelta time.Duration
	stats         StatsValue
	limit         uint64
}

type Option func(c *apiOptions)

// WithTimeout can be used to provide an optional query evaluation timeout for Query and QueryRange.
// https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries
func WithTimeout(timeout time.Duration) Option {
	return func(o *apiOptions) {
		o.timeout = timeout
	}
}

// WithLookbackDelta can be used to provide an optional query lookback delta for Query and QueryRange.
// This URL variable is not documented on Prometheus HTTP API.
// https://github.com/prometheus/prometheus/blob/e04913aea2792a5c8bc7b3130c389ca1b027dd9b/promql/engine.go#L162-L167
func WithLookbackDelta(lookbackDelta time.Duration) Option {
	return func(o *apiOptions) {
		o.lookbackDelta = lookbackDelta
	}
}

// WithStats can be used to provide an optional per step stats for Query and QueryRange.
// This URL variable is not documented on Prometheus HTTP API.
// https://github.com/prometheus/prometheus/blob/e04913aea2792a5c8bc7b3130c389ca1b027dd9b/promql/engine.go#L162-L167
func WithStats(stats StatsValue) Option {
	return func(o *apiOptions) {
		o.stats = stats
	}
}

// WithLimit provides an optional maximum number of returned entries for APIs that support limit parameter
// e.g. https://prometheus.io/docs/prometheus/latest/querying/api/#instant-querie:~:text=%3A%20End%20timestamp.-,limit%3D%3Cnumber%3E,-%3A%20Maximum%20number%20of
func WithLimit(limit uint64) Option {
	return func(o *apiOptions) {
		o.limit = limit
	}
}

func addOptionalURLParams(q url.Values, opts []Option) url.Values {
	opt := &apiOptions{}
	for _, o := range opts {
		o(opt)
	}

	if opt.timeout > 0 {
		q.Set("timeout", opt.timeout.String())
	}

	if opt.lookbackDelta > 0 {
		q.Set("lookback_delta", opt.lookbackDelta.String())
	}

	if opt.stats != "" {
		q.Set("stats", string(opt.stats))
	}

	if opt.limit > 0 {
		q.Set("limit", strconv.FormatUint(opt.limit, 10))
	}

	return q
}

func (h *httpAPI) Query(ctx context.Context, query string, ts time.Time, opts ...Option) (model.Value, Warnings, Infos, error) {
	u := h.client.URL(epQuery, nil)
	q := addOptionalURLParams(u.Query(), opts)

	q.Set("query", query)
	if !ts.IsZero() {
		q.Set("time", formatTime(ts))
	}

	var qres queryResult
	_, warnings, infos, err := h.client.DoGetFallback(ctx, u, q, &qres)
	return qres.v, warnings, infos, err
}

func (h *httpAPI) QueryRange(ctx context.Context, query string, r Range, opts ...Option) (model.Value, Warnings, Infos, error) {
	u := h.client.URL(epQueryRange, nil)
	q := addOptionalURLParams(u.Query(), opts)

	q.Set("query", query)
	q.Set("start", formatTime(r.Start))
	q.Set("end", formatTime(r.End))
	q.Set("step", strconv.FormatFloat(r.Step.Seconds(), 'f', -1, 64))

	var qres queryResult
	_, warnings, infos, err := h.client.DoGetFallback(ctx, u, q, &qres)
	return qres.v, warnings, infos, err
}

func (h *httpAPI) Series(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...Option) ([]model.LabelSet, Warnings, Infos, error) {
	u := h.client.URL(epSeries, nil)
	q := addOptionalURLParams(u.Query(), opts)

	for _, m := range matches {
		q.Add("match[]", m)
	}

	if !startTime.IsZero() {
		q.Set("start", formatTime(startTime))
	}
	if !endTime.IsZero() {
		q.Set("end", formatTime(endTime))
	}

	var mset []model.LabelSet
	_, warnings, infos, err := h.client.DoGetFallback(ctx, u, q, &mset)
	return mset, warnings, infos, err
}

func (h *httpAPI) Snapshot(ctx context.Context, skipHead bool) (SnapshotResult, error) {
	u := h.client.URL(epSnapshot, nil)
	q := u.Query()

	q.Set("skip_head", strconv.FormatBool(skipHead))

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		return SnapshotResult{}, err
	}

	var res SnapshotResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) Rules(ctx context.Context, matches []string) (RulesResult, error) {
	u := h.client.URL(epRules, nil)
	q := u.Query()

	for _, m := range matches {
		q.Add("match[]", m)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return RulesResult{}, err
	}

	var res RulesResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) Targets(ctx context.Context) (TargetsResult, error) {
	u := h.client.URL(epTargets, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return TargetsResult{}, err
	}

	var res TargetsResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) TargetsMetadata(ctx context.Context, matchTarget, metric, limit string) ([]MetricMetadata, error) {
	u := h.client.URL(epTargetsMetadata, nil)
	q := u.Query()

	q.Set("match_target", matchTarget)
	q.Set("metric", metric)
	q.Set("limit", limit)

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	var res []MetricMetadata
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]Metadata, error) {
	u := h.client.URL(epMetadata, nil)
	q := u.Query()

	q.Set("metric", metric)
	q.Set("limit", limit)

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	var res map[string][]Metadata
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) TSDB(ctx context.Context, opts ...Option) (TSDBResult, error) {
	u := h.client.URL(epTSDB, nil)
	q := addOptionalURLParams(u.Query(), opts)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return TSDBResult{}, err
	}

	var res TSDBResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) TSDBBlocks(ctx context.Context) (TSDBBlocksResult, error) {
	u := h.client.URL(epTSDBBlocks, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return TSDBBlocksResult{}, err
	}

	var res TSDBBlocksResult
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) WalReplay(ctx context.Context) (WalReplayStatus, error) {
	u := h.client.URL(epWalReplay, nil)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return WalReplayStatus{}, err
	}

	var res WalReplayStatus
	_, _, _, err = h.client.Do(ctx, req, &res)
	return res, err
}

func (h *httpAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]ExemplarQueryResult, error) {
	u := h.client.URL(epQueryExemplars, nil)
	q := u.Query()

	q.Set("query", query)
	if !startTime.IsZero() {
		q.Set("start", formatTime(startTime))
	}
	if !endTime.IsZero() {
		q.Set("end", formatTime(endTime))
	}

	var res []ExemplarQueryResult
	_, _, _, err := h.client.DoGetFallback(ctx, u, q, &res)
	return res, err
}

func (h *httpAPI) FormatQuery(ctx context.Context, query string) (string, error) {
	u := h.client.URL(epFormatQuery, nil)
	q := u.Query()
	q.Set("query", query)

	into := gojson.RawMessage{}
	_, _, _, err := h.client.DoGetFallback(ctx, u, q, &into)
	return string(into), err
}

// Warnings is an array of non-critical errors.
type Warnings []string

// Infos is an array of informational messages.
type Infos []string

// apiClient wraps a regular client and processes successful API responses.
// Successful also includes responses that errored at the API level.
type apiClient interface {
	URL(ep string, args map[string]string) *url.URL
	Do(ctx context.Context, req *http.Request, into any) (*http.Response, Warnings, Infos, error)
	DoGetFallback(ctx context.Context, u *url.URL, args url.Values, into any) (*http.Response, Warnings, Infos, error)
}

type apiClientImpl struct {
	client api.Client
}

type apiResponse struct {
	Status    string    `json:"status"`
	Data      any       `json:"data"`
	ErrorType ErrorType `json:"errorType"`
	Error     string    `json:"error"`
	Warnings  []string  `json:"warnings,omitempty"`
	Infos     []string  `json:"infos,omitempty"`
}

func apiError(code int) bool {
	// These are the codes that Prometheus sends when it returns an error.
	return code == http.StatusUnprocessableEntity || code == http.StatusBadRequest
}

func errorTypeAndMsgFor(resp *http.Response) (ErrorType, string) {
	switch resp.StatusCode / 100 {
	case 4:
		return ErrClient, fmt.Sprintf("client error: %d", resp.StatusCode)
	case 5:
		return ErrServer, fmt.Sprintf("server error: %d", resp.StatusCode)
	}
	return ErrBadResponse, fmt.Sprintf("bad response code %d", resp.StatusCode)
}

func (h *apiClientImpl) URL(ep string, args map[string]string) *url.URL {
	return h.client.URL(ep, args)
}

func (h *apiClientImpl) Do(ctx context.Context, req *http.Request, into any) (*http.Response, Warnings, Infos, error) {
	resp, body, err := h.client.Do(ctx, req)
	if err != nil {
		return resp, nil, nil, err
	}

	code := resp.StatusCode

	// Error response code, always return an error.
	if code/100 != 2 {
		// Generic HTTP error.
		if !apiError(code) {
			errorType, errorMsg := errorTypeAndMsgFor(resp)
			return resp, nil, nil, &Error{
				Type:   errorType,
				Msg:    errorMsg,
				Detail: string(body),
			}
		}

		// API response error.
		// Decode to get warnings, info, errorType/error, ignoring `into` object.
		result := apiResponse{Data: &gojson.RawMessage{}}
		if jsonErr := gojson.Unmarshal(body, &result); jsonErr != nil {
			errorType, errorMsg := errorTypeAndMsgFor(resp)
			return resp, nil, nil, &Error{
				Type:   errorType,
				Msg:    errorMsg,
				Detail: string(body),
			}
		}
		if result.Status == "success" {
			return resp, result.Warnings, result.Infos, &Error{
				Type: ErrBadResponse,
				Msg:  "inconsistent body for response code",
			}
		}
		// Match errorTypeAndMsgFor() if errorType/error message is unset in response.
		if result.ErrorType == "" {
			result.ErrorType = ErrClient
		}
		if result.Error == "" {
			result.Error = fmt.Sprintf("client error: %d", code)
		}
		return resp, result.Warnings, result.Infos, &Error{
			Type: result.ErrorType,
			Msg:  result.Error,
		}
	}

	// If no into type was provided, tolerate arbitrary data.
	if into == nil {
		into = &gojson.RawMessage{}
	}
	_, untypedInto := into.(*gojson.RawMessage)

	// If the response indicated no content, and no specific typed data was expected, return early.
	if http.StatusNoContent == code && untypedInto {
		return resp, nil, nil, nil
	}

	// Decode into the envelope into the expected object.
	result := apiResponse{Data: into}
	if jsonErr := gojson.Unmarshal(body, &result); jsonErr != nil {
		// in case the decode error came from decoding into a typed `into` object,
		// re-attempt to capture any warnings and infos in the response
		if !untypedInto {
			result = apiResponse{Data: &gojson.RawMessage{}}
			_ = gojson.Unmarshal(body, &result)
		}
		return resp, result.Warnings, result.Infos, &Error{
			Type: ErrBadResponse,
			Msg:  jsonErr.Error(),
		}
	}

	if result.Status == "error" {
		// Unexpected with a successful status code, but if the response body indicates an error, return an error.
		err = &Error{
			Type: result.ErrorType,
			Msg:  result.Error,
		}
	}

	return resp, result.Warnings, result.Infos, err
}

// DoGetFallback will attempt to do the request as-is, and on a 403, 405, or
// 501 it will fallback to a GET request.
func (h *apiClientImpl) DoGetFallback(ctx context.Context, u *url.URL, args url.Values, into any) (*http.Response, Warnings, Infos, error) {
	encodedArgs := args.Encode()
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(encodedArgs))
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Following comment originates from https://pkg.go.dev/net/http#Transport
	// Transport only retries a request upon encountering a network error if the request is
	// idempotent and either has no body or has its Request.GetBody defined. HTTP requests
	// are considered idempotent if they have HTTP methods GET, HEAD, OPTIONS, or TRACE; or
	// if their Header map contains an "Idempotency-Key" or "X-Idempotency-Key" entry. If the
	// idempotency key value is a zero-length slice, the request is treated as idempotent but
	// the header is not sent on the wire.
	req.Header["Idempotency-Key"] = nil

	resp, warnings, infos, err := h.Do(ctx, req, into)
	if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented) {
		u.RawQuery = encodedArgs
		req, err = http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, warnings, infos, err
		}
		return h.Do(ctx, req, into)
	}
	return resp, warnings, infos, err
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}
