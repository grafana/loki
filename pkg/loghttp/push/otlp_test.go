package push

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/push"
)

func TestOTLPToLokiPushRequest(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	for _, tc := range []struct {
		name                string
		generateLogs        func() plog.Logs
		expectedPushRequest logproto.PushRequest
		expectedStats       Stats
		otlpConfig          OTLPConfig
		tracker             UsageTracker
	}{
		{
			name: "no logs",
			generateLogs: func() plog.Logs {
				return plog.NewLogs()
			},
			expectedPushRequest: logproto.PushRequest{},
			expectedStats:       *newPushStats(),
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
			expectedStats:       *newPushStats(),
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
				NumLines: 1,
				LogLinesBytes: map[time.Duration]int64{
					time.Hour: 9,
				},
				StructuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 0,
				},
				ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{
					time.Hour: nil,
				},
				StreamLabelsSize:         21,
				MostRecentEntryTimestamp: now,
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
				NumLines: 1,
				LogLinesBytes: map[time.Duration]int64{
					time.Hour: 9,
				},
				StructuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 0,
				},
				ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{
					time.Hour: nil,
				},
				StreamLabelsSize:         27,
				MostRecentEntryTimestamp: now,
			},
		},
		{
			name:       "service.name not defined in resource attributes",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			tracker:    NewMockTracker(),
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
				NumLines: 1,
				LogLinesBytes: map[time.Duration]int64{
					time.Hour: 9,
				},
				StructuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 0,
				},
				ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{
					time.Hour: nil,
				},
				StreamLabelsSize:         47,
				MostRecentEntryTimestamp: now,
				/*
					logLinesBytesCustomTrackers: []customTrackerPair{
						{
							Labels: []labels.Label{
								{Name: "service_namespace", Value: "foo"},
								{Name: "tracker", Value: "foo"},
							},
							Bytes: map[time.Duration]int64{
								time.Hour: 9,
							},
						},
					},
					structuredMetadataBytesCustomTrackers: []customTrackerPair{
						{
							Labels: []labels.Label{
								{Name: "service_namespace", Value: "foo"},
								{Name: "tracker", Value: "foo"},
							},
							Bytes: map[time.Duration]int64{
								time.Hour: 0,
							},
						},
					},
				*/
			},
			//expectedTrackedUsaged:
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
				NumLines: 2,
				LogLinesBytes: map[time.Duration]int64{
					time.Hour: 26,
				},
				StructuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 37,
				},
				ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{
					time.Hour: []push.LabelAdapter{
						{Name: "service_image", Value: "loki"},
						{Name: "op", Value: "buzz"},
						{Name: "scope_name", Value: "fizz"},
					},
				},
				StreamLabelsSize:         21,
				MostRecentEntryTimestamp: now,
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
				NumLines: 2,
				LogLinesBytes: map[time.Duration]int64{
					time.Hour: 26,
				},
				StructuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 97,
				},
				ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{
					time.Hour: []push.LabelAdapter{
						{Name: "resource_nested_foo", Value: "bar"},
						{Name: "scope_nested_foo", Value: "bar"},
						{Name: "scope_name", Value: "fizz"},
					},
				},
				StreamLabelsSize:         21,
				MostRecentEntryTimestamp: now,
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
						}, {
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
				NumLines: 2,
				LogLinesBytes: map[time.Duration]int64{
					time.Hour: 26,
				},
				StructuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 113,
				},
				ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{
					time.Hour: []push.LabelAdapter{
						{Name: "pod_ip", Value: "10.200.200.200"},
						{Name: "resource_nested_foo", Value: "bar"},
						{Name: "scope_nested_foo", Value: "bar"},
						{Name: "scope_name", Value: "fizz"},
					},
				},
				StreamLabelsSize:         42,
				MostRecentEntryTimestamp: now,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := newPushStats()
			pushReq := otlpToLokiPushRequest(tc.generateLogs(), "foo", fakeRetention{}, tc.otlpConfig, tc.tracker, stats)
			require.Equal(t, tc.expectedPushRequest, *pushReq)
			require.Equal(t, tc.expectedStats, *stats)
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
			require.Equal(t, tc.expectedResp, otlpLogToPushEntry(tc.buildLogRecord(), DefaultOTLPConfig(defaultGlobalOTLPConfig)))
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
