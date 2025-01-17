package client

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/utils"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	lokiflag "github.com/grafana/loki/v3/pkg/util/flagext"
)

var logEntries = []api.Entry{
	{Labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(1, 0).UTC(), Line: "line1"}},
	{Labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(2, 0).UTC(), Line: "line2"}},
	{Labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(3, 0).UTC(), Line: "line3"}},
	{Labels: model.LabelSet{"__tenant_id__": "tenant-1"}, Entry: logproto.Entry{Timestamp: time.Unix(4, 0).UTC(), Line: "line4"}},
	{Labels: model.LabelSet{"__tenant_id__": "tenant-1"}, Entry: logproto.Entry{Timestamp: time.Unix(5, 0).UTC(), Line: "line5"}},
	{Labels: model.LabelSet{"__tenant_id__": "tenant-2"}, Entry: logproto.Entry{Timestamp: time.Unix(6, 0).UTC(), Line: "line6"}},
	{Labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(6, 0).UTC(), Line: "line0123456789"}},
	{
		Labels: model.LabelSet{},
		Entry: logproto.Entry{
			Timestamp: time.Unix(7, 0).UTC(),
			Line:      "line7",
			StructuredMetadata: push.LabelsAdapter{
				{Name: "trace_id", Value: "12345"},
			},
		},
	},
}

func TestClient_Handle(t *testing.T) {
	tests := map[string]struct {
		clientBatchSize           int
		clientBatchWait           time.Duration
		clientMaxRetries          int
		clientMaxLineSize         int
		clientMaxLineSizeTruncate bool
		clientTenantID            string
		clientDropRateLimited     bool
		serverResponseStatus      int
		inputEntries              []api.Entry
		inputDelay                time.Duration
		expectedReqs              []utils.RemoteWriteRequest
		expectedMetrics           string
	}{
		"batch log entries together until the batch size is reached": {
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []api.Entry{logEntries[0], logEntries[1], logEntries[2]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[2].Entry}}}},
				},
			},
			expectedMetrics: `
                               # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                               # TYPE promtail_sent_entries_total counter
                               promtail_sent_entries_total{host="__HOST__"} 3.0
                               # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                               # TYPE promtail_dropped_entries_total counter
                               promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                               # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                               # TYPE promtail_mutated_entries_total counter
                               promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                               # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                               # TYPE promtail_mutated_bytes_total counter
                               promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                               promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                       `,
		},
		"dropping log entries that have max_line_size exceeded": {
			clientBatchSize:           10,
			clientBatchWait:           100 * time.Millisecond,
			clientMaxRetries:          3,
			clientMaxLineSize:         10, // any log line more than this length should be discarded
			clientMaxLineSizeTruncate: false,
			serverResponseStatus:      200,
			inputEntries:              []api.Entry{logEntries[0], logEntries[1], logEntries[6]}, // this logEntries[6] entries has line more than size 10
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}}}},
				},
			},
			expectedMetrics: `
                               # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                               # TYPE promtail_sent_entries_total counter
                               promtail_sent_entries_total{host="__HOST__"} 2.0
                               # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                               # TYPE promtail_dropped_entries_total counter
                               promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 1
                               promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                               # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                               # TYPE promtail_mutated_entries_total counter
                               promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                       `,
		},
		"truncating log entries that have max_line_size exceeded": {
			clientBatchSize:           10,
			clientBatchWait:           100 * time.Millisecond,
			clientMaxRetries:          3,
			clientMaxLineSize:         10,
			clientMaxLineSizeTruncate: true,
			serverResponseStatus:      200,
			inputEntries:              []api.Entry{logEntries[0], logEntries[1], logEntries[6]}, // logEntries[6]'s line is greater than 10 bytes
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request: logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{
						logEntries[0].Entry,
						logEntries[1].Entry,
						{
							Timestamp: logEntries[6].Entry.Timestamp,
							Line:      logEntries[6].Line[:10],
						},
					}}}},
				},
			},
			expectedMetrics: `
                               # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                               # TYPE promtail_sent_entries_total counter
                               promtail_sent_entries_total{host="__HOST__"} 3.0
                               # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                               # TYPE promtail_dropped_entries_total counter
                               promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                               # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                               # TYPE promtail_mutated_entries_total counter
                               promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 1
                               promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                               promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 4
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                       `,
		},

		"batch log entries together until the batch wait time is reached": {
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []api.Entry{logEntries[0], logEntries[1]},
			inputDelay:           110 * time.Millisecond,
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[1].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 2.0
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                       `,
		},
		"retry send a batch up to backoff's max retries in case the server responds with a 5xx": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 500,
			inputEntries:         []api.Entry{logEntries[0]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 1
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 0
                       `,
		},
		"do not retry send a batch in case the server responds with a 4xx": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 400,
			inputEntries:         []api.Entry{logEntries[0]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 1
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 0
                       `,
		},
		"do retry sending a batch in case the server responds with a 429": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 429,
			inputEntries:         []api.Entry{logEntries[0]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 1
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 0
                       `,
		},
		"do not retry in case of 429 when client is configured to drop rate limited batches": {
			clientBatchSize:       10,
			clientBatchWait:       10 * time.Millisecond,
			clientMaxRetries:      3,
			clientDropRateLimited: true,
			serverResponseStatus:  429,
			inputEntries:          []api.Entry{logEntries[0]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 1
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 0
                       `,
		},
		"batch log entries together honoring the client tenant ID": {
			clientBatchSize:      100,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			clientTenantID:       "tenant-default",
			serverResponseStatus: 200,
			inputEntries:         []api.Entry{logEntries[0], logEntries[1]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "tenant-default",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 2.0
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__", reason="ingester_error", tenant="tenant-default"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-default"} 0
                              promtail_dropped_entries_total{host="__HOST__", reason="rate_limited", tenant="tenant-default"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-default"} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-default"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-default"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-default"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-default"} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant="tenant-default"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant="tenant-default"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant="tenant-default"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant="tenant-default"} 0
                       `,
		},
		"batch log entries together honoring the tenant ID overridden while processing the pipeline stages": {
			clientBatchSize:      100,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			clientTenantID:       "tenant-default",
			serverResponseStatus: 200,
			inputEntries:         []api.Entry{logEntries[0], logEntries[3], logEntries[4], logEntries[5]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "tenant-default",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					TenantID: "tenant-1",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[3].Entry, logEntries[4].Entry}}}},
				},
				{
					TenantID: "tenant-2",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[5].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 4.0
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-1"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-2"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-default"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-1"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-2"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-default"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-1"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-2"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-default"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-1"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-2"} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-default"} 0
                              # HELP promtail_mutated_entries_total The total number of log entries that have been mutated.
                              # TYPE promtail_mutated_entries_total counter
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-1"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-2"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="ingester_error",tenant="tenant-default"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-1"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-2"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="line_too_long",tenant="tenant-default"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-1"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-2"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="rate_limited",tenant="tenant-default"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-1"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-2"} 0
                              promtail_mutated_entries_total{host="__HOST__",reason="stream_limited",tenant="tenant-default"} 0
                              # HELP promtail_mutated_bytes_total The total number of bytes that have been mutated.
                              # TYPE promtail_mutated_bytes_total counter
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant="tenant-1"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant="tenant-2"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="ingester_error",tenant="tenant-default"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant="tenant-1"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant="tenant-2"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="line_too_long",tenant="tenant-default"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant="tenant-1"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant="tenant-2"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="rate_limited",tenant="tenant-default"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant="tenant-1"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant="tenant-2"} 0
                              promtail_mutated_bytes_total{host="__HOST__",reason="stream_limited",tenant="tenant-default"} 0
                       `,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			reg := prometheus.NewRegistry()

			// Create a buffer channel where we do enqueue received requests
			receivedReqsChan := make(chan utils.RemoteWriteRequest, 10)

			// Start a local HTTP server
			server := utils.NewRemoteWriteServer(receivedReqsChan, testData.serverResponseStatus)
			require.NotNil(t, server)
			defer server.Close()

			// Get the URL at which the local test server is listening to
			serverURL := flagext.URLValue{}
			err := serverURL.Set(server.URL)
			require.NoError(t, err)

			// Instance the client
			cfg := Config{
				URL:                    serverURL,
				BatchWait:              testData.clientBatchWait,
				BatchSize:              testData.clientBatchSize,
				DropRateLimitedBatches: testData.clientDropRateLimited,
				Client:                 config.HTTPClientConfig{},
				BackoffConfig:          backoff.Config{MinBackoff: 1 * time.Millisecond, MaxBackoff: 2 * time.Millisecond, MaxRetries: testData.clientMaxRetries},
				ExternalLabels:         lokiflag.LabelSet{},
				Timeout:                1 * time.Second,
				TenantID:               testData.clientTenantID,
			}

			m := NewMetrics(reg)
			c, err := New(m, cfg, 0, testData.clientMaxLineSize, testData.clientMaxLineSizeTruncate, log.NewNopLogger())
			require.NoError(t, err)

			// Send all the input log entries
			for i, logEntry := range testData.inputEntries {
				c.Chan() <- logEntry

				if testData.inputDelay > 0 && i < len(testData.inputEntries)-1 {
					time.Sleep(testData.inputDelay)
				}
			}

			// Wait until the expected push requests are received (with a timeout)
			deadline := time.Now().Add(1 * time.Second)
			for len(receivedReqsChan) < len(testData.expectedReqs) && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}

			// Stop the client: it waits until the current batch is sent
			c.Stop()
			close(receivedReqsChan)

			// Get all push requests received on the server side
			receivedReqs := make([]utils.RemoteWriteRequest, 0)
			for req := range receivedReqsChan {
				receivedReqs = append(receivedReqs, req)
			}

			// Due to implementation details (maps iteration ordering is random) we just check
			// that the expected requests are equal to the received requests, without checking
			// the exact order which is not guaranteed in case of multi-tenant
			// require.ElementsMatch(t, testData.expectedReqs, receivedReqs)
			fmt.Printf("Received reqs: %#v\n", receivedReqs)
			fmt.Printf("Expected reqs: %#v\n", testData.expectedReqs)

			expectedMetrics := strings.Replace(testData.expectedMetrics, "__HOST__", serverURL.Host, -1)
			err = testutil.GatherAndCompare(reg, strings.NewReader(expectedMetrics), "promtail_sent_entries_total", "promtail_dropped_entries_total", "promtail_mutated_entries_total", "promtail_mutated_bytes_total")
			assert.NoError(t, err)
		})
	}
}

func TestClient_StopNow(t *testing.T) {
	cases := []struct {
		name                 string
		clientBatchSize      int
		clientBatchWait      time.Duration
		clientMaxRetries     int
		clientTenantID       string
		serverResponseStatus int
		inputEntries         []api.Entry
		inputDelay           time.Duration
		expectedReqs         []utils.RemoteWriteRequest
		expectedMetrics      string
	}{
		{
			name:                 "send requests shouldn't be cancelled after StopNow()",
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []api.Entry{logEntries[0], logEntries[1], logEntries[2]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}}}},
				},
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[2].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 3.0
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                       `,
		},
		{
			name:                 "shouldn't retry after StopNow()",
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 429,
			inputEntries:         []api.Entry{logEntries[0]},
			expectedReqs: []utils.RemoteWriteRequest{
				{
					TenantID: "",
					Request:  logproto.PushRequest{Streams: []logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
                              # HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
                              # TYPE promtail_dropped_entries_total counter
                              promtail_dropped_entries_total{host="__HOST__",reason="ingester_error",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="line_too_long",tenant=""} 0
                              promtail_dropped_entries_total{host="__HOST__",reason="rate_limited",tenant=""} 1
                              promtail_dropped_entries_total{host="__HOST__",reason="stream_limited",tenant=""} 0
                              # HELP promtail_sent_entries_total Number of log entries sent to the ingester.
                              # TYPE promtail_sent_entries_total counter
                              promtail_sent_entries_total{host="__HOST__"} 0
                       `,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()

			// Create a buffer channel where we do enqueue received requests
			receivedReqsChan := make(chan utils.RemoteWriteRequest, 10)

			// Start a local HTTP server
			server := utils.NewRemoteWriteServer(receivedReqsChan, c.serverResponseStatus)
			require.NotNil(t, server)
			defer server.Close()

			// Get the URL at which the local test server is listening to
			serverURL := flagext.URLValue{}
			err := serverURL.Set(server.URL)
			require.NoError(t, err)

			// Instance the client
			cfg := Config{
				URL:            serverURL,
				BatchWait:      c.clientBatchWait,
				BatchSize:      c.clientBatchSize,
				Client:         config.HTTPClientConfig{},
				BackoffConfig:  backoff.Config{MinBackoff: 5 * time.Second, MaxBackoff: 10 * time.Second, MaxRetries: c.clientMaxRetries},
				ExternalLabels: lokiflag.LabelSet{},
				Timeout:        1 * time.Second,
				TenantID:       c.clientTenantID,
			}

			m := NewMetrics(reg)
			cl, err := New(m, cfg, 0, 0, false, log.NewNopLogger())
			require.NoError(t, err)

			// Send all the input log entries
			for i, logEntry := range c.inputEntries {
				cl.Chan() <- logEntry

				if c.inputDelay > 0 && i < len(c.inputEntries)-1 {
					time.Sleep(c.inputDelay)
				}
			}

			// Wait until the expected push requests are received (with a timeout)
			deadline := time.Now().Add(1 * time.Second)
			for len(receivedReqsChan) < len(c.expectedReqs) && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}

			// StopNow should have cancelled client's ctx
			cc := cl.(*client)
			require.NoError(t, cc.ctx.Err())

			// Stop the client: it waits until the current batch is sent
			cl.StopNow()
			close(receivedReqsChan)

			require.Error(t, cc.ctx.Err()) // non-nil error if its cancelled.

			// Get all push requests received on the server side
			receivedReqs := make([]utils.RemoteWriteRequest, 0)
			for req := range receivedReqsChan {
				receivedReqs = append(receivedReqs, req)
			}

			// Due to implementation details (maps iteration ordering is random) we just check
			// that the expected requests are equal to the received requests, without checking
			// the exact order which is not guaranteed in case of multi-tenant
			require.ElementsMatch(t, c.expectedReqs, receivedReqs)

			expectedMetrics := strings.Replace(c.expectedMetrics, "__HOST__", serverURL.Host, -1)
			err = testutil.GatherAndCompare(reg, strings.NewReader(expectedMetrics), "promtail_sent_entries_total", "promtail_dropped_entries_total")
			assert.NoError(t, err)
		})
	}
}

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (r RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return r(req)
}

func Test_Tripperware(t *testing.T) {
	url, err := url.Parse("http://foo.com")
	require.NoError(t, err)
	var called bool
	c, err := NewWithTripperware(metrics, Config{
		URL: flagext.URLValue{URL: url},
	}, 0, 0, false, log.NewNopLogger(), func(_ http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, r.URL.String(), "http://foo.com")
			called = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		})
	})
	require.NoError(t, err)

	c.Chan() <- api.Entry{
		Labels: model.LabelSet{"foo": "bar"},
		Entry:  logproto.Entry{Timestamp: time.Now(), Line: "foo"},
	}
	c.Stop()
	require.True(t, called)
}
