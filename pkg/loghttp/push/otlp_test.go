package push

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
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
		expectedStats       pushStats
	}{
		{
			name: "no logs",
			generateLogs: func() plog.Logs {
				return plog.NewLogs()
			},
			expectedPushRequest: logproto.PushRequest{},
			expectedStats:       *newPushStats(),
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
		},
		{
			name: "resource with a log entry",
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
								Timestamp: now,
								Line:      "test body",
							},
						},
					},
				},
			},
			expectedStats: pushStats{
				numLines: 1,
				logLinesBytes: map[time.Duration]int64{
					time.Hour: 9,
				},
				structuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 0,
				},
				streamLabelsSize:         21,
				mostRecentEntryTimestamp: now,
			},
		},
		{
			name: "resource attributes and scope attributes stored as structured metadata",
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
			expectedStats: pushStats{
				numLines: 2,
				logLinesBytes: map[time.Duration]int64{
					time.Hour: 26,
				},
				structuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 37,
				},
				streamLabelsSize:         21,
				mostRecentEntryTimestamp: now,
			},
		},
		{
			name: "attributes with nested data",
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
			expectedStats: pushStats{
				numLines: 2,
				logLinesBytes: map[time.Duration]int64{
					time.Hour: 26,
				},
				structuredMetadataBytes: map[time.Duration]int64{
					time.Hour: 97,
				},
				streamLabelsSize:         21,
				mostRecentEntryTimestamp: now,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pushReq, stats := otlpToLokiPushRequest(tc.generateLogs(), "foo", fakeRetention{})
			require.Equal(t, tc.expectedPushRequest, *pushReq)
			require.Equal(t, tc.expectedStats, *stats)
		})
	}
}

type fakeRetention struct{}

func (f fakeRetention) RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration {
	return time.Hour
}
