package ruler

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/prometheus/config"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/validation"
)

func Test_Load(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		data  string
		match string
	}{
		{
			desc: "load correctly",
			data: `
groups:
  - name: testgrp2
    interval: 0s
    rules:
      - alert: HTTPCredentialsLeaked
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail empty groupname",
			match: "Groupname must not be empty",
			data: `
groups:
  - name:
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail duplicate grps",
			match: "repeated in the same file",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail record & alert",
			match: "only one of 'record' and 'alert' must be set",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        record: doublevision
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail neither record nor alert",
			match: "one of 'record' or 'alert' must be set",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail empty expr",
			match: "field 'expr' must be set in rule",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail bad expr",
			match: "could not parse expression",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: garbage
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail annotations in recording rule",
			match: "invalid field 'annotations' in recording rule",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - record: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail for in recording rule",
			match: "invalid field 'for' in recording rule",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - record: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
`,
		},
		{
			desc:  "fail recording rule name",
			match: "invalid recording rule name:",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - record: 'Hi.ghThroughputLogStreams'
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
`,
		},
		{
			desc:  "fail invalid label name",
			match: "invalid label name:",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            'se.verity': page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail invalid annotation",
			match: "invalid annotation name:",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            's.ummary': High request latency
`,
		},
		{
			desc:  "unknown fields",
			match: "field unknown not found",
			data: `
unknown: true
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            's.ummary': High request latency
`,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var loader GroupLoader
			f, err := ioutil.TempFile(os.TempDir(), "rules")
			require.Nil(t, err)
			defer os.Remove(f.Name())
			err = ioutil.WriteFile(f.Name(), []byte(tc.data), 0777)
			require.Nil(t, err)

			_, errs := loader.Load(f.Name())
			if tc.match != "" {
				require.NotNil(t, errs)
				var found bool
				for _, err := range errs {
					found = found || strings.Contains(err.Error(), tc.match)
				}
				if !found {
					fmt.Printf("\nerrors did not contain desired (%s): %v", tc.match, errs)
				}
				require.Equal(t, true, found)
			} else {
				require.Nil(t, errs)
			}
		})

	}
}

// TestInvalidRemoteWriteConfig tests that a validation error is raised when config is invalid
func TestInvalidRemoteWriteConfig(t *testing.T) {
	// if remote-write is not enabled, validation fails
	cfg := Config{
		Config: ruler.Config{},
		RemoteWrite: RemoteWriteConfig{
			Enabled: false,
		},
	}
	require.Nil(t, cfg.RemoteWrite.Validate())

	// if no remote-write URL is configured, validation fails
	cfg = Config{
		Config: ruler.Config{},
		RemoteWrite: RemoteWriteConfig{
			Enabled: true,
			Client: config.RemoteWriteConfig{
				URL: nil,
			},
		},
	}
	require.Error(t, cfg.RemoteWrite.Validate())
}

// TestDiscardingAppender tests that a DiscardingAppender is created when remote-write is disabled
func TestDiscardingAppender(t *testing.T) {
	cfg := Config{
		Config: ruler.Config{},
		RemoteWrite: RemoteWriteConfig{
			Enabled: false,
		},
	}
	require.False(t, cfg.RemoteWrite.Enabled)

	appendable := newAppendable(cfg, &validation.Overrides{}, log.NewNopLogger(), "fake", metrics)
	appender := appendable.Appender(context.TODO())
	require.Equal(t, DiscardingAppender{ErrRemoteWriteDisabled}, appender)
}

// TestNonMetricQuery tests that only metric queries can be executed in the query function,
// as both alert and recording rules rely on metric queries being run
func TestNonMetricQuery(t *testing.T) {
	overrides, err := validation.NewOverrides(validation.Limits{}, nil)
	require.Nil(t, err)

	engine := logql.NewEngine(logql.EngineOpts{}, &FakeQuerier{}, overrides)
	queryFunc := engineQueryFunc(engine, overrides, "fake")

	_, err = queryFunc(context.TODO(), `{job="nginx"}`, time.Now())
	require.Error(t, err, "rule result is not a vector or scalar")
}

type FakeQuerier struct{}

func (q *FakeQuerier) SelectLogs(context.Context, logql.SelectLogParams) (iter.EntryIterator, error) {
	return iter.NoopIterator, nil
}

func (q *FakeQuerier) SelectSamples(context.Context, logql.SelectSampleParams) (iter.SampleIterator, error) {
	return iter.NoopIterator, nil
}
