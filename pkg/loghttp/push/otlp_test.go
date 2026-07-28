package push

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"

	"bytes"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

var defaultGlobalOTLPConfig = GlobalOTLPConfig{}

func init() {
	flagext.DefaultValues(&defaultGlobalOTLPConfig)
}

func TestOTLPToLokiPushRequest(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())
	defaultServiceDetection := []string{
		"service",
		"app",
		"application",
		"name",
		"app_kubernetes_io_name",
		"container",
		"container_name",
		"k8s_container_name",
		"component",
		"workload",
		"job",
		"k8s_job_name",
	}

	for _, tc := range []struct {
		name                string
		generateLogs        func() plog.Logs
		expectedPushRequest logproto.PushRequest
		expectedStats       Stats
		otlpConfig          OTLPConfig
		discoverServiceName []string
	}{
		{
			name: "no logs",
			generateLogs: func() plog.Logs {
				return plog.NewLogs()
			},
			expectedPushRequest: logproto.PushRequest{},
			expectedStats:       *NewPushStats(),
			otlpConfig:          DefaultOTLPConfig(defaultGlobalOTLPConfig),
		},
		{
			name: "resource with no logs",
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr("service.name", "service-1")
				return ld
			},
			expectedPushRequest: logproto.PushRequest{},
			expectedStats:       *NewPushStats(),
			otlpConfig:          DefaultOTLPConfig(defaultGlobalOTLPConfig),
		},
		{
			name:       "resource with a log entry",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr("service.name", "service-1")
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test body")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{service_name="service-1"}`,
						Entries: []logproto.Entry{
							{
								Timestamp:          now,
								Line:               "test body",
								StructuredMetadata: push.LabelsAdapter{},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"service-1-policy": 1,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 9,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 0,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"service-1-policy": {
						time.Hour: nil,
					},
				},
				StreamLabelsSize:                  21,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
		{
			name:       "no resource attributes defined",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty()
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test body")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{service_name="unknown_service"}`,
						Entries: []logproto.Entry{
							{
								Timestamp:          now,
								Line:               "test body",
								StructuredMetadata: push.LabelsAdapter{},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"others": 1,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"others": {
						time.Hour: 9,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"others": {
						time.Hour: 0,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"others": {
						time.Hour: nil,
					},
				},
				StreamLabelsSize:                  27,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
		{
			name:       "service.name not defined in resource attributes",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr("service.namespace", "foo")
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test body")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{service_name="unknown_service", service_namespace="foo"}`,
						Entries: []logproto.Entry{
							{
								Timestamp:          now,
								Line:               "test body",
								StructuredMetadata: push.LabelsAdapter{},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"others": 1,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"others": {
						time.Hour: 9,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"others": {
						time.Hour: 0,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"others": {
						time.Hour: nil,
					},
				},
				StreamLabelsSize:                  47,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
		{
			name:       "service.name not defined and discovery candidate is empty",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			discoverServiceName: []string{
				"container_name",
			},
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr("container.name", "")
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test body")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{container_name="", service_name="unknown_service"}`,
						Entries: []logproto.Entry{
							{
								Timestamp:          now,
								Line:               "test body",
								StructuredMetadata: push.LabelsAdapter{},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"others": 1,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"others": {
						time.Hour: 9,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"others": {
						time.Hour: 0,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"others": {
						time.Hour: nil,
					},
				},
				StreamLabelsSize:                  41,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
		{
			name:       "resource attributes and scope attributes stored as structured metadata",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty()
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("service.name", "service-1")
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("service.image", "loki")
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty()
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().SetName("fizz")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().Attributes().PutStr("op", "buzz")
				for i := 0; i < 2; i++ {
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().AppendEmpty()
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Body().SetStr(fmt.Sprintf("test body - %d", i))
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				}
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{service_name="service-1"}`,
						Entries: []logproto.Entry{
							{
								Timestamp: now,
								Line:      "test body - 0",
								StructuredMetadata: push.LabelsAdapter{
									{
										Name:  "service_image",
										Value: "loki",
									},
									{
										Name:  "op",
										Value: "buzz",
									},
									{
										Name:  "scope_name",
										Value: "fizz",
									},
								},
							},
							{
								Timestamp: now,
								Line:      "test body - 1",
								StructuredMetadata: push.LabelsAdapter{
									{
										Name:  "service_image",
										Value: "loki",
									},
									{
										Name:  "op",
										Value: "buzz",
									},
									{
										Name:  "scope_name",
										Value: "fizz",
									},
								},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"service-1-policy": 2,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 26,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 37,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"service-1-policy": {
						time.Hour: []push.LabelAdapter{
							{Name: "service_image", Value: "loki"},
							{Name: "op", Value: "buzz"},
							{Name: "scope_name", Value: "fizz"},
						},
					},
				},
				StreamLabelsSize:                  21,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
		{
			name:       "attributes with nested data",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty()
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("service.name", "service-1")
				ld.ResourceLogs().At(0).Resource().Attributes().PutEmptyMap("resource.nested").PutStr("foo", "bar")
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty()
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().SetName("fizz")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().Attributes().PutEmptyMap("scope.nested").PutStr("foo", "bar")
				for i := 0; i < 2; i++ {
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().AppendEmpty()
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Body().SetStr(fmt.Sprintf("test body - %d", i))
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Attributes().PutEmptyMap("log.nested").PutStr("foo", fmt.Sprintf("bar - %d", i))
				}
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{service_name="service-1"}`,
						Entries: []logproto.Entry{
							{
								Timestamp: now,
								Line:      "test body - 0",
								StructuredMetadata: push.LabelsAdapter{
									{
										Name:  "log_nested_foo",
										Value: "bar - 0",
									},
									{
										Name:  "resource_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_name",
										Value: "fizz",
									},
								},
							},
							{
								Timestamp: now,
								Line:      "test body - 1",
								StructuredMetadata: push.LabelsAdapter{
									{
										Name:  "log_nested_foo",
										Value: "bar - 1",
									},
									{
										Name:  "resource_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_name",
										Value: "fizz",
									},
								},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"service-1-policy": 2,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 26,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 97,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"service-1-policy": {
						time.Hour: []push.LabelAdapter{
							{Name: "resource_nested_foo", Value: "bar"},
							{Name: "scope_nested_foo", Value: "bar"},
							{Name: "scope_name", Value: "fizz"},
						},
					},
				},
				StreamLabelsSize:                  21,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
		{
			name: "custom otlp config",
			otlpConfig: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					AttributesConfig: []AttributesConfig{
						{
							Action:     IndexLabel,
							Attributes: []string{"pod.name"},
						},
						{
							Action: IndexLabel,
							Regex:  relabel.MustNewRegexp("service.*"),
						},
						{
							Action: Drop,
							Regex:  relabel.MustNewRegexp("drop.*"),
						},
						{
							Action:     StructuredMetadata,
							Attributes: []string{"resource.nested"},
						},
					},
				},
				ScopeAttributes: []AttributesConfig{
					{
						Action:     Drop,
						Attributes: []string{"drop.function"},
					},
				},
				LogAttributes: []AttributesConfig{
					{
						Action: StructuredMetadata,
						Regex:  relabel.MustNewRegexp(".*_id"),
					},
					{
						Action: Drop,
						Regex:  relabel.MustNewRegexp(".*"),
					},
				},
			},
			generateLogs: func() plog.Logs {
				ld := plog.NewLogs()
				ld.ResourceLogs().AppendEmpty()
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("service.name", "service-1")
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("pod.name", "service-1-abc")
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("pod.ip", "10.200.200.200")
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("drop.service.addr", "192.168.0.1")
				ld.ResourceLogs().At(0).Resource().Attributes().PutStr("drop.service.version", "v1")
				ld.ResourceLogs().At(0).Resource().Attributes().PutEmptyMap("resource.nested").PutStr("foo", "bar")
				ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty()
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().SetName("fizz")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().Attributes().PutStr("drop.function", "login")
				ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().Attributes().PutEmptyMap("scope.nested").PutStr("foo", "bar")
				for i := 0; i < 2; i++ {
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().AppendEmpty()
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Body().SetStr(fmt.Sprintf("test body - %d", i))
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).SetTimestamp(pcommon.Timestamp(now.UnixNano()))
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Attributes().PutStr("user_id", "u1")
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Attributes().PutStr("order_id", "o1")
					ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(i).Attributes().PutEmptyMap("drop.log.nested").PutStr("foo", fmt.Sprintf("bar - %d", i))
				}
				return ld
			},
			expectedPushRequest: logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{pod_name="service-1-abc", service_name="service-1"}`,
						Entries: []logproto.Entry{
							{
								Timestamp: now,
								Line:      "test body - 0",
								StructuredMetadata: push.LabelsAdapter{
									{
										Name:  "user_id",
										Value: "u1",
									},
									{
										Name:  "order_id",
										Value: "o1",
									},
									{
										Name:  "pod_ip",
										Value: "10.200.200.200",
									},
									{
										Name:  "resource_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_name",
										Value: "fizz",
									},
								},
							},
							{
								Timestamp: now,
								Line:      "test body - 1",
								StructuredMetadata: push.LabelsAdapter{
									{
										Name:  "user_id",
										Value: "u1",
									},
									{
										Name:  "order_id",
										Value: "o1",
									},
									{
										Name:  "pod_ip",
										Value: "10.200.200.200",
									},
									{
										Name:  "resource_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_nested_foo",
										Value: "bar",
									},
									{
										Name:  "scope_name",
										Value: "fizz",
									},
								},
							},
						},
					},
				},
			},
			expectedStats: Stats{
				PolicyNumLines: map[string]int64{
					"service-1-policy": 2,
				},
				LogLinesBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 26,
					},
				},
				StructuredMetadataBytes: PolicyWithRetentionWithBytes{
					"service-1-policy": {
						time.Hour: 113,
					},
				},
				ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{
					"service-1-policy": {
						time.Hour: []push.LabelAdapter{
							{Name: "pod_ip", Value: "10.200.200.200"},
							{Name: "resource_nested_foo", Value: "bar"},
							{Name: "scope_nested_foo", Value: "bar"},
							{Name: "scope_name", Value: "fizz"},
						},
					},
				},
				StreamLabelsSize:                  42,
				MostRecentEntryTimestamp:          now,
				StreamSizeBytes:                   map[string]int64{},
				MostRecentEntryTimestampPerStream: map[string]time.Time{},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			discoverServiceName := defaultServiceDetection
			if tc.discoverServiceName != nil {
				discoverServiceName = tc.discoverServiceName
			}

			stats := NewPushStats()
			tracker := NewMockTracker()
			streamResolver := newMockStreamResolver("fake", &fakeLimits{})
			streamResolver.policyForOverride = func(_ context.Context, lbs labels.Labels) string {
				if lbs.Get("service_name") == "service-1" {
					return "service-1-policy"
				}
				return "others"
			}

			pushReq, err := otlpToLokiPushRequest(
				context.Background(),
				tc.generateLogs(),
				"foo",
				tc.otlpConfig,
				nil,
				discoverServiceName,
				false,
				tracker,
				stats,
				log.NewNopLogger(),
				streamResolver,
				constants.OTLP,
			)
			require.NoError(t, err)
			require.Equal(t, tc.expectedPushRequest, *pushReq)

			// TotalExpandedEntriesSize is the size of each entry after resource/scope attributes have been
			// merged into its structured metadata, which is exactly what expectedPushRequest's entries already
			// contain.
			expectedStats := tc.expectedStats
			for _, stream := range tc.expectedPushRequest.Streams {
				for i := range stream.Entries {
					expectedStats.TotalExpandedEntriesSize += int64(util.EntryTotalSize(&stream.Entries[i]))
				}
			}
			require.Equal(t, expectedStats, *stats)

			totalBytes := 0.0
			for _, policyMapping := range stats.LogLinesBytes {
				for _, b := range policyMapping {
					totalBytes += float64(b)
				}
			}
			for _, policyMapping := range stats.StructuredMetadataBytes {
				for _, b := range policyMapping {
					totalBytes += float64(b)
				}
			}
			require.Equal(t, totalBytes, tracker.Total(), "Total tracked bytes must equal total bytes of the stats.")
		})
	}
}

func TestOTLPLogToPushEntry(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	for _, tc := range []struct {
		name           string
		buildLogRecord func() plog.LogRecord
		expectedResp   push.Entry
	}{
		{
			name: "only body and timestamp set",
			buildLogRecord: func() plog.LogRecord {
				log := plog.NewLogRecord()
				log.Body().SetStr("log body")
				log.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				return log
			},
			expectedResp: push.Entry{
				Timestamp:          now,
				Line:               "log body",
				StructuredMetadata: push.LabelsAdapter{},
			},
		},
		{
			name: "all the values set",
			buildLogRecord: func() plog.LogRecord {
				log := plog.NewLogRecord()
				log.Body().SetStr("log body")
				log.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				log.SetObservedTimestamp(pcommon.Timestamp(now.UnixNano() + 1))
				log.SetSeverityNumber(plog.SeverityNumberDebug)
				log.SetSeverityText("debug")
				log.SetDroppedAttributesCount(1)
				log.SetFlags(plog.DefaultLogRecordFlags.WithIsSampled(true))
				log.SetTraceID([16]byte{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78})
				log.SetSpanID([8]byte{0x12, 0x23, 0xAD, 0x12, 0x23, 0xAD, 0x12, 0x23})
				log.SetEventName("my.event")
				log.Attributes().PutStr("foo", "bar")

				return log
			},
			expectedResp: push.Entry{
				Timestamp: now,
				Line:      "log body",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "foo",
						Value: "bar",
					},
					{
						Name:  "observed_timestamp",
						Value: fmt.Sprintf("%d", now.UnixNano()+1),
					},
					{
						Name:  "severity_number",
						Value: "5",
					},
					{
						Name:  "severity_text",
						Value: "debug",
					},
					{
						Name:  "dropped_attributes_count",
						Value: "1",
					},
					{
						Name:  "flags",
						Value: fmt.Sprintf("%d", plog.DefaultLogRecordFlags.WithIsSampled(true)),
					},
					{
						Name:  "trace_id",
						Value: "12345678123456781234567812345678",
					},
					{
						Name:  "span_id",
						Value: "1223ad1223ad1223",
					},
					{
						Name:  "event_name",
						Value: "my.event",
					},
				},
			},
		},
		{
			name: "event_name attribute conflicts with EventName field — OTLP field wins",
			buildLogRecord: func() plog.LogRecord {
				log := plog.NewLogRecord()
				log.Body().SetStr("log body")
				log.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				log.SetEventName("otlp.field")
				log.Attributes().PutStr(OTLPEventName, "attribute.value")

				return log
			},
			expectedResp: push.Entry{
				Timestamp: now,
				Line:      "log body",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "event_name",
						Value: "otlp.field",
					},
				},
			},
		},
		{
			name: "event_name only",
			buildLogRecord: func() plog.LogRecord {
				log := plog.NewLogRecord()
				log.Body().SetStr("log body")
				log.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
				log.SetEventName("session.start")

				return log
			},
			expectedResp: push.Entry{
				Timestamp: now,
				Line:      "log body",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "event_name",
						Value: "session.start",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, res, err := otlpLogToPushEntry(tc.buildLogRecord(), DefaultOTLPConfig(defaultGlobalOTLPConfig), false, nil)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResp, res)
		})
	}
}

func TestOtlpError(t *testing.T) {
	for _, tc := range []struct {
		name         string
		msg          string
		inCode       int
		expectedCode int
	}{
		{
			name:         "500 error maps 503",
			msg:          "test error 500 to 503",
			inCode:       http.StatusInternalServerError,
			expectedCode: http.StatusServiceUnavailable,
		},
		{
			name:         "other error",
			msg:          "test error",
			inCode:       http.StatusForbidden,
			expectedCode: http.StatusForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()

			r := httptest.NewRecorder()
			OTLPError(r, tc.msg, tc.inCode, logger)

			require.Equal(t, tc.expectedCode, r.Code)
			require.Equal(t, "application/octet-stream", r.Header().Get("Content-Type"))

			respStatus := &status.Status{}
			require.NoError(t, proto.Unmarshal(r.Body.Bytes(), respStatus))

			require.Equal(t, tc.msg, respStatus.Message)
			require.EqualValues(t, 0, respStatus.Code)
		})
	}
}

func TestOTLPLogAttributesAsIndexLabels(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	// Create a custom OTLP config that indexes log attributes
	customOTLPConfig := DefaultOTLPConfig(GlobalOTLPConfig{
		DefaultOTLPResourceAttributesAsIndexLabels: []string{"service.name"},
	})

	// Override the LogAttributes to include IndexLabel action
	customOTLPConfig.LogAttributes = []AttributesConfig{
		{
			// Index detected_level and log.level as labels
			Action:     IndexLabel,
			Attributes: []string{"detected_level", "log.level"},
		},
		{
			// Keep other attributes as structured metadata
			Action:     StructuredMetadata,
			Attributes: []string{"trace_id", "error_code", "component"},
		},
	}

	// Generate logs with different log.level attributes
	generateLogs := func() plog.Logs {
		ld := plog.NewLogs()

		// Create resource with service name
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "test-service")

		// Create scope logs
		sl := rl.ScopeLogs().AppendEmpty()

		// Add log with "info" level
		infoLog := sl.LogRecords().AppendEmpty()
		infoLog.Body().SetStr("This is an info message")
		infoLog.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		infoLog.Attributes().PutStr("detected_level", "info")
		infoLog.Attributes().PutStr("trace_id", "abc123")

		// Add log with "error" level
		errorLog := sl.LogRecords().AppendEmpty()
		errorLog.Body().SetStr("This is an error message")
		errorLog.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		errorLog.Attributes().PutStr("detected_level", "error")
		errorLog.Attributes().PutStr("error_code", "500")

		// Add log with "debug" level using log.level instead
		debugLog := sl.LogRecords().AppendEmpty()
		debugLog.Body().SetStr("This is a debug message")
		debugLog.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		debugLog.Attributes().PutStr("log.level", "debug")
		debugLog.Attributes().PutStr("component", "database")

		return ld
	}

	// Run the test
	stats := NewPushStats()
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	// All logs will use the same policy for simplicity
	streamResolver.policyForOverride = func(_ context.Context, _ labels.Labels) string {
		return "test-policy"
	}

	// Convert OTLP logs to Loki push request
	pushReq, err := otlpToLokiPushRequest(
		context.Background(),
		generateLogs(),
		"test-user",
		customOTLPConfig,
		nil,
		[]string{}, // No service name discovery needed
		false,
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		constants.OTLP,
	)
	require.NoError(t, err)

	// Debug: Print the actual streams we got
	t.Logf("Number of streams: %d", len(pushReq.Streams))
	for i, stream := range pushReq.Streams {
		t.Logf("Stream %d: Labels=%s, Entries=%d", i, stream.Labels, len(stream.Entries))
	}

	// Filter out empty streams
	nonEmptyStreams := make([]logproto.Stream, 0, len(pushReq.Streams))
	for _, stream := range pushReq.Streams {
		if len(stream.Entries) > 0 {
			nonEmptyStreams = append(nonEmptyStreams, stream)
		}
	}

	// Verify the streams were created with the correct labels
	require.Equal(t, 3, len(nonEmptyStreams), "Should have 3 non-empty streams (one for each log level)")

	// Create a map of streams by labels for easier verification
	streamsByLabels := make(map[string]logproto.Stream)
	for _, stream := range nonEmptyStreams {
		streamsByLabels[stream.Labels] = stream
	}

	// Check for each expected log level in the streams
	infoStreamFound := false
	errorStreamFound := false
	debugStreamFound := false

	for lbs, stream := range streamsByLabels {
		t.Logf("Checking stream with labels: %s", lbs)

		if strings.Contains(lbs, "detected_level=\"info\"") {
			infoStreamFound = true
			require.Equal(t, "This is an info message", stream.Entries[0].Line)
		}
		if strings.Contains(lbs, "detected_level=\"error\"") {
			errorStreamFound = true
			require.Equal(t, "This is an error message", stream.Entries[0].Line)
		}
		if strings.Contains(lbs, "log_level=\"debug\"") {
			debugStreamFound = true
			require.Equal(t, "This is a debug message", stream.Entries[0].Line)
		}
	}

	require.True(t, infoStreamFound, "Stream with info level not found")
	require.True(t, errorStreamFound, "Stream with error level not found")
	require.True(t, debugStreamFound, "Stream with debug level not found")

	// Verify stats
	require.Equal(t, int64(3), stats.PolicyNumLines["test-policy"], "Should have counted 3 log lines")
}

func TestOTLPStructuredMetadataCalculation(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	generateLogs := func() plog.Logs {
		ld := plog.NewLogs()

		// Create resource with attributes
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "test-service")
		rl.Resource().Attributes().PutStr("resource.key", "resource.value")

		// Create scope with attributes
		sl := rl.ScopeLogs().AppendEmpty()
		sl.Scope().SetName("test-scope")
		sl.Scope().Attributes().PutStr("scope.key", "scope.value")

		// Add a log record with minimal metadata
		logRecord := sl.LogRecords().AppendEmpty()
		logRecord.Body().SetStr("Test entry with minimal metadata")
		logRecord.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		logRecord.Attributes().PutStr("entry.key", "entry.value")

		return ld
	}

	// Run the test
	stats := NewPushStats()
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	streamResolver.policyForOverride = func(_ context.Context, _ labels.Labels) string {
		return "test-policy"
	}

	// Convert OTLP logs to Loki push request
	pushReq, err := otlpToLokiPushRequest(
		context.Background(),
		generateLogs(),
		"test-user",
		DefaultOTLPConfig(defaultGlobalOTLPConfig),
		nil,        // tenantConfigs
		[]string{}, // discoverServiceName
		false,
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		constants.OTLP,
	)
	require.NoError(t, err)

	// Verify there is exactly one stream
	require.Equal(t, 1, len(pushReq.Streams))

	// Verify we have a single entry with all the expected metadata
	stream := pushReq.Streams[0]
	require.Equal(t, 1, len(stream.Entries))

	// Verify the structured metadata bytes are positive
	require.Greater(t, stats.StructuredMetadataBytes["test-policy"][time.Hour], int64(0),
		"Structured metadata bytes should be positive")

	// Verify we can find the resource, scope, and entry metadata in the entry
	entry := stream.Entries[0]

	resourceMetadataFound := false
	scopeMetadataFound := false
	entryMetadataFound := false

	for _, metadata := range entry.StructuredMetadata {
		if metadata.Name == "resource_key" && metadata.Value == "resource.value" {
			resourceMetadataFound = true
		}
		if metadata.Name == "scope_key" && metadata.Value == "scope.value" {
			scopeMetadataFound = true
		}
		if metadata.Name == "entry_key" && metadata.Value == "entry.value" {
			entryMetadataFound = true
		}
	}

	require.True(t, resourceMetadataFound, "Resource metadata should be present in the entry")
	require.True(t, scopeMetadataFound, "Scope metadata should be present in the entry")
	require.True(t, entryMetadataFound, "Entry metadata should be present in the entry")
}

func TestNegativeMetadataScenarioExplicit(t *testing.T) {
	// This test explicitly demonstrates how negative structured metadata size values
	// could occur when subtracting resource/scope attributes from total structured metadata size

	// Setup: Create metadata with a label that would be excluded from size calculation
	resourceMeta := push.LabelsAdapter{
		{Name: "resource_key", Value: "resource_value"}, // 27 bytes
		{Name: "excluded_label", Value: "value"},        // This would be excluded from size calculation
	}

	scopeMeta := push.LabelsAdapter{
		{Name: "scope_key", Value: "scope_value"}, // 20 bytes
	}

	entryMeta := push.LabelsAdapter{
		{Name: "entry_key", Value: "entry_value"}, // 20 bytes
	}

	// ExcludedStructuredMetadataLabels would exclude certain labels
	// from size calculations.
	calculateSize := func(labels push.LabelsAdapter) int {
		size := 0
		for _, label := range labels {
			// Simulate a label being excluded from size calc
			if label.Name != "excluded_label" {
				size += len(label.Name) + len(label.Value)
			}
		}
		return size
	}

	// Calculate sizes with simulated exclusions
	resourceSize := calculateSize(resourceMeta) // 27 bytes (excluded_label not counted)
	scopeSize := calculateSize(scopeMeta)       // 20 bytes
	entrySize := calculateSize(entryMeta)       // 20 bytes

	// The original approach:
	// 1. Add resource and scope attributes to entry metadata
	combined := make(push.LabelsAdapter, 0)
	combined = append(combined, entryMeta...)
	combined = append(combined, resourceMeta...)
	combined = append(combined, scopeMeta...)

	// 2. Calculate combined size (with certain labels excluded)
	combinedSize := calculateSize(combined) // Should be 27 + 20 + 20 = 67 bytes

	// 3. Calculate entry-specific metadata by subtraction
	//    metadataSize := int64(combinedSize - resourceSize - scopeSize)
	oldCalculation := combinedSize - resourceSize - scopeSize

	// Should be: 67 - 27 - 20 = 20 bytes, which equals entrySize

	t.Logf("Resource size: %d bytes", resourceSize)
	t.Logf("Scope size: %d bytes", scopeSize)
	t.Logf("Entry size: %d bytes", entrySize)
	t.Logf("Combined size: %d bytes", combinedSize)
	t.Logf("Old calculation (combined - resource - scope): %d bytes", oldCalculation)

	// Now, to demonstrate how this could produce negative values:
	// In reality, due to potential inconsistencies in how labels were excluded/combined/normalized,
	// the combined size could be LESS than the sum of parts
	simulatedRealCombinedSize := resourceSize + scopeSize - 5 // 5 bytes less than sum

	// Using the original calculation method:
	simulatedRealCalculation := simulatedRealCombinedSize - resourceSize - scopeSize
	// This will be: (27 + 20 - 5) - 27 - 20 = 42 - 47 = -5 bytes

	t.Logf("Simulated real combined size: %d bytes", simulatedRealCombinedSize)
	t.Logf("Simulated real calculation (old method): %d bytes", simulatedRealCalculation)

	// This would be a negative value!
	require.Less(t, simulatedRealCalculation, 0,
		"This demonstrates how the old calculation could produce negative values")

	// Directly use entry's size before combining
	t.Logf("New calculation (direct entry size): %d bytes", entrySize)
	require.Equal(t, entrySize, 20,
		"New calculation provides correct entry size")
	require.Greater(t, entrySize, 0,
		"New calculation always produces non-negative values")
}

func TestOTLPSeverityTextAsLabel(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	// Create a custom OTLP config with severity_text as label enabled
	customOTLPConfig := DefaultOTLPConfig(GlobalOTLPConfig{
		DefaultOTLPResourceAttributesAsIndexLabels: []string{"service.name"},
	})

	// Explicitly set SeverityTextAsLabel to true for this test
	customOTLPConfig.SeverityTextAsLabel = true

	// Generate logs with different severity_text values
	generateLogs := func() plog.Logs {
		ld := plog.NewLogs()

		// Create resource with service name
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "test-service")

		// Create scope logs
		sl := rl.ScopeLogs().AppendEmpty()

		// Add log with "INFO" severity
		infoLog := sl.LogRecords().AppendEmpty()
		infoLog.Body().SetStr("This is an info message")
		infoLog.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		infoLog.SetSeverityText("INFO")

		// Add log with "ERROR" severity
		errorLog := sl.LogRecords().AppendEmpty()
		errorLog.Body().SetStr("This is an error message")
		errorLog.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		errorLog.SetSeverityText("ERROR")

		// Add log with "DEBUG" severity
		debugLog := sl.LogRecords().AppendEmpty()
		debugLog.Body().SetStr("This is a debug message")
		debugLog.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
		debugLog.SetSeverityText("DEBUG")

		return ld
	}

	// Run the test
	stats := NewPushStats()
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	// All logs will use the same policy for simplicity
	streamResolver.policyForOverride = func(_ context.Context, _ labels.Labels) string {
		return "test-policy"
	}

	// Convert OTLP logs to Loki push request
	pushReq, err := otlpToLokiPushRequest(
		context.Background(),
		generateLogs(),
		"test-user",
		customOTLPConfig,
		nil,
		[]string{}, // No service name discovery needed
		false,
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		constants.OTLP,
	)
	require.NoError(t, err)

	// Debug: Print the actual streams we got
	t.Logf("Number of streams: %d", len(pushReq.Streams))
	for i, stream := range pushReq.Streams {
		t.Logf("Stream %d: Labels=%s, Entries=%d", i, stream.Labels, len(stream.Entries))
	}

	// Filter out empty streams
	nonEmptyStreams := make([]logproto.Stream, 0, len(pushReq.Streams))
	for _, stream := range pushReq.Streams {
		if len(stream.Entries) > 0 {
			nonEmptyStreams = append(nonEmptyStreams, stream)
		}
	}

	// Verify the streams were created with the correct labels
	require.Equal(t, 3, len(nonEmptyStreams), "Should have 3 non-empty streams (one for each severity level)")

	// Create a map of streams by labels for easier verification
	streamsByLabels := make(map[string]logproto.Stream)
	for _, stream := range nonEmptyStreams {
		streamsByLabels[stream.Labels] = stream
	}

	// Check for each expected severity level in the streams
	infoStreamFound := false
	errorStreamFound := false
	debugStreamFound := false

	for lbs, stream := range streamsByLabels {
		t.Logf("Checking stream with labels: %s", lbs)

		if strings.Contains(lbs, "severity_text=\"INFO\"") {
			infoStreamFound = true
			require.Equal(t, "This is an info message", stream.Entries[0].Line)
		}
		if strings.Contains(lbs, "severity_text=\"ERROR\"") {
			errorStreamFound = true
			require.Equal(t, "This is an error message", stream.Entries[0].Line)
		}
		if strings.Contains(lbs, "severity_text=\"DEBUG\"") {
			debugStreamFound = true
			require.Equal(t, "This is a debug message", stream.Entries[0].Line)
		}
	}

	// Verify all expected streams were found
	require.True(t, infoStreamFound, "Stream with INFO severity_text not found")
	require.True(t, errorStreamFound, "Stream with ERROR severity_text not found")
	require.True(t, debugStreamFound, "Stream with DEBUG severity_text not found")
}

func simpleOTLPLogs() plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "test-service")
	sl := rl.ScopeLogs().AppendEmpty()
	logRecord := sl.LogRecords().AppendEmpty()
	logRecord.Body().SetStr("test log message")
	logRecord.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
	return ld
}

// largeOTLPLogs creates an OTLP log record which is larger than 1MB
// and will compress to less than 1MB (~3kb depending on the compression algorithm).
func largeOTLPLogs() plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "test-service")
	sl := rl.ScopeLogs().AppendEmpty()
	for i := 0; i < 1024; i++ {
		logRecord := sl.LogRecords().AppendEmpty()
		logRecord.Body().SetStr(strings.Repeat(" ", 1024))
	}
	return ld
}

func createJSON(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	jsonBytes, err := req.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return jsonBytes, nil
}

func createGzipCompressedProtobuf(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	protoBytes, err := req.MarshalProto()
	if err != nil {
		return nil, err
	}
	return compressWithGzip(protoBytes)
}

func createGzipCompressedJSON(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	jsonBytes, err := req.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return compressWithGzip(jsonBytes)
}

func compressWithGzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func createZstdCompressedProtobuf(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	protoBytes, err := req.MarshalProto()
	if err != nil {
		return nil, err
	}
	return compressWithZstd(protoBytes)
}

func createZstdCompressedJSON(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	jsonBytes, err := req.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return compressWithZstd(jsonBytes)
}

func compressWithZstd(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func createLz4CompressedProtobuf(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	protoBytes, err := req.MarshalProto()
	if err != nil {
		return nil, err
	}
	return compressWithLz4(protoBytes)
}

func createLz4CompressedJSON(logs plog.Logs) ([]byte, error) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	jsonBytes, err := req.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return compressWithLz4(jsonBytes)
}

func compressWithLz4(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := lz4.NewWriter(&buf)
	_, err := writer.Write(data)
	if err != nil {
		return nil, err
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func createOTLPLogWithNestedAttributes() plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "test-service")

	nestedMap := rl.Resource().Attributes().PutEmptyMap("nested")
	nestedMap.PutStr("key1", "value1")
	nestedMap.PutInt("key2", 42)

	sl := rl.ScopeLogs().AppendEmpty()
	logRecord := sl.LogRecords().AppendEmpty()
	logRecord.Body().SetStr("test log with nested attributes")
	logRecord.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
	return ld
}

func TestContentEncodingAndLength(t *testing.T) {
	testCases := []struct {
		name                 string
		contentType          string
		contentEncoding      string
		generateBody         func() ([]byte, error)
		expectedError        bool
		expectedErrorMessage string
		expectedLogs         plog.Logs
		maxRecvMsgSize       int
		maxDecompressedSize  int64
	}{
		{
			name:            "identity_valid_json",
			contentType:     "application/json",
			contentEncoding: "",
			generateBody: func() ([]byte, error) {
				return createJSON(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "identity_large_json",
			contentType:     "application/json",
			contentEncoding: "",
			generateBody: func() ([]byte, error) {
				return createJSON(largeOTLPLogs())
			},
			expectedError:        true,
			expectedErrorMessage: "message size too large than max",
			expectedLogs:         simpleOTLPLogs(),
			maxRecvMsgSize:       1 << 20, // 1 MB
		},
		{
			name:            "gzip_valid_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedProtobuf(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "gzip_valid_json",
			contentType:     "application/json",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedJSON(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "gzip_invalid_data",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return []byte("invalid gzip data"), nil
			},
			expectedError:  true,
			expectedLogs:   plog.NewLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "gzip_nested_attributes",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedProtobuf(createOTLPLogWithNestedAttributes())
			},
			expectedError:  false,
			expectedLogs:   createOTLPLogWithNestedAttributes(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "gzip_large_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedProtobuf(largeOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   largeOTLPLogs(),
			maxRecvMsgSize: 1 << 20, // 1 MB
		},
		{
			name:            "gzip_too_large_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedProtobuf(largeOTLPLogs())
			},
			expectedError:        true,
			expectedErrorMessage: "message size too large than max (40961 vs 40960)",
			expectedLogs:         largeOTLPLogs(),
			maxRecvMsgSize:       1 << 12, // 4 KB
			maxDecompressedSize:  40960,   // Explicitly set to trigger error
		},
		{
			name:            "zstd_valid_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "zstd",
			generateBody: func() ([]byte, error) {
				return createZstdCompressedProtobuf(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "zstd_valid_json",
			contentType:     "application/json",
			contentEncoding: "zstd",
			generateBody: func() ([]byte, error) {
				return createZstdCompressedJSON(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "zstd_invalid_data",
			contentType:     "application/x-protobuf",
			contentEncoding: "zstd",
			generateBody: func() ([]byte, error) {
				return []byte("invalid zstd data"), nil
			},
			expectedError:  true,
			expectedLogs:   plog.NewLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "zstd_nested_attributes",
			contentType:     "application/x-protobuf",
			contentEncoding: "zstd",
			generateBody: func() ([]byte, error) {
				return createZstdCompressedProtobuf(createOTLPLogWithNestedAttributes())
			},
			expectedError:  false,
			expectedLogs:   createOTLPLogWithNestedAttributes(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "zstd_too_large_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "zstd",
			generateBody: func() ([]byte, error) {
				return createZstdCompressedProtobuf(largeOTLPLogs())
			},
			expectedError:        true,
			expectedErrorMessage: "message size too large than max (40961 vs 40960)",
			expectedLogs:         largeOTLPLogs(),
			maxRecvMsgSize:       1 << 12, // 4 KB
			maxDecompressedSize:  40960,   // Explicitly set to trigger error
		},
		{
			name:            "lz4_valid_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "lz4",
			generateBody: func() ([]byte, error) {
				return createLz4CompressedProtobuf(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "lz4_valid_json",
			contentType:     "application/json",
			contentEncoding: "lz4",
			generateBody: func() ([]byte, error) {
				return createLz4CompressedJSON(simpleOTLPLogs())
			},
			expectedError:  false,
			expectedLogs:   simpleOTLPLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "lz4_invalid_data",
			contentType:     "application/x-protobuf",
			contentEncoding: "lz4",
			generateBody: func() ([]byte, error) {
				return []byte("invalid lz4 data"), nil
			},
			expectedError:  true,
			expectedLogs:   plog.NewLogs(),
			maxRecvMsgSize: 100 << 20, // 100 MB
		},
		{
			name:            "lz4_too_large_protobuf",
			contentType:     "application/x-protobuf",
			contentEncoding: "lz4",
			generateBody: func() ([]byte, error) {
				return createLz4CompressedProtobuf(largeOTLPLogs())
			},
			expectedError:        true,
			expectedErrorMessage: "message size too large than max (81921 vs 81920)",
			expectedLogs:         largeOTLPLogs(),
			maxRecvMsgSize:       1 << 13, // 8 KB
			maxDecompressedSize:  81920,   // Explicitly set to trigger error
		},
		{
			name:            "unsupported_encoding",
			contentType:     "application/x-protobuf",
			contentEncoding: "br",
			generateBody: func() ([]byte, error) {
				return []byte("dummy brotly data"), nil
			},
			expectedError:        true,
			expectedErrorMessage: "unsupported content encoding br: only gzip, lz4 and zstd are supported",
			expectedLogs:         plog.NewLogs(),
			maxRecvMsgSize:       100 << 20, // 100 MB
		},
		{
			name:            "gzip_with_zero_maxDecompressedSize",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedProtobuf(simpleOTLPLogs())
			},
			expectedError:       false,
			expectedLogs:        simpleOTLPLogs(),
			maxRecvMsgSize:      100 << 20, // 100 MB
			maxDecompressedSize: 0,         // 0 means no limit (should still work for small payloads)
		},
		{
			name:            "gzip_large_with_zero_maxDecompressedSize",
			contentType:     "application/x-protobuf",
			contentEncoding: "gzip",
			generateBody: func() ([]byte, error) {
				return createGzipCompressedProtobuf(largeOTLPLogs())
			},
			expectedError:       false, // No limit when maxDecompressedSize is 0
			expectedLogs:        largeOTLPLogs(),
			maxRecvMsgSize:      1 << 20, // 1 MB
			maxDecompressedSize: 0,       // 0 means no limit
		},
		{
			name:            "zstd_with_zero_maxDecompressedSize",
			contentType:     "application/x-protobuf",
			contentEncoding: "zstd",
			generateBody: func() ([]byte, error) {
				return createZstdCompressedProtobuf(simpleOTLPLogs())
			},
			expectedError:       false,
			expectedLogs:        simpleOTLPLogs(),
			maxRecvMsgSize:      100 << 20, // 100 MB
			maxDecompressedSize: 0,         // 0 means no limit
		},
		{
			name:            "lz4_with_zero_maxDecompressedSize",
			contentType:     "application/x-protobuf",
			contentEncoding: "lz4",
			generateBody: func() ([]byte, error) {
				return createLz4CompressedProtobuf(simpleOTLPLogs())
			},
			expectedError:       false,
			expectedLogs:        simpleOTLPLogs(),
			maxRecvMsgSize:      100 << 20, // 100 MB
			maxDecompressedSize: 0,         // 0 means no limit
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := tc.generateBody()
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/v1/logs", bytes.NewReader(body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Content-Encoding", tc.contentEncoding)

			stats := NewPushStats()
			maxDecompressedSize := tc.maxDecompressedSize
			// Only apply default if maxDecompressedSize is 0 and not explicitly testing zero behavior
			// For test cases with maxDecompressedSize explicitly set to 0, we want to test the actual behavior
			// For other cases, calculate as 10x maxRecvMsgSize (matching Validate() behavior) or use 100MB if maxRecvMsgSize is 0
			zeroMaxDecompressedSizeTests := map[string]bool{
				"gzip_with_zero_maxDecompressedSize":       true,
				"gzip_large_with_zero_maxDecompressedSize": true,
				"zstd_with_zero_maxDecompressedSize":       true,
				"lz4_with_zero_maxDecompressedSize":        true,
			}
			if maxDecompressedSize == 0 && !zeroMaxDecompressedSizeTests[tc.name] {
				if tc.maxRecvMsgSize > 0 {
					maxDecompressedSize = int64(tc.maxRecvMsgSize) * 50 // 50x default
				} else {
					maxDecompressedSize = 5000 << 20 // 5000 MB fallback default (50x 100MB)
				}
			}
			extractedLogs, err := extractLogs(req, tc.maxRecvMsgSize, maxDecompressedSize, stats)

			if tc.expectedError {
				require.Error(t, err)

				if tc.expectedErrorMessage != "" {
					require.Contains(t, err.Error(), tc.expectedErrorMessage)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, extractedLogs)

			require.Equal(t, tc.contentEncoding, stats.ContentEncoding)
			require.Equal(t, tc.contentType, stats.ContentType)
			require.Greater(t, stats.BodySize, int64(0))

			if tc.expectedLogs.ResourceLogs().Len() > 0 {
				require.Equal(t, tc.expectedLogs.ResourceLogs().Len(), extractedLogs.ResourceLogs().Len())

				if tc.expectedLogs.ResourceLogs().Len() > 0 {
					expectedRL := tc.expectedLogs.ResourceLogs().At(0)
					extractedRL := extractedLogs.ResourceLogs().At(0)
					expectedServiceName, _ := expectedRL.Resource().Attributes().Get("service.name")
					extractedServiceName, _ := extractedRL.Resource().Attributes().Get("service.name")
					require.Equal(t, expectedServiceName.AsString(), extractedServiceName.AsString())
					require.Equal(t, expectedRL.ScopeLogs().Len(), extractedRL.ScopeLogs().Len())

					if expectedRL.ScopeLogs().Len() > 0 {
						expectedSL := expectedRL.ScopeLogs().At(0)
						extractedSL := extractedRL.ScopeLogs().At(0)

						require.Equal(t, expectedSL.LogRecords().Len(), extractedSL.LogRecords().Len())
						if expectedSL.LogRecords().Len() > 0 && extractedSL.LogRecords().Len() > 0 {
							expectedLog := expectedSL.LogRecords().At(0)
							extractedLog := extractedSL.LogRecords().At(0)
							require.Equal(t, expectedLog.Body().AsString(), extractedLog.Body().AsString())
						}
					}
				}
			}
		})
	}
}

type otlpTestAttr struct{ name, value string }

// otlpDeferredExpansionHelpers returns builders for OTLP payloads used by the deferred structured
// metadata expansion tests. Attributes are added in slice order so the resulting structured
// metadata order is deterministic.
func otlpAddResource(ld plog.Logs, attrs ...otlpTestAttr) plog.ResourceLogs {
	rl := ld.ResourceLogs().AppendEmpty()
	for _, a := range attrs {
		rl.Resource().Attributes().PutStr(a.name, a.value)
	}
	return rl
}

func otlpAddScope(rl plog.ResourceLogs, scopeName string, attrs ...otlpTestAttr) plog.ScopeLogs {
	sl := rl.ScopeLogs().AppendEmpty()
	if scopeName != "" {
		sl.Scope().SetName(scopeName)
	}
	for _, a := range attrs {
		sl.Scope().Attributes().PutStr(a.name, a.value)
	}
	return sl
}

func otlpAddRecord(sl plog.ScopeLogs, ts time.Time, body string, attrs ...otlpTestAttr) {
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr(body)
	lr.SetTimestamp(pcommon.Timestamp(ts.UnixNano()))
	for _, a := range attrs {
		lr.Attributes().PutStr(a.name, a.value)
	}
}

func runOTLPToLokiPushRequest(t *testing.T, ld plog.Logs, otlpConfig OTLPConfig, deferExpansion bool) (*logproto.PushRequest, *Stats, *MockCustomTracker) {
	t.Helper()

	stats := NewPushStats()
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	pushReq, err := otlpToLokiPushRequest(
		context.Background(),
		ld,
		"fake",
		otlpConfig,
		nil,
		[]string{},
		deferExpansion,
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		constants.OTLP,
	)
	require.NoError(t, err)

	return pushReq, stats, tracker
}

// flatOTLPEntry is an entry with the structured metadata it effectively carries, i.e. its own plus
// the shared one of its stream. It is used to compare the output of the two expansion modes.
type flatOTLPEntry struct {
	labels string
	line   string
	sm     string
}

// effectiveStructuredMetadata renders the effective structured metadata of an entry, in order.
//
// The order is compared on purpose: push.EffectiveStructuredMetadata materializes the pool in the
// same own, resource, scope order the expanded mode appends in, so the two modes must produce the
// very same list of pairs, duplicate names included, and not merely the same set.
func effectiveStructuredMetadata(stream *logproto.Stream, entry *logproto.Entry) string {
	resource, scope := stream.SharedFor(entry)
	effective := push.EffectiveStructuredMetadata(resource, scope, entry.StructuredMetadata)

	pairs := make([]string, 0, len(effective))
	for _, l := range effective {
		pairs = append(pairs, fmt.Sprintf("%s=%s", l.Name, l.Value))
	}
	return strings.Join(pairs, ",")
}

func flattenOTLPPushRequest(pr *logproto.PushRequest) []flatOTLPEntry {
	out := make([]flatOTLPEntry, 0, len(pr.Streams))
	for i := range pr.Streams {
		stream := &pr.Streams[i]
		for j := range stream.Entries {
			out = append(out, flatOTLPEntry{
				labels: stream.Labels,
				line:   stream.Entries[j].Line,
				sm:     effectiveStructuredMetadata(stream, &stream.Entries[j]),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].labels != out[j].labels {
			return out[i].labels < out[j].labels
		}
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].sm < out[j].sm
	})
	return out
}

func nonEmptyOTLPStreams(pr *logproto.PushRequest) []logproto.Stream {
	out := make([]logproto.Stream, 0, len(pr.Streams))
	for _, stream := range pr.Streams {
		if len(stream.Entries) > 0 {
			out = append(out, stream)
		}
	}
	return out
}

func TestOTLPDeferStructuredMetadataExpansion(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)

	// Two resources that resolve to the same stream labels ({service_name="svc"}) but carry
	// different resource attributes, plus a second scope under the first resource.
	generateLogs := func() plog.Logs {
		ld := plog.NewLogs()

		resourceA := otlpAddResource(ld,
			otlpTestAttr{"service.name", "svc"},
			otlpTestAttr{"host.name", "host-a"},
		)
		scopeA1 := otlpAddScope(resourceA, "scope-1", otlpTestAttr{"scope.attr", "one"})
		otlpAddRecord(scopeA1, now, "a1", otlpTestAttr{"entry.attr", "e1"})
		scopeA2 := otlpAddScope(resourceA, "scope-2", otlpTestAttr{"scope.attr", "two"})
		otlpAddRecord(scopeA2, now, "a2")

		resourceB := otlpAddResource(ld,
			otlpTestAttr{"service.name", "svc"},
			otlpTestAttr{"host.name", "host-b"},
		)
		scopeB1 := otlpAddScope(resourceB, "")
		otlpAddRecord(scopeB1, now, "b1")

		return ld
	}

	t.Run("without deferred expansion all entries merge into a single stream", func(t *testing.T) {
		pushReq, _, _ := runOTLPToLokiPushRequest(t, generateLogs(), otlpConfig, false)

		streams := nonEmptyOTLPStreams(pushReq)
		require.Len(t, streams, 1)
		require.Equal(t, `{service_name="svc"}`, streams[0].Labels)
		require.Len(t, streams[0].Entries, 3)
		require.Empty(t, streams[0].SharedStructuredMetadataSets)

		// Every entry carries the resource and scope attributes of its origin.
		for _, entry := range streams[0].Entries {
			require.Contains(t, fmt.Sprint(entry.StructuredMetadata), "host_name")
			require.Zero(t, entry.SharedResourceRef)
			require.Zero(t, entry.SharedScopeRef)
		}
	})

	t.Run("with deferred expansion one stream pools the resource and scope sets", func(t *testing.T) {
		pushReq, _, _ := runOTLPToLokiPushRequest(t, generateLogs(), otlpConfig, true)

		streams := nonEmptyOTLPStreams(pushReq)
		// Grouping is by labels alone: the three resource/scope combinations share one stream and
		// are told apart by the pool.
		require.Len(t, streams, 1)
		stream := streams[0]
		require.Equal(t, `{service_name="svc"}`, stream.Labels)
		require.Len(t, stream.Entries, 3)
		require.NoError(t, stream.ValidateSharedRefs())

		// Resource and scope sets are pooled separately, in the order they are first seen: the two
		// scopes of resource A share the single pooled copy of A's attributes.
		require.Equal(t, []logproto.SharedStructuredMetadataSet{
			{Attrs: []push.LabelAdapter{{Name: "host_name", Value: "host-a"}}},
			{Attrs: []push.LabelAdapter{
				{Name: "scope_attr", Value: "one"},
				{Name: "scope_name", Value: "scope-1"},
			}},
			{Attrs: []push.LabelAdapter{
				{Name: "scope_attr", Value: "two"},
				{Name: "scope_name", Value: "scope-2"},
			}},
			{Attrs: []push.LabelAdapter{{Name: "host_name", Value: "host-b"}}},
		}, stream.SharedStructuredMetadataSets)

		byLine := map[string]*logproto.Entry{}
		for i := range stream.Entries {
			byLine[stream.Entries[i].Line] = &stream.Entries[i]
		}
		require.Len(t, byLine, 3)

		require.Equal(t, uint32(1), byLine["a1"].SharedResourceRef)
		require.Equal(t, uint32(2), byLine["a1"].SharedScopeRef)
		require.Equal(t, uint32(1), byLine["a2"].SharedResourceRef, "the two scopes of resource A reference the same resource set")
		require.Equal(t, uint32(3), byLine["a2"].SharedScopeRef)
		require.Equal(t, uint32(4), byLine["b1"].SharedResourceRef)
		require.Zero(t, byLine["b1"].SharedScopeRef, "a scope with no attributes is not pooled")

		// The references resolve to what the entry effectively carries.
		resource, scope := stream.SharedFor(byLine["a1"])
		require.Equal(t, push.LabelsAdapter{{Name: "host_name", Value: "host-a"}}, resource)
		require.Equal(t, push.LabelsAdapter{
			{Name: "scope_attr", Value: "one"},
			{Name: "scope_name", Value: "scope-1"},
		}, scope)

		// Entries only carry their own attributes.
		require.Equal(t, push.LabelsAdapter{{Name: "entry_attr", Value: "e1"}}, byLine["a1"].StructuredMetadata)
		require.Empty(t, byLine["a2"].StructuredMetadata)
		require.Empty(t, byLine["b1"].StructuredMetadata)
	})

	t.Run("two resources with identical attributes and labels dedupe to one pool set", func(t *testing.T) {
		generate := func() plog.Logs {
			ld := plog.NewLogs()
			for _, line := range []string{"first", "second"} {
				rl := otlpAddResource(ld,
					otlpTestAttr{"service.name", "svc"},
					otlpTestAttr{"host.name", "host-a"},
				)
				otlpAddRecord(otlpAddScope(rl, ""), now, line)
			}
			return ld
		}

		pushReq, _, _ := runOTLPToLokiPushRequest(t, generate(), otlpConfig, true)

		streams := nonEmptyOTLPStreams(pushReq)
		require.Len(t, streams, 1)
		require.Len(t, streams[0].Entries, 2)
		require.NoError(t, streams[0].ValidateSharedRefs())

		// Byte-identical attribute sets are pooled once and both entries point at that one set.
		require.Equal(t, []logproto.SharedStructuredMetadataSet{
			{Attrs: []push.LabelAdapter{{Name: "host_name", Value: "host-a"}}},
		}, streams[0].SharedStructuredMetadataSets)
		for _, entry := range streams[0].Entries {
			require.Equal(t, uint32(1), entry.SharedResourceRef)
			require.Zero(t, entry.SharedScopeRef)
		}
	})

	t.Run("entries promoted to their own labels build their own pool", func(t *testing.T) {
		cfg := DefaultOTLPConfig(defaultGlobalOTLPConfig)
		cfg.LogAttributes = []AttributesConfig{
			{Action: IndexLabel, Attributes: []string{"promoted"}},
		}

		generate := func() plog.Logs {
			ld := plog.NewLogs()
			for _, host := range []string{"host-a", "host-b"} {
				rl := otlpAddResource(ld,
					otlpTestAttr{"service.name", "svc"},
					otlpTestAttr{"host.name", host},
				)
				otlpAddRecord(otlpAddScope(rl, ""), now, "line-"+host, otlpTestAttr{"promoted", "yes"})
			}
			return ld
		}

		pushReq, _, _ := runOTLPToLokiPushRequest(t, generate(), cfg, true)

		streams := nonEmptyOTLPStreams(pushReq)
		// Same promoted label set for both resources: one wire stream, whose own pool holds the
		// two resource sets its entries reference.
		require.Len(t, streams, 1)
		stream := streams[0]
		require.Equal(t, `{promoted="yes", service_name="svc"}`, stream.Labels)
		require.NoError(t, stream.ValidateSharedRefs())
		require.Equal(t, []logproto.SharedStructuredMetadataSet{
			{Attrs: []push.LabelAdapter{{Name: "host_name", Value: "host-a"}}},
			{Attrs: []push.LabelAdapter{{Name: "host_name", Value: "host-b"}}},
		}, stream.SharedStructuredMetadataSets)

		shared := map[string]string{}
		for i := range stream.Entries {
			resource, scope := stream.SharedFor(&stream.Entries[i])
			require.Empty(t, scope)
			require.Len(t, resource, 1)
			require.Equal(t, "host_name", resource[0].Name)
			shared[stream.Entries[i].Line] = resource[0].Value
		}
		require.Equal(t, map[string]string{"line-host-a": "host-a", "line-host-b": "host-b"}, shared)
	})

	t.Run("entries, stats and usage tracking are identical with and without deferred expansion", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			cfg      OTLPConfig
			generate func() plog.Logs
		}{
			{
				name:     "multiple resources and scopes",
				cfg:      otlpConfig,
				generate: generateLogs,
			},
			{
				name: "resource attributes as structured metadata only",
				cfg:  OTLPConfig{},
				generate: func() plog.Logs {
					ld := plog.NewLogs()
					rl := otlpAddResource(ld, otlpTestAttr{"service.name", "svc"}, otlpTestAttr{"host.name", "host-a"})
					otlpAddRecord(otlpAddScope(rl, "scope-1"), now, "only-line", otlpTestAttr{"entry.attr", "e1"})
					return ld
				},
			},
			{
				name: "promoted log attributes",
				cfg: func() OTLPConfig {
					cfg := DefaultOTLPConfig(defaultGlobalOTLPConfig)
					cfg.LogAttributes = []AttributesConfig{{Action: IndexLabel, Attributes: []string{"promoted"}}}
					return cfg
				}(),
				generate: func() plog.Logs {
					ld := plog.NewLogs()
					rl := otlpAddResource(ld, otlpTestAttr{"service.name", "svc"}, otlpTestAttr{"host.name", "host-a"})
					sl := otlpAddScope(rl, "scope-1", otlpTestAttr{"scope.attr", "one"})
					otlpAddRecord(sl, now, "promoted-line", otlpTestAttr{"promoted", "yes"})
					otlpAddRecord(sl, now, "plain-line")
					return ld
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				expandedReq, expandedStats, expandedTracker := runOTLPToLokiPushRequest(t, tc.generate(), tc.cfg, false)
				deferredReq, deferredStats, deferredTracker := runOTLPToLokiPushRequest(t, tc.generate(), tc.cfg, true)

				// Same entries with the same effective structured metadata, in the same order.
				require.Equal(t, flattenOTLPPushRequest(expandedReq), flattenOTLPPushRequest(deferredReq))

				// Received bytes, structured metadata bytes, expanded bytes, line counts and stream
				// label sizes must not change: all of them are unexpanded-accounting invariant.
				require.Equal(t, expandedStats, deferredStats)

				// The GEL usage tracker reports unexpanded bytes and must be unaffected too.
				require.Equal(t, expandedTracker.receivedBytes, deferredTracker.receivedBytes)
				require.Equal(t, expandedTracker.Total(), deferredTracker.Total())

				// Sanity check that the expanded-equivalent size is still reported and accounts for
				// the resource/scope attributes of every entry.
				var deferredWireSize int64
				for _, stream := range deferredReq.Streams {
					require.NoError(t, stream.ValidateSharedRefs())
					for i := range stream.Entries {
						deferredWireSize += int64(util.EntryTotalSize(&stream.Entries[i]))
					}
					deferredWireSize += int64(util.SharedSetsSize(stream.SharedStructuredMetadataSets))
				}
				require.Greater(t, deferredStats.TotalExpandedEntriesSize, int64(0))
				require.GreaterOrEqual(t, deferredStats.TotalExpandedEntriesSize, deferredWireSize)
			})
		}
	})
}

// TestParseOTLPRequestDeferStructuredMetadataExpansion covers the per-tenant limit plumbing:
// ParseOTLPRequest must honour Limits.OTLPDeferStructuredMetadataExpansion.
func TestParseOTLPRequestDeferStructuredMetadataExpansion(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	generateBody := func(t *testing.T) []byte {
		t.Helper()
		ld := plog.NewLogs()
		rl := otlpAddResource(ld, otlpTestAttr{"service.name", "svc"}, otlpTestAttr{"host.name", "host-a"})
		otlpAddRecord(otlpAddScope(rl, ""), now, "a line")
		body, err := createJSON(ld)
		require.NoError(t, err)
		return body
	}

	for _, tc := range []struct {
		name           string
		deferExpansion bool
		expectedPool   []logproto.SharedStructuredMetadataSet
		expectedRef    uint32
		expectedOwn    push.LabelsAdapter
	}{
		{
			name:           "limit disabled",
			deferExpansion: false,
			expectedPool:   nil,
			expectedOwn:    push.LabelsAdapter{{Name: "host_name", Value: "host-a"}},
		},
		{
			name:           "limit enabled",
			deferExpansion: true,
			expectedPool: []logproto.SharedStructuredMetadataSet{
				{Attrs: []push.LabelAdapter{{Name: "host_name", Value: "host-a"}}},
			},
			expectedRef: 1,
			expectedOwn: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &fakeLimits{deferStructuredMetadataExpansion: tc.deferExpansion}

			req := httptest.NewRequest("POST", "/otlp/v1/logs", bytes.NewReader(generateBody(t)))
			req.Header.Set("Content-Type", applicationJSON)

			pushReq, _, err := ParseOTLPRequest("fake", req, limits, nil, 100<<20, 100<<20, NewMockTracker(), newMockStreamResolver("fake", limits), log.NewNopLogger())
			require.NoError(t, err)

			streams := nonEmptyOTLPStreams(pushReq)
			require.Len(t, streams, 1)
			require.Equal(t, tc.expectedPool, streams[0].SharedStructuredMetadataSets)
			require.Equal(t, tc.expectedRef, streams[0].Entries[0].SharedResourceRef)
			require.Zero(t, streams[0].Entries[0].SharedScopeRef)
			if len(tc.expectedOwn) == 0 {
				require.Empty(t, streams[0].Entries[0].StructuredMetadata)
			} else {
				require.Equal(t, tc.expectedOwn, streams[0].Entries[0].StructuredMetadata)
			}
		})
	}
}
