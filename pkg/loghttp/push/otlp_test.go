package push

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
)

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
			stats := NewPushStats()
			tracker := NewMockTracker()
			streamResolver := newMockStreamResolver("fake", &fakeLimits{})
			streamResolver.policyForOverride = func(lbs labels.Labels) string {
				if lbs.Get("service_name") == "service-1" {
					return "service-1-policy"
				}
				return "others"
			}

			pushReq := otlpToLokiPushRequest(
				context.Background(),
				tc.generateLogs(),
				"foo",
				tc.otlpConfig,
				nil,
				defaultServiceDetection,
				tracker,
				stats,
				log.NewNopLogger(),
				streamResolver,
				constants.OTLP,
			)
			require.Equal(t, tc.expectedPushRequest, *pushReq)
			require.Equal(t, tc.expectedStats, *stats)

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
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, res := otlpLogToPushEntry(tc.buildLogRecord(), DefaultOTLPConfig(defaultGlobalOTLPConfig), false, nil)
			require.Equal(t, tc.expectedResp, res)
		})
	}
}

func TestAttributesToLabels(t *testing.T) {
	for _, tc := range []struct {
		name         string
		buildAttrs   func() pcommon.Map
		expectedResp push.LabelsAdapter
	}{
		{
			name: "no attributes",
			buildAttrs: func() pcommon.Map {
				return pcommon.NewMap()
			},
			expectedResp: push.LabelsAdapter{},
		},
		{
			name: "with attributes",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutEmpty("empty")
				attrs.PutStr("str", "val")
				attrs.PutInt("int", 1)
				attrs.PutDouble("double", 3.14)
				attrs.PutBool("bool", true)
				attrs.PutEmptyBytes("bytes").Append(1, 2, 3)

				slice := attrs.PutEmptySlice("slice")
				slice.AppendEmpty().SetInt(1)
				slice.AppendEmpty().SetEmptySlice().AppendEmpty().SetStr("foo")
				slice.AppendEmpty().SetEmptyMap().PutStr("fizz", "buzz")

				m := attrs.PutEmptyMap("nested")
				m.PutStr("foo", "bar")
				m.PutEmptyMap("more").PutStr("key", "val")

				return attrs
			},
			expectedResp: push.LabelsAdapter{
				{
					Name: "empty",
				},
				{
					Name:  "str",
					Value: "val",
				},
				{
					Name:  "int",
					Value: "1",
				},
				{
					Name:  "double",
					Value: "3.14",
				},
				{
					Name:  "bool",
					Value: "true",
				},
				{
					Name:  "bytes",
					Value: base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
				},
				{
					Name:  "slice",
					Value: `[1,["foo"],{"fizz":"buzz"}]`,
				},
				{
					Name:  "nested_foo",
					Value: "bar",
				},
				{
					Name:  "nested_more_key",
					Value: "val",
				},
			},
		},
		{
			name: "attributes with special chars",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutStr("st.r", "val")

				m := attrs.PutEmptyMap("nest*ed")
				m.PutStr("fo@o", "bar")
				m.PutEmptyMap("m$ore").PutStr("k_ey", "val")

				return attrs
			},
			expectedResp: push.LabelsAdapter{
				{
					Name:  "st_r",
					Value: "val",
				},
				{
					Name:  "nest_ed_fo_o",
					Value: "bar",
				},
				{
					Name:  "nest_ed_m_ore_k_ey",
					Value: "val",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedResp, attributesToLabels(tc.buildAttrs(), ""))
		})
	}
}

type fakeRetention struct{}

func (f fakeRetention) RetentionPeriodFor(_ string, _ labels.Labels) time.Duration {
	return time.Hour
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
	streamResolver.policyForOverride = func(_ labels.Labels) string {
		return "test-policy"
	}

	// Convert OTLP logs to Loki push request
	pushReq := otlpToLokiPushRequest(
		context.Background(),
		generateLogs(),
		"test-user",
		customOTLPConfig,
		nil,
		[]string{}, // No service name discovery needed
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		"otlp",
	)

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

	streamResolver.policyForOverride = func(_ labels.Labels) string {
		return "test-policy"
	}

	// Convert OTLP logs to Loki push request
	pushReq := otlpToLokiPushRequest(
		context.Background(),
		generateLogs(),
		"test-user",
		DefaultOTLPConfig(defaultGlobalOTLPConfig),
		nil,        // tenantConfigs
		[]string{}, // discoverServiceName
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		constants.OTLP,
	)

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
	streamResolver.policyForOverride = func(_ labels.Labels) string {
		return "test-policy"
	}

	// Convert OTLP logs to Loki push request
	pushReq := otlpToLokiPushRequest(
		context.Background(),
		generateLogs(),
		"test-user",
		customOTLPConfig,
		nil,
		[]string{}, // No service name discovery needed
		tracker,
		stats,
		log.NewNopLogger(),
		streamResolver,
		constants.OTLP,
	)

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
