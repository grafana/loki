package distributor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/c2h5oh/datasize"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/otlptranslator"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/limits"
	limits_frontend "github.com/grafana/loki/v3/pkg/limits/frontend"
	limits_frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	limitsproto "github.com/grafana/loki/v3/pkg/limits/proto"
	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	loki_net "github.com/grafana/loki/v3/pkg/util/net"
	"github.com/grafana/loki/v3/pkg/util/test"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/grafana/loki/pkg/push"
)

const (
	smValidName    = "valid_name"
	smInvalidName  = "invalid-name"
	smValidValue   = "valid-value私"
	smInvalidValue = "valid-value�"
)

var (
	success = &logproto.PushResponse{}
	ctx     = user.InjectOrgID(context.Background(), "test")
)

func TestDistributor(t *testing.T) {
	lineSize := 10
	ingestionRateLimit := datasize.ByteSize(400)
	ingestionRateLimitMB := ingestionRateLimit.MBytes() // 400 Bytes/s limit

	for i, tc := range []struct {
		lines            int
		maxLineSize      uint64
		streams          int
		mangleLabels     int
		expectedResponse *logproto.PushResponse
		expectedErrors   []error
	}{
		{
			lines:            10,
			streams:          1,
			expectedResponse: success,
		},
		{
			lines:          100,
			streams:        1,
			expectedErrors: []error{httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", ingestionRateLimit, 100, 100*lineSize)},
		},
		{
			lines:            100,
			streams:          1,
			maxLineSize:      1,
			expectedResponse: success,
			expectedErrors:   []error{httpgrpc.Errorf(http.StatusBadRequest, "100 errors like: %s", fmt.Sprintf(validation.LineTooLongErrorMsg, 1, "{foo=\"bar\"}", 10))},
		},
		{
			lines:            100,
			streams:          1,
			mangleLabels:     1,
			expectedResponse: success,
			expectedErrors:   []error{httpgrpc.Errorf(http.StatusBadRequest, validation.InvalidLabelsErrorMsg, "{ab\"", "1:4: parse error: unterminated quoted string")},
		},
		{
			lines:            10,
			streams:          2,
			mangleLabels:     1,
			maxLineSize:      1,
			expectedResponse: success,
			expectedErrors: []error{
				httpgrpc.Errorf(http.StatusBadRequest, ""),
				fmt.Errorf("1 errors like: %s", fmt.Sprintf(validation.InvalidLabelsErrorMsg, "{ab\"", "1:4: parse error: unterminated quoted string")),
				fmt.Errorf("10 errors like: %s", fmt.Sprintf(validation.LineTooLongErrorMsg, 1, "{foo=\"bar\"}", 10)),
			},
		},
	} {
		t.Run(fmt.Sprintf("[%d](lines=%v)", i, tc.lines), func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.IngestionRateMB = ingestionRateLimitMB
			limits.IngestionBurstSizeMB = ingestionRateLimitMB
			limits.MaxLineSize = loki_flagext.ByteSize(tc.maxLineSize)

			distributors, _ := prepare(t, 1, 5, limits, nil)

			var request logproto.PushRequest
			for i := 0; i < tc.streams; i++ {
				req := makeWriteRequest(tc.lines, lineSize)
				request.Streams = append(request.Streams, req.Streams[0])
			}

			for i := 0; i < tc.mangleLabels; i++ {
				request.Streams[i].Labels = `{ab"`
			}

			response, err := distributors[i%len(distributors)].Push(ctx, &request)
			assert.Equal(t, tc.expectedResponse, response)
			if len(tc.expectedErrors) > 0 {
				for _, expectedError := range tc.expectedErrors {
					if len(tc.expectedErrors) == 1 {
						assert.Equal(t, expectedError, err)
					} else {
						assert.Contains(t, err.Error(), expectedError.Error())
					}
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDuplicateTimestampTracker(t *testing.T) {
	var tracker duplicateTimestampTracker
	var entry logproto.Entry
	at := time.Unix(123456, 0)

	for i, tc := range []struct {
		line            string
		timestamp, want time.Time
	}{
		{line: "first", timestamp: at, want: at},
		{line: "", timestamp: at, want: at.Add(time.Nanosecond)},
		{line: "third", timestamp: at, want: at.Add(2 * time.Nanosecond)},
		{line: "third", timestamp: at.Add(2 * time.Nanosecond), want: at.Add(2 * time.Nanosecond)},
		{line: "fourth", timestamp: at.Add(2 * time.Nanosecond), want: at.Add(3 * time.Nanosecond)},
		{line: "later", timestamp: at.Add(time.Second), want: at.Add(time.Second)},
	} {
		// Reuse the input variable to verify the tracker retains its own previous state.
		entry = logproto.Entry{Timestamp: tc.timestamp, Line: tc.line}
		tracker.increment(&entry)
		require.Equal(t, tc.want, entry.Timestamp, "entry %d", i)
		require.Equal(t, tc.line, entry.Line, "entry %d", i)
	}
}

func Test_IncrementTimestamp(t *testing.T) {
	incrementingDisabled := &validation.Limits{}
	flagext.DefaultValues(incrementingDisabled)
	incrementingDisabled.RejectOldSamples = false
	incrementingDisabled.DiscoverLogLevels = false

	incrementingEnabled := &validation.Limits{}
	flagext.DefaultValues(incrementingEnabled)
	incrementingEnabled.RejectOldSamples = false
	incrementingEnabled.IncrementDuplicateTimestamp = true
	incrementingEnabled.DiscoverLogLevels = false

	defaultLimits := &validation.Limits{}
	flagext.DefaultValues(defaultLimits)
	defaultLimits.DiscoverLogLevels = false

	tests := map[string]struct {
		limits       *validation.Limits
		push         *logproto.PushRequest
		expectedPush *logproto.PushRequest
	}{
		"incrementing disabled, no dupes": {
			limits: incrementingDisabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123457, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123457, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
		},
		"incrementing disabled, with dupe timestamp different entry": {
			limits: incrementingDisabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
		},
		"incrementing disabled, with dupe timestamp same entry": {
			limits: incrementingDisabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
						},
					},
				},
			},
		},
		"incrementing enabled, no dupes": {
			limits: incrementingEnabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123457, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123457, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
		},
		"incrementing enabled, with dupe timestamp different entry": {
			limits: incrementingEnabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyiiiiiii"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 1), Line: "heyiiiiiii"},
						},
					},
				},
			},
		},
		"incrementing enabled, with dupe timestamp same entry": {
			limits: incrementingEnabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
						},
					},
				},
			},
		},
		"incrementing enabled, multiple repeated-timestamps": {
			limits: incrementingEnabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "hi"},
							{Timestamp: time.Unix(123456, 0), Line: "hey there"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 1), Line: "hi"},
							{Timestamp: time.Unix(123456, 2), Line: "hey there"},
						},
					},
				},
			},
		},
		"incrementing enabled, multiple subsequent increments": {
			limits: incrementingEnabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 0), Line: "hi"},
							{Timestamp: time.Unix(123456, 1), Line: "hey there"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "heyooooooo"},
							{Timestamp: time.Unix(123456, 1), Line: "hi"},
							{Timestamp: time.Unix(123456, 2), Line: "hey there"},
						},
					},
				},
			},
		},
		"incrementing enabled, no dupes, out of order": {
			limits: incrementingEnabled,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "hey1"},
							{Timestamp: time.Unix(123458, 0), Line: "hey3"},
							{Timestamp: time.Unix(123457, 0), Line: "hey2"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Hash:   0x8eeb87f5eb220480,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "hey1"},
							{Timestamp: time.Unix(123458, 0), Line: "hey3"},
							{Timestamp: time.Unix(123457, 0), Line: "hey2"},
						},
					},
				},
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, testData.limits, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
			_, err := distributors[0].Push(ctx, testData.push)
			assert.NoError(t, err)
			topVal := ing.Peek()
			assert.Equal(t, testData.expectedPush, topVal)
		})
	}
}

// Exercise multiple groups directly; parsers currently produce one group per stream.
func Test_PushInternalRequest(t *testing.T) {
	at := time.Unix(123456, 0)
	group := func(attr string, entries ...logproto.Entry) logproto.ResourceLogs {
		res := logproto.ResourceLogs{ScopeLogs: []logproto.ScopeLogs{{Entries: entries}}}
		if attr != "" {
			res.Attrs = []logproto.LabelAdapter{{Name: "service_name", Value: attr}}
		}
		return res
	}

	for _, tc := range []struct {
		name       string
		limits     func(*validation.Limits)
		groups     []logproto.ResourceLogs
		wantErr    bool
		want       []logproto.Entry
		wantScopes []int
	}{
		{
			name:       "a duplicate timestamp across a group boundary",
			wantScopes: []int{1, 1},
			limits:     func(l *validation.Limits) { l.IncrementDuplicateTimestamp = true },
			groups: []logproto.ResourceLogs{
				group("first", logproto.Entry{Timestamp: at, Line: "hi"}),
				group("second", logproto.Entry{Timestamp: at, Line: "hey"}),
			},
			want: []logproto.Entry{
				{Timestamp: at, Line: "hi", StructuredMetadata: []logproto.LabelAdapter{{Name: "service_name", Value: "first"}}},
				{Timestamp: at.Add(time.Nanosecond), Line: "hey", StructuredMetadata: []logproto.LabelAdapter{{Name: "service_name", Value: "second"}}},
			},
		},
		{
			name:       "a group emptied by validation keeps the rest",
			wantScopes: []int{1},
			limits: func(l *validation.Limits) {
				l.MaxLineSize = 10
				l.MaxLineSizeTruncate = false
			},
			groups: []logproto.ResourceLogs{
				group("first", logproto.Entry{Timestamp: at, Line: "kept"}),
				group("second", logproto.Entry{Timestamp: at, Line: "dropped because this line is far too long"}),
			},
			wantErr: true,
			want:    []logproto.Entry{{Timestamp: at, Line: "kept", StructuredMetadata: []logproto.LabelAdapter{{Name: "service_name", Value: "first"}}}},
		},
		{
			name:       "entry and scope metadata override shared names",
			wantScopes: []int{2},
			groups: []logproto.ResourceLogs{{
				Attrs: []logproto.LabelAdapter{{Name: "resource", Value: "r"}, {Name: "overlap", Value: "resource"}},
				ScopeLogs: []logproto.ScopeLogs{
					{
						Attrs: []logproto.LabelAdapter{{Name: "scope", Value: "first"}, {Name: "overlap", Value: "scope"}},
						Entries: []logproto.Entry{
							{Timestamp: at, Line: "entry wins", StructuredMetadata: []logproto.LabelAdapter{{Name: "overlap", Value: "entry"}}},
							{Timestamp: at.Add(time.Second), Line: "scope wins"},
						},
					},
					{
						Attrs:   []logproto.LabelAdapter{{Name: "scope", Value: "second"}},
						Entries: []logproto.Entry{{Timestamp: at.Add(2 * time.Second), Line: "resource wins"}},
					},
				},
			}},
			want: []logproto.Entry{
				{Timestamp: at, Line: "entry wins", StructuredMetadata: []logproto.LabelAdapter{
					{Name: "overlap", Value: "entry"}, {Name: "scope", Value: "first"}, {Name: "resource", Value: "r"},
				}},
				{Timestamp: at.Add(time.Second), Line: "scope wins", StructuredMetadata: []logproto.LabelAdapter{
					{Name: "scope", Value: "first"}, {Name: "overlap", Value: "scope"}, {Name: "resource", Value: "r"},
				}},
				{Timestamp: at.Add(2 * time.Second), Line: "resource wins", StructuredMetadata: []logproto.LabelAdapter{
					{Name: "scope", Value: "second"}, {Name: "resource", Value: "r"}, {Name: "overlap", Value: "resource"},
				}},
			},
		},
		{
			name: "empty scopes and resources are removed without changing entry order",
			limits: func(l *validation.Limits) {
				l.MaxLineSize = 10
				l.MaxLineSizeTruncate = false
				l.IncrementDuplicateTimestamp = true
			},
			groups: []logproto.ResourceLogs{
				group("dropped-first", logproto.Entry{Timestamp: at, Line: "this line is too long"}),
				{
					Attrs: []logproto.LabelAdapter{{Name: "service_name", Value: "first"}},
					ScopeLogs: []logproto.ScopeLogs{
						{Entries: []logproto.Entry{{Timestamp: at, Line: "this line is too long"}}},
						{Attrs: []logproto.LabelAdapter{{Name: "scope", Value: "one"}}, Entries: []logproto.Entry{{Timestamp: at, Line: "one"}}},
						{Entries: []logproto.Entry{{Timestamp: at, Line: "this line is too long"}}},
						{Attrs: []logproto.LabelAdapter{{Name: "scope", Value: "two"}}, Entries: []logproto.Entry{{Timestamp: at, Line: "two"}}},
						{Entries: []logproto.Entry{{Timestamp: at, Line: "this line is too long"}}},
					},
				},
				group("dropped-middle", logproto.Entry{Timestamp: at, Line: "this line is too long"}),
				group("second", logproto.Entry{Timestamp: at, Line: "three"}),
				group("dropped-last", logproto.Entry{Timestamp: at, Line: "this line is too long"}),
			},
			wantErr:    true,
			wantScopes: []int{2, 1},
			want: []logproto.Entry{
				{Timestamp: at, Line: "one", StructuredMetadata: []logproto.LabelAdapter{{Name: "scope", Value: "one"}, {Name: "service_name", Value: "first"}}},
				{Timestamp: at.Add(time.Nanosecond), Line: "two", StructuredMetadata: []logproto.LabelAdapter{{Name: "scope", Value: "two"}, {Name: "service_name", Value: "first"}}},
				{Timestamp: at.Add(2 * time.Nanosecond), Line: "three", StructuredMetadata: []logproto.LabelAdapter{{Name: "service_name", Value: "second"}}},
			},
		},
		{
			name: "discarded groups are not normalized",
			limits: func(l *validation.Limits) {
				l.MaxLineSize = 10
				l.MaxLineSizeTruncate = false
			},
			groups: []logproto.ResourceLogs{
				{Attrs: buildNestedAttrs("__", "invalid"), ScopeLogs: []logproto.ScopeLogs{{
					Entries: []logproto.Entry{{Timestamp: at, Line: "this line is too long"}},
				}}},
				{Attrs: buildNestedAttrs("service_name", "kept"), ScopeLogs: []logproto.ScopeLogs{
					{Attrs: buildNestedAttrs("__", "invalid"), Entries: []logproto.Entry{{Timestamp: at, Line: "this line is too long"}}},
					{Entries: []logproto.Entry{{Timestamp: at, Line: "kept"}}},
				}},
			},
			wantErr:    true,
			wantScopes: []int{1},
			want:       []logproto.Entry{{Timestamp: at, Line: "kept", StructuredMetadata: buildNestedAttrs("service_name", "kept")}},
		},
		{
			name: "all entries discarded removes the stream",
			limits: func(l *validation.Limits) {
				l.MaxLineSize = 10
				l.MaxLineSizeTruncate = false
			},
			groups: []logproto.ResourceLogs{
				group("first", logproto.Entry{Timestamp: at, Line: "this line is too long"}),
				group("second", logproto.Entry{Timestamp: at, Line: "this line is also too long"}),
			},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			if tc.limits != nil {
				tc.limits(limits)
			}

			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, limits, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
			d := distributors[0]

			req := &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{{
				Labels: `{job="internal"}`, ResourceLogs: tc.groups,
			}}}
			_, err := d.pushWithResolver(ctx, req, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			got := ing.Peek()
			if len(tc.want) == 0 {
				require.Empty(t, req.Streams)
				require.Nil(t, got)
				return
			}
			require.Len(t, req.Streams, 1)
			require.Len(t, req.Streams[0].ResourceLogs, len(tc.wantScopes))
			for i, resource := range req.Streams[0].ResourceLogs {
				require.Len(t, resource.ScopeLogs, tc.wantScopes[i])
				for _, scope := range resource.ScopeLogs {
					require.NotEmpty(t, scope.Entries)
				}
			}

			require.NotNil(t, got, "the stream still had an entry to send")
			require.Len(t, got.Streams, 1)
			require.Equal(t, tc.want, got.Streams[0].Entries)
		})
	}
}

func Test_DiscardAccounting(t *testing.T) {
	now := time.Now()
	firstEntry := logproto.Entry{Timestamp: now, Line: "first", StructuredMetadata: buildNestedAttrs("m", "one", "s", "first", "r", "resource")}
	secondEntry := logproto.Entry{Timestamp: now, Line: "second", StructuredMetadata: buildNestedAttrs("m", "two", "s", "second", "r", "resource")}
	thirdEntry := logproto.Entry{Timestamp: now, Line: "third", StructuredMetadata: buildNestedAttrs("m", "three", "s", "third", "r", "other")}
	type discard struct {
		flatBytes, nestedBytes float64
		samples                float64
	}
	// The fixture has 16 line bytes, 14 entry-metadata bytes, and 34 shared bytes.
	// Expanding the resource shared by two scopes adds another 9 bytes.
	const flatStreamBytes, nestedStreamBytes = 73, 64
	wholeStreamDiscard := discard{flatBytes: flatStreamBytes, nestedBytes: nestedStreamBytes, samples: 3}
	rateLimitRemainingEntries := func(l *validation.Limits) {
		l.MaxLineSize = 20
		l.IngestionRateMB = 1.0 / (1024 * 1024)
		l.IngestionBurstSizeMB = 1.0 / (1024 * 1024)
	}
	for _, tc := range []struct {
		name            string
		limits          func(*validation.Limits)
		labels          string
		policy          string
		noEntryMetadata bool
		timestamp       time.Time
		oversizedLines  []string
		streamLimit     bool
		success         bool
		wantDiscards    map[string]discard
		wantEntries     []logproto.Entry
	}{
		{
			name:         "invalid labels",
			labels:       "{foo=",
			wantDiscards: map[string]discard{validation.InvalidLabels: wholeStreamDiscard},
		},
		{
			name:   "missing labels",
			labels: "{}",
			// Preserve the existing missing-label sample counter alongside invalid-label accounting.
			wantDiscards: map[string]discard{
				validation.MissingLabels: {samples: 1},
				validation.InvalidLabels: wholeStreamDiscard,
			},
		},
		{
			name:   "too many labels",
			labels: `{foo="bar", job="test"}`,
			limits: func(l *validation.Limits) { l.MaxLabelNamesPerSeries = 1 },
			wantDiscards: map[string]discard{
				validation.MaxLabelNamesPerSeries: wholeStreamDiscard,
				validation.InvalidLabels:          wholeStreamDiscard,
			},
		},
		{
			name:   "label name too long",
			limits: func(l *validation.Limits) { l.MaxLabelNameLength = 2 },
			wantDiscards: map[string]discard{
				validation.LabelNameTooLong: wholeStreamDiscard,
				validation.InvalidLabels:    wholeStreamDiscard,
			},
		},
		{
			name:   "label value too long",
			limits: func(l *validation.Limits) { l.MaxLabelValueLength = 2 },
			wantDiscards: map[string]discard{
				validation.LabelValueTooLong: wholeStreamDiscard,
				validation.InvalidLabels:     wholeStreamDiscard,
			},
		},
		{
			name:   "duplicate labels",
			labels: `{foo="bar", foo="baz"}`,
			wantDiscards: map[string]discard{
				validation.DuplicateLabelNames: wholeStreamDiscard,
				validation.InvalidLabels:       wholeStreamDiscard,
			},
		},
		{
			name:   "missing enforced labels",
			limits: func(l *validation.Limits) { l.EnforcedLabels = []string{"app"} },
			wantDiscards: map[string]discard{
				validation.MissingEnforcedLabels: wholeStreamDiscard,
			},
		},
		{
			name:   "blocked ingestion",
			limits: func(l *validation.Limits) { l.BlockIngestionUntil = flagext.Time(now.Add(time.Hour)) },
			wantDiscards: map[string]discard{
				validation.BlockedIngestion: wholeStreamDiscard,
			},
		},
		{
			name: "blocked ingestion with successful response",
			limits: func(l *validation.Limits) {
				l.BlockIngestionUntil = flagext.Time(now.Add(time.Hour))
				l.BlockIngestionStatusCode = http.StatusOK
			},
			success: true,
			wantDiscards: map[string]discard{
				validation.BlockedIngestion: wholeStreamDiscard,
			},
		},
		{
			name:   "blocked policy",
			policy: "accounting",
			limits: func(l *validation.Limits) {
				l.BlockIngestionPolicyUntil = map[string]flagext.Time{"accounting": flagext.Time(now.Add(time.Hour))}
			},
			wantDiscards: map[string]discard{
				validation.BlockedIngestionPolicy: wholeStreamDiscard,
			},
		},
		{
			name: "blocked global policy",
			limits: func(l *validation.Limits) {
				l.BlockIngestionPolicyUntil = map[string]flagext.Time{validation.GlobalPolicy: flagext.Time(now.Add(time.Hour))}
			},
			wantDiscards: map[string]discard{
				validation.BlockedIngestionPolicy: wholeStreamDiscard,
			},
		},
		{
			name:      "too old",
			timestamp: now.Add(-2 * time.Hour),
			limits: func(l *validation.Limits) {
				l.RejectOldSamples = true
				l.RejectOldSamplesMaxAge = model.Duration(time.Hour)
			},
			wantDiscards: map[string]discard{
				validation.GreaterThanMaxSampleAge: wholeStreamDiscard,
			},
		},
		{
			name:      "too far in future",
			timestamp: now.Add(2 * time.Hour),
			wantDiscards: map[string]discard{
				validation.TooFarInFuture: wholeStreamDiscard,
			},
		},
		{
			name:   "line too long",
			limits: func(l *validation.Limits) { l.MaxLineSize = 4 },
			wantDiscards: map[string]discard{
				validation.LineTooLong: wholeStreamDiscard,
			},
		},
		{
			name:           "one oversized entry empties the first scope, two entries ingested",
			limits:         func(l *validation.Limits) { l.MaxLineSize = 20 },
			oversizedLines: []string{"first"},
			wantDiscards: map[string]discard{
				validation.LineTooLong: {flatBytes: 59, nestedBytes: 50, samples: 1},
			},
			wantEntries: []logproto.Entry{secondEntry, thirdEntry},
		},
		{
			name:           "one oversized entry empties the middle scope, two entries ingested",
			limits:         func(l *validation.Limits) { l.MaxLineSize = 20 },
			oversizedLines: []string{"second"},
			wantDiscards: map[string]discard{
				validation.LineTooLong: {flatBytes: 60, nestedBytes: 51, samples: 1},
			},
			wantEntries: []logproto.Entry{firstEntry, thirdEntry},
		},
		{
			name:           "two oversized entries empty a resource, one entry ingested",
			limits:         func(l *validation.Limits) { l.MaxLineSize = 20 },
			oversizedLines: []string{"first", "second"},
			wantDiscards: map[string]discard{
				validation.LineTooLong: {flatBytes: 119, nestedBytes: 110, samples: 2},
			},
			wantEntries: []logproto.Entry{thirdEntry},
		},
		{
			name:   "disallowed structured metadata",
			limits: func(l *validation.Limits) { l.AllowStructuredMetadata = false },
			wantDiscards: map[string]discard{
				validation.DisallowedStructuredMetadata: wholeStreamDiscard,
			},
		},
		{
			name:            "disallowed shared metadata only",
			limits:          func(l *validation.Limits) { l.AllowStructuredMetadata = false },
			noEntryMetadata: true,
			wantDiscards: map[string]discard{
				validation.DisallowedStructuredMetadata: {flatBytes: 59, nestedBytes: 50, samples: 3},
			},
		},
		{
			name:   "structured metadata too large",
			limits: func(l *validation.Limits) { l.MaxStructuredMetadataSize = 14 },
			wantDiscards: map[string]discard{
				validation.StructuredMetadataTooLarge: wholeStreamDiscard,
			},
		},
		{
			name:   "only the second entry exceeds the metadata size limit, two entries ingested",
			limits: func(l *validation.Limits) { l.MaxStructuredMetadataSize = 19 },
			// The entries have 19, 20, and 18 metadata bytes including shared attributes.
			wantDiscards: map[string]discard{
				validation.StructuredMetadataTooLarge: {flatBytes: 26, nestedBytes: 17, samples: 1},
			},
			wantEntries: []logproto.Entry{firstEntry, thirdEntry},
		},
		{
			name:   "too many structured metadata entries",
			limits: func(l *validation.Limits) { l.MaxStructuredMetadataEntriesCount = 2 },
			wantDiscards: map[string]discard{
				validation.StructuredMetadataTooMany: wholeStreamDiscard,
			},
		},
		{
			name:   "all three entries rate limited",
			limits: rateLimitRemainingEntries,
			wantDiscards: map[string]discard{
				validation.RateLimited: wholeStreamDiscard,
			},
		},
		// Validation discards oversized lines first; the one-byte burst rejects every remaining entry.
		{
			name:           "one oversized entry empties a scope, two entries rate limited",
			limits:         rateLimitRemainingEntries,
			oversizedLines: []string{"first"},
			wantDiscards: map[string]discard{
				validation.LineTooLong: {flatBytes: 59, nestedBytes: 50, samples: 1},
				validation.RateLimited: {flatBytes: 49, nestedBytes: 49, samples: 2},
			},
		},
		{
			name:           "two oversized entries empty a resource, one entry rate limited",
			limits:         rateLimitRemainingEntries,
			oversizedLines: []string{"first", "second"},
			wantDiscards: map[string]discard{
				validation.LineTooLong: {flatBytes: 119, nestedBytes: 110, samples: 2},
				validation.RateLimited: {flatBytes: 23, nestedBytes: 23, samples: 1},
			},
		},
		{
			name:         "stream limit",
			streamLimit:  true,
			wantDiscards: map[string]discard{validation.StreamLimit: wholeStreamDiscard},
		},
	} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nested=%t", tc.name, nested), func(t *testing.T) {
				validation.DiscardedBytes.Reset()
				validation.DiscardedSamples.Reset()
				defer validation.DiscardedBytes.Reset()
				defer validation.DiscardedSamples.Reset()

				ls := tc.labels
				if ls == "" {
					ls = `{foo="bar"}`
				}
				at := tc.timestamp
				if at.IsZero() {
					at = now
				}
				// Each scope holds one entry: "first" and "second" share a resource; "third" has its own.
				stream := logproto.InternalStreamAdapter{Labels: ls, ResourceLogs: []logproto.ResourceLogs{
					{Attrs: []logproto.LabelAdapter{{Name: "r", Value: "resource"}}, ScopeLogs: []logproto.ScopeLogs{
						{Attrs: []logproto.LabelAdapter{{Name: "s", Value: "first"}}, Entries: []logproto.Entry{{Timestamp: at, Line: "first", StructuredMetadata: []logproto.LabelAdapter{{Name: "m", Value: "one"}}}}},
						{Attrs: []logproto.LabelAdapter{{Name: "s", Value: "second"}}, Entries: []logproto.Entry{{Timestamp: at, Line: "second", StructuredMetadata: []logproto.LabelAdapter{{Name: "m", Value: "two"}}}}},
					}},
					{Attrs: []logproto.LabelAdapter{{Name: "r", Value: "other"}}, ScopeLogs: []logproto.ScopeLogs{
						{Attrs: []logproto.LabelAdapter{{Name: "s", Value: "third"}}, Entries: []logproto.Entry{{Timestamp: at, Line: "third", StructuredMetadata: []logproto.LabelAdapter{{Name: "m", Value: "three"}}}}},
					}},
				}}
				stream.EachEntryWithShared(func(entry *logproto.Entry, _, _ []logproto.LabelAdapter) {
					if tc.noEntryMetadata {
						entry.StructuredMetadata = nil
					}
					if slices.Contains(tc.oversizedLines, entry.Line) {
						entry.Line = strings.Repeat("x", 40)
					}
				})

				lim := &validation.Limits{}
				flagext.DefaultValues(lim)
				lim.RejectOldSamples = false
				lim.DiscoverLogLevels = false
				lim.MaxLineSizeTruncate = false
				if tc.policy != "" {
					lim.PolicyStreamMapping = validation.PolicyStreamMapping{tc.policy: {{Selector: ls, Priority: 1}}}
				}
				if tc.limits != nil {
					tc.limits(lim)
				}
				distributors, ingesters := prepare(t, 1, 3, lim, nil)
				d := distributors[0]
				if tc.streamLimit {
					streamBytes := uint64(flatStreamBytes)
					if nested {
						streamBytes = nestedStreamBytes
					}
					parsed, err := syntax.ParseLabels(ls)
					require.NoError(t, err)
					hash := labels.StableHash(parsed)
					d.cfg.IngestLimitsEnabled = true
					d.ingestLimits = newIngestLimits(&mockIngestLimitsFrontendClient{
						t:                            t,
						expectedExceedsLimitsRequest: &limitsproto.ExceedsLimitsRequest{Tenant: "test", Streams: []*limitsproto.StreamMetadata{{StreamHash: hash, TotalSize: streamBytes}}},
						exceedsLimitsResponse:        &limitsproto.ExceedsLimitsResponse{Results: []*limitsproto.ExceedsLimitsResult{{StreamHash: hash, Reason: uint32(limits.ReasonMaxStreams)}}},
					}, prometheus.NewRegistry())
				}
				var err error
				if nested {
					_, err = d.pushWithResolver(ctx, &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{stream}}, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
				} else {
					var flat logproto.Stream
					stream.ToStream(&flat)
					_, err = d.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{flat}})
				}
				if tc.success {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				for i := range ingesters {
					if len(tc.wantEntries) == 0 {
						require.Nil(t, ingesters[i].Peek())
						continue
					}
					// Push returns after quorum; wait for the remaining replica too.
					require.Eventually(t, func() bool { return ingesters[i].Peek() != nil }, time.Second, 10*time.Millisecond)
					got := ingesters[i].Peek()
					require.Len(t, got.Streams, 1)
					require.Equal(t, ls, got.Streams[0].Labels)
					require.Len(t, got.Streams[0].Entries, len(tc.wantEntries))
					for j, want := range tc.wantEntries {
						entry := got.Streams[0].Entries[j]
						require.Equal(t, want.Timestamp, entry.Timestamp, "entry %d timestamp", j)
						require.Equal(t, want.Line, entry.Line, "entry %d line", j)
						require.ElementsMatch(t, want.StructuredMetadata, entry.StructuredMetadata, "entry %d metadata", j)
					}
				}
				for reason, counts := range tc.wantDiscards {
					wantBytes := counts.flatBytes
					if nested {
						wantBytes = counts.nestedBytes
					}
					require.Equal(t, wantBytes, testutil.ToFloat64(validation.DiscardedBytes.WithLabelValues(reason, "test", "0", tc.policy, constants.Loki)), "%s bytes", reason)
					require.Equal(t, counts.samples, testutil.ToFloat64(validation.DiscardedSamples.WithLabelValues(reason, "test", "0", tc.policy, constants.Loki)), "%s samples", reason)
				}
			})
		}
	}
}

func TestDistributor_PushRateLimitsOnlyValidatedStreams(t *testing.T) {
	const validLine, rejectedLine = "valid", "rejected"
	for _, tc := range []struct {
		name           string
		rejectedLabels string
		reason         string
		policy         string
		limits         func(*validation.Limits)
	}{
		{
			name:           "malformed labels",
			rejectedLabels: `{app=`,
			reason:         validation.InvalidLabels,
		},
		{
			name:           "too many labels",
			rejectedLabels: `{app="rejected", extra="label"}`,
			reason:         validation.InvalidLabels,
			limits:         func(l *validation.Limits) { l.MaxLabelNamesPerSeries = 1 },
		},
		{
			name:           "missing enforced labels",
			rejectedLabels: `{other="rejected"}`,
			reason:         validation.MissingEnforcedLabels,
			limits:         func(l *validation.Limits) { l.EnforcedLabels = []string{"app"} },
		},
		{
			name:           "blocked policy",
			rejectedLabels: `{app="blocked"}`,
			reason:         validation.BlockedIngestionPolicy,
			policy:         "blocked",
			limits: func(l *validation.Limits) {
				l.PolicyStreamMapping = validation.PolicyStreamMapping{
					"blocked": {{Selector: `{app="blocked"}`, Priority: 1}},
				}
				l.BlockIngestionPolicyUntil = map[string]flagext.Time{"blocked": flagext.Time(time.Now().Add(time.Hour))}
				l.BlockIngestionStatusCode = http.StatusBadRequest
			},
		},
	} {
		for _, rateLimited := range []bool{false, true} {
			for _, rejectedFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/rateLimited=%t/rejectedFirst=%t", tc.name, rateLimited, rejectedFirst), func(t *testing.T) {
					validation.DiscardedBytes.Reset()
					validation.DiscardedSamples.Reset()
					defer validation.DiscardedBytes.Reset()
					defer validation.DiscardedSamples.Reset()

					lim := &validation.Limits{}
					flagext.DefaultValues(lim)
					lim.DiscoverLogLevels = false
					lim.IngestionRateStrategy = validation.LocalIngestionRateStrategy
					lim.IngestionRateMB = datasize.ByteSize(1).MBytes()
					burst := len(validLine)
					if rateLimited {
						burst--
					}
					lim.IngestionBurstSizeMB = datasize.ByteSize(burst).MBytes()
					if tc.limits != nil {
						tc.limits(lim)
					}
					require.NoError(t, lim.Validate())
					distributors, ingesters := prepare(t, 1, 3, lim, nil)
					now := time.Now()
					valid := logproto.Stream{Labels: `{app="valid"}`, Entries: []logproto.Entry{{Timestamp: now, Line: validLine}}}
					rejected := logproto.Stream{Labels: tc.rejectedLabels, Entries: []logproto.Entry{{Timestamp: now, Line: rejectedLine}}}
					streams := []logproto.Stream{valid, rejected}
					if rejectedFirst {
						slices.Reverse(streams)
					}

					_, err := distributors[0].Push(ctx, &logproto.PushRequest{Streams: streams})
					require.Error(t, err)
					response, ok := httpgrpc.HTTPResponseFromError(err)
					require.True(t, ok)
					wantStatus := http.StatusBadRequest
					var rateLimitedBytes, rateLimitedSamples float64
					if rateLimited {
						wantStatus = http.StatusTooManyRequests
						rateLimitedBytes, rateLimitedSamples = float64(len(validLine)), 1
					}
					require.Equal(t, int32(wantStatus), response.Code)
					require.Equal(t, float64(len(rejectedLine)), testutil.ToFloat64(validation.DiscardedBytes.WithLabelValues(tc.reason, "test", "0", tc.policy, constants.Loki)))
					require.Equal(t, float64(1), testutil.ToFloat64(validation.DiscardedSamples.WithLabelValues(tc.reason, "test", "0", tc.policy, constants.Loki)))
					require.Equal(t, rateLimitedBytes, testutil.ToFloat64(validation.DiscardedBytes.WithLabelValues(validation.RateLimited, "test", "0", "", constants.Loki)))
					require.Equal(t, rateLimitedSamples, testutil.ToFloat64(validation.DiscardedSamples.WithLabelValues(validation.RateLimited, "test", "0", "", constants.Loki)))
					if tc.policy != "" {
						require.Zero(t, testutil.ToFloat64(validation.DiscardedBytes.WithLabelValues(validation.RateLimited, "test", "0", tc.policy, constants.Loki)))
						require.Zero(t, testutil.ToFloat64(validation.DiscardedSamples.WithLabelValues(validation.RateLimited, "test", "0", tc.policy, constants.Loki)))
					}
					for i := range ingesters {
						ingester := &ingesters[i]
						if rateLimited {
							require.Nil(t, ingester.Peek())
							continue
						}
						require.Eventually(t, func() bool { return ingester.Peek() != nil }, time.Second, 10*time.Millisecond)
						got := ingester.Peek().Streams
						require.Len(t, got, 1)
						require.Equal(t, valid.Labels, got[0].Labels)
						require.Equal(t, valid.Entries, got[0].Entries)
					}
				})
			}
		}
	}
}

func TestDistributor_ProcessStreamEntries(t *testing.T) {
	now := time.Now()
	entry := func(at time.Time, line string) logproto.Entry {
		return logproto.Entry{Timestamp: at, Line: line, StructuredMetadata: buildNestedAttrs("m", "v")}
	}
	scope := func(entries ...logproto.Entry) logproto.ScopeLogs {
		return logproto.ScopeLogs{Attrs: buildNestedAttrs("s", "scope"), Entries: entries}
	}
	resource := func(scopes ...logproto.ScopeLogs) logproto.ResourceLogs {
		return logproto.ResourceLogs{Attrs: buildNestedAttrs("r", "resource"), ScopeLogs: scopes}
	}
	// Entry sizes include their own metadata: kept=4, tooLong=8, tooOld=5, future=5.
	// Each scope adds 6 shared bytes; each resource adds 9.
	kept := entry(now, "ok")
	tooLong := entry(now, "123456")
	tooOld := entry(now.Add(-2*time.Hour), "old")
	future := entry(now.Add(2*time.Hour), "new")
	type discard struct {
		bytes, samples float64
	}
	for _, tc := range []struct {
		name          string
		resources     []logproto.ResourceLogs
		wantDiscards  map[string]discard
		wantKept      []string
		wantKeptBytes int
	}{
		{
			name:          "all entries kept",
			resources:     []logproto.ResourceLogs{resource(scope(kept, kept))},
			wantKept:      []string{"ok", "ok"},
			wantKeptBytes: 23,
		},
		{
			name:          "rejection before a kept entry retains all shared metadata",
			resources:     []logproto.ResourceLogs{resource(scope(tooLong, kept))},
			wantDiscards:  map[string]discard{validation.LineTooLong: {bytes: 8, samples: 1}},
			wantKept:      []string{"ok"},
			wantKeptBytes: 19,
		},
		{
			name:          "rejection after a kept entry retains all shared metadata",
			resources:     []logproto.ResourceLogs{resource(scope(kept, tooLong))},
			wantDiscards:  map[string]discard{validation.LineTooLong: {bytes: 8, samples: 1}},
			wantKept:      []string{"ok"},
			wantKeptBytes: 19,
		},
		{
			name:      "mixed rejections with a surviving entry charge only their own bytes",
			resources: []logproto.ResourceLogs{resource(scope(tooLong, tooOld, future, kept))},
			wantDiscards: map[string]discard{
				validation.LineTooLong:             {bytes: 8, samples: 1},
				validation.GreaterThanMaxSampleAge: {bytes: 5, samples: 1},
				validation.TooFarInFuture:          {bytes: 5, samples: 1},
			},
			wantKept:      []string{"ok"},
			wantKeptBytes: 19,
		},
		{
			name:      "last rejection in an emptied scope gets its shared bytes",
			resources: []logproto.ResourceLogs{resource(scope(tooLong, tooOld), scope(kept))},
			wantDiscards: map[string]discard{
				validation.LineTooLong:             {bytes: 8, samples: 1},
				validation.GreaterThanMaxSampleAge: {bytes: 11, samples: 1},
			},
			wantKept:      []string{"ok"},
			wantKeptBytes: 19,
		},
		{
			name:      "last rejection in an emptied resource gets all remaining shared bytes",
			resources: []logproto.ResourceLogs{resource(scope(tooLong, tooOld))},
			wantDiscards: map[string]discard{
				validation.LineTooLong:             {bytes: 8, samples: 1},
				validation.GreaterThanMaxSampleAge: {bytes: 20, samples: 1},
			},
		},
		{
			name:      "different reasons empty separate scopes and share resource bytes once",
			resources: []logproto.ResourceLogs{resource(scope(tooOld), scope(), scope(tooLong), scope())},
			wantDiscards: map[string]discard{
				validation.GreaterThanMaxSampleAge: {bytes: 11, samples: 1},
				validation.LineTooLong:             {bytes: 23, samples: 1},
			},
		},
		{
			name:          "surviving resource followed by a discarded resource",
			resources:     []logproto.ResourceLogs{resource(scope(kept)), resource(scope(tooLong))},
			wantDiscards:  map[string]discard{validation.LineTooLong: {bytes: 23, samples: 1}},
			wantKept:      []string{"ok"},
			wantKeptBytes: 19,
		},
		{
			name:         "empty resources do not inherit an earlier rejection reason",
			resources:    []logproto.ResourceLogs{resource(scope(tooLong)), resource(scope()), resource()},
			wantDiscards: map[string]discard{validation.LineTooLong: {bytes: 23, samples: 1}},
		},
		{
			name:          "groups that arrived empty do not count as discarded",
			resources:     []logproto.ResourceLogs{resource(scope()), resource(), resource(scope(kept))},
			wantKept:      []string{"ok"},
			wantKeptBytes: 19,
		},
		{
			name: "discard accounting stays raw after shared metadata normalization",
			resources: []logproto.ResourceLogs{{
				Attrs: buildNestedAttrs("1", "r"),
				ScopeLogs: []logproto.ScopeLogs{
					{Attrs: buildNestedAttrs("2", "s"), Entries: []logproto.Entry{kept}},
					{Attrs: buildNestedAttrs("3", "s"), Entries: []logproto.Entry{tooLong, tooOld}},
				},
			}},
			wantDiscards: map[string]discard{
				validation.LineTooLong:             {bytes: 8, samples: 1},
				validation.GreaterThanMaxSampleAge: {bytes: 7, samples: 1},
			},
			wantKept:      []string{"ok"},
			wantKeptBytes: 16,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validation.DiscardedBytes.Reset()
			validation.DiscardedSamples.Reset()
			t.Cleanup(validation.DiscardedBytes.Reset)
			t.Cleanup(validation.DiscardedSamples.Reset)

			// setup limits and other fixtures
			lim := &validation.Limits{}
			flagext.DefaultValues(lim)
			lim.RejectOldSamples = true
			lim.RejectOldSamplesMaxAge = model.Duration(time.Hour)
			lim.CreationGracePeriod = model.Duration(time.Hour)
			lim.MaxLineSize = 5
			lim.DiscoverLogLevels = false
			overrides, err := validation.NewOverrides(*lim, nil)
			require.NoError(t, err)
			tracker := &discardUsageTracker{bytesByReason: map[string]float64{}}
			v, err := NewValidator(overrides, tracker)
			require.NoError(t, err)
			d := &Distributor{validator: v, m: newMetrics(prometheus.NewRegistry())}
			vCtx := v.getValidationContextForTime(now, "test")
			lbs := labels.FromStrings("app", "shared")
			stream := logproto.InternalStreamAdapter{Labels: lbs.String(), ResourceLogs: tc.resources}
			var validationErrors util.GroupedErrors

			stats, err := d.processStreamEntries(ctx, vCtx, &stream, lbs, "24", "accounting", constants.OTLP, newFieldDetector(vCtx), &validationErrors)
			require.NoError(t, err)
			require.Equal(t, tc.wantKeptBytes, stats.unexpandedSize)
			require.Equal(t, tc.wantKeptBytes, nestedStreamSize(stream))
			require.Equal(t, len(tc.wantKept), stats.entriesKept)
			var keptLines []string
			for _, resource := range stream.ResourceLogs {
				require.NotEmpty(t, resource.ScopeLogs)
				for _, scope := range resource.ScopeLogs {
					require.NotEmpty(t, scope.Entries)
					for _, entry := range scope.Entries {
						keptLines = append(keptLines, entry.Line)
					}
				}
			}
			require.Equal(t, tc.wantKept, keptLines)

			wantTrackedBytes := map[string]float64{}
			var totalSamples float64
			for reason, counts := range tc.wantDiscards {
				require.Equal(t, counts.bytes, testutil.ToFloat64(validation.DiscardedBytes.WithLabelValues(reason, "test", "24", "accounting", constants.OTLP)), "%s bytes", reason)
				require.Equal(t, counts.samples, testutil.ToFloat64(validation.DiscardedSamples.WithLabelValues(reason, "test", "24", "accounting", constants.OTLP)), "%s samples", reason)
				wantTrackedBytes[reason] = counts.bytes
				totalSamples += counts.samples
			}
			require.Equal(t, wantTrackedBytes, tracker.bytesByReason)
			require.Equal(t, len(tc.wantDiscards), testutil.CollectAndCount(validation.DiscardedBytes))
			require.Equal(t, len(tc.wantDiscards), testutil.CollectAndCount(validation.DiscardedSamples))
			require.Len(t, validationErrors.MultiError, int(totalSamples))
		})
	}
}

type discardUsageTracker struct {
	bytesByReason map[string]float64
}

func (*discardUsageTracker) ReceivedBytesAdd(context.Context, string, time.Duration, labels.Labels, float64, string) {
}

func (t *discardUsageTracker) DiscardedBytesAdd(_ context.Context, _, reason string, _ labels.Labels, value float64, _ string) {
	t.bytesByReason[reason] += value
}

func Test_MissingEnforcedLabels(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	limits.EnforcedLabels = []string{"app"}
	limits.PolicyEnforcedLabels = map[string][]string{
		"policy1":               {"cluster", "namespace"},
		"policy2":               {"namespace"},
		validation.GlobalPolicy: {"env"},
	}

	distributors, _ := prepare(t, 1, 5, limits, nil)

	// request with all required labels.
	lbs := labels.FromMap(map[string]string{"app": "foo", "env": "prod", "cluster": "cluster1", "namespace": "ns1"})
	missing, missingLabels := distributors[0].missingEnforcedLabels(lbs, "test", "policy1")

	assert.False(t, missing)
	assert.Empty(t, missingLabels)

	// request missing the `app` label from per-tenant enforced labels and `cluster` label from policy enforced labels.
	lbs = labels.FromMap(map[string]string{"env": "prod", "namespace": "ns1"})
	missing, missingLabels = distributors[0].missingEnforcedLabels(lbs, "test", "policy1")
	assert.True(t, missing)
	assert.EqualValues(t, []string{"app", "cluster"}, missingLabels)

	// request missing the `env` label from global policy enforced labels and `cluster` label from policy1 enforced labels.
	lbs = labels.FromMap(map[string]string{"app": "foo", "namespace": "ns1"})
	missing, missingLabels = distributors[0].missingEnforcedLabels(lbs, "test", "policy1")
	assert.True(t, missing)
	assert.EqualValues(t, []string{"env", "cluster"}, missingLabels)

	// request missing all required labels.
	lbs = labels.FromMap(map[string]string{"pod": "distributor-abc"})
	missing, missingLabels = distributors[0].missingEnforcedLabels(lbs, "test", "policy2")
	assert.True(t, missing)
	assert.EqualValues(t, []string{"app", "env", "namespace"}, missingLabels)
}

func Test_PushWithEnforcedLabels(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	// makeWriteRequest only contains a `{foo="bar"}` label.
	req := makeWriteRequest(100, 100) // 100 lines of 100 bytes each
	limits.EnforcedLabels = []string{"app", "env"}
	distributors, _ := prepare(t, 1, 3, limits, nil)

	// reset metrics in case they were set from a previous test.
	validation.DiscardedBytes.Reset()
	validation.DiscardedSamples.Reset()

	// enforced labels configured, but all labels are missing.
	_, err := distributors[0].Push(ctx, req)
	require.Error(t, err)
	expectedErr := httpgrpc.Errorf(http.StatusBadRequest, validation.MissingEnforcedLabelsErrorMsg, "app,env", "test", "{foo=\"bar\"}", "")
	require.EqualError(t, err, expectedErr.Error())

	// Verify metrics for discarded samples due to missing enforced labels
	assert.Equal(t, float64(10000), testutil.ToFloat64(validation.DiscardedBytes)) // 100 lines * 100 bytes
	assert.Equal(t, float64(100), testutil.ToFloat64(validation.DiscardedSamples)) // 100 lines

	// enforced labels, but all labels are present.
	req = makeWriteRequestWithLabels(100, 100, []string{`{app="foo", env="prod"}`}, false, false, false)
	_, err = distributors[0].Push(ctx, req)
	require.NoError(t, err)

	// Metrics should not have increased since this push was successful
	assert.Equal(t, float64(10000), testutil.ToFloat64(validation.DiscardedBytes))
	assert.Equal(t, float64(100), testutil.ToFloat64(validation.DiscardedSamples))

	// Make a new request, since Push may have modified req.
	req = makeWriteRequestWithLabels(100, 100, []string{`{app="foo", env="prod"}`}, false, false, false)
	// no enforced labels, so no errors.
	limits.EnforcedLabels = []string{}
	distributors, _ = prepare(t, 1, 3, limits, nil)
	_, err = distributors[0].Push(ctx, req)
	require.NoError(t, err)

	// Metrics should remain unchanged
	assert.Equal(t, float64(10000), testutil.ToFloat64(validation.DiscardedBytes))
	assert.Equal(t, float64(100), testutil.ToFloat64(validation.DiscardedSamples))

	// enforced labels are configured but the stream is an aggregated metric, so no errors.
	limits.EnforcedLabels = []string{"app", "env"}
	distributors, _ = prepare(t, 1, 3, limits, nil)

	req = makeWriteRequestWithLabels(100, 100, []string{`{__aggregated_metric__="foo"}`}, false, false, false)
	_, err = distributors[0].Push(ctx, req)
	require.NoError(t, err)

	// Metrics should remain unchanged
	assert.Equal(t, float64(10000), testutil.ToFloat64(validation.DiscardedBytes))
	assert.Equal(t, float64(100), testutil.ToFloat64(validation.DiscardedSamples))
}

func TestDistributorPushConcurrently(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	distributors, ingesters := prepare(t, 1, 5, limits, nil)

	numReq := 1
	var wg sync.WaitGroup
	for i := 0; i < numReq; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			request := makeWriteRequestWithLabels(100, 100,
				[]string{
					fmt.Sprintf(`{app="foo-%d"}`, n),
					fmt.Sprintf(`{instance="bar-%d"}`, n),
				}, false, false, false,
			)
			response, err := distributors[n%len(distributors)].Push(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, &logproto.PushResponse{}, response)
		}(i)
	}

	wg.Wait()
	// make sure the ingesters received the push requests
	time.Sleep(10 * time.Millisecond)

	counter := 0
	labels := make(map[string]int)

	for i := range ingesters {
		ingesters[i].mu.Lock()

		pushed := ingesters[i].pushed
		counter = counter + len(pushed)
		for _, pr := range pushed {
			for _, st := range pr.Streams {
				labels[st.Labels] = labels[st.Labels] + 1
			}
		}
		ingesters[i].mu.Unlock()
	}
	assert.Equal(t, numReq*3, counter) // RF=3
	// each stream is present 3 times
	for i := 0; i < numReq; i++ {
		l := fmt.Sprintf(`{instance="bar-%d"}`, i)
		assert.Equal(t, 3, labels[l], "stream %s expected 3 times, got %d", l, labels[l])
		l = fmt.Sprintf(`{app="foo-%d"}`, i)
		assert.Equal(t, 3, labels[l], "stream %s expected 3 times, got %d", l, labels[l])
	}
}

func TestDistributorPushErrors(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	t.Run("with RF=3 a single push can fail", func(t *testing.T) {
		distributors, ingesters := prepare(t, 1, 3, limits, nil)
		ingesters[0].failAfter = 5 * time.Millisecond
		ingesters[1].succeedAfter = 10 * time.Millisecond
		ingesters[2].succeedAfter = 15 * time.Millisecond

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			return len(ingesters[1].pushed) == 1 && len(ingesters[2].pushed) == 1
		}, time.Second, 10*time.Millisecond)

		require.Equal(t, 0, len(ingesters[0].pushed))
	})
	t.Run("with RF=3 two push failures result in error", func(t *testing.T) {
		distributors, ingesters := prepare(t, 1, 3, limits, nil)
		ingesters[0].failAfter = 5 * time.Millisecond
		ingesters[1].succeedAfter = 10 * time.Millisecond
		ingesters[2].failAfter = 15 * time.Millisecond

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.Error(t, err)

		require.Eventually(t, func() bool {
			return len(ingesters[1].pushed) == 1
		}, time.Second, 10*time.Millisecond)

		require.Equal(t, 0, len(ingesters[0].pushed))
		require.Equal(t, 0, len(ingesters[2].pushed))
	})
}

func TestDistributorPushToKafka(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	t.Run("with kafka, any failure fails the request", func(t *testing.T) {
		kafkaWriter := &mockKafkaProducer{
			failOnWrite: true,
		}
		distributors, _ := prepareButDontStart(t, 1, 0, limits, nil)
		for _, d := range distributors {
			d.cfg.KafkaEnabled = true
			d.cfg.IngesterEnabled = false
			d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
			d.kafkaWriter = kafkaWriter
		}
		startAndWaitRunningDistributors(t, distributors)

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.Error(t, err)
	})

	t.Run("with kafka, no failures is successful", func(t *testing.T) {
		kafkaWriter := &mockKafkaProducer{
			failOnWrite: false,
		}
		distributors, _ := prepareButDontStart(t, 1, 0, limits, nil)
		for _, d := range distributors {
			d.cfg.KafkaEnabled = true
			d.cfg.IngesterEnabled = false
			d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
			d.kafkaWriter = kafkaWriter
		}
		startAndWaitRunningDistributors(t, distributors)

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)

		require.Equal(t, uint64(1), kafkaWriter.pushes)
	})

	t.Run("shared metadata survives Kafka encoding", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			maxSize     int
			wantRecords int
			wantErr     string
		}{
			{name: "one record", maxSize: 1024, wantRecords: 1},
			{name: "split records", maxSize: 256, wantRecords: 2},
			{name: "entry exceeds record limit", maxSize: 100, wantErr: "single entry size"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				lim := &validation.Limits{}
				flagext.DefaultValues(lim)
				lim.RejectOldSamples = false
				lim.DiscoverLogLevels = false
				distributors, _ := prepareButDontStart(t, 1, 0, lim, nil)
				d := distributors[0]
				producer := &mockKafkaProducer{}
				d.cfg.KafkaEnabled = true
				d.cfg.IngesterEnabled = false
				d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = tc.maxSize
				d.kafkaWriter = producer
				startAndWaitRunningDistributors(t, distributors)

				at := time.Unix(123456, 0).UTC()
				first := logproto.Entry{Timestamp: at, Line: strings.Repeat("a", 100), StructuredMetadata: buildNestedAttrs("service_name", "entry")}
				second := logproto.Entry{Timestamp: at.Add(time.Second), Line: strings.Repeat("b", 100)}
				req := &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{{
					Labels: `{app="shared"}`,
					ResourceLogs: []logproto.ResourceLogs{{Attrs: buildNestedAttrs("service.name", "resource"), ScopeLogs: []logproto.ScopeLogs{
						{Attrs: buildNestedAttrs("scope.name", "first"), Entries: []logproto.Entry{first}},
						{Attrs: buildNestedAttrs("scope.name", "second"), Entries: []logproto.Entry{second}},
					}}},
				}}}
				_, err := d.pushWithResolver(ctx, req, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
				if tc.wantErr != "" {
					require.ErrorContains(t, err, tc.wantErr)
					require.Empty(t, producer.records)
					return
				}
				require.NoError(t, err)
				require.Len(t, producer.records, tc.wantRecords)

				want := []logproto.Entry{
					{Timestamp: first.Timestamp, Line: first.Line, StructuredMetadata: buildNestedAttrs("service_name", "entry", "scope_name", "first")},
					{Timestamp: second.Timestamp, Line: second.Line, StructuredMetadata: buildNestedAttrs("scope_name", "second", "service_name", "resource")},
				}
				decoder, err := kafka.NewDecoder()
				require.NoError(t, err)
				decoded := 0
				for _, record := range producer.records {
					require.Equal(t, "test", string(record.Key))
					require.LessOrEqual(t, len(record.Value), tc.maxSize)
					stream, _, err := decoder.Decode(record.Value)
					require.NoError(t, err)
					require.Equal(t, `{app="shared"}`, stream.Labels)
					require.NotEmpty(t, stream.Entries)
					for _, entry := range stream.Entries {
						require.Less(t, decoded, len(want))
						require.Equal(t, want[decoded], entry)
						decoded++
					}
				}
				require.Equal(t, len(want), decoded)
			})
		}
	})

	t.Run("with kafka and ingesters, both must complete", func(t *testing.T) {
		kafkaWriter := &mockKafkaProducer{
			failOnWrite: false,
		}
		distributors, ingesters := prepareButDontStart(t, 1, 3, limits, nil)
		ingesters[0].succeedAfter = 5 * time.Millisecond
		ingesters[1].succeedAfter = 10 * time.Millisecond
		ingesters[2].succeedAfter = 15 * time.Millisecond

		for _, d := range distributors {
			d.cfg.KafkaEnabled = true
			d.cfg.IngesterEnabled = true
			d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
			d.kafkaWriter = kafkaWriter
		}
		startAndWaitRunningDistributors(t, distributors)

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)

		require.Equal(t, uint64(1), kafkaWriter.pushes)

		require.Equal(t, 1, len(ingesters[0].pushed))
		require.Equal(t, 1, len(ingesters[1].pushed))
		require.Eventually(t, func() bool {
			ingesters[2].mu.Lock()
			defer ingesters[2].mu.Unlock()
			return len(ingesters[2].pushed) == 1
		}, time.Second, 10*time.Millisecond)
	})

	t.Run("with kafka, does shuffle sharding", func(t *testing.T) {
		tests := map[string]struct {
			numIngesters                int
			shardSize                   int
			expectedPartitionsShardedTo int
		}{
			"shardSize=0 -> shards to all partitions": {
				numIngesters:                3,
				shardSize:                   0,
				expectedPartitionsShardedTo: 3,
			},
			"shardSize=1 -> shards to one partition": {
				numIngesters:                3,
				shardSize:                   1,
				expectedPartitionsShardedTo: 1,
			},
			"shardSize=2 -> shards to two partitions": {
				numIngesters:                3,
				shardSize:                   2,
				expectedPartitionsShardedTo: 2,
			},
		}
		for name, test := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				kafkaWriter := &mockKafkaProducer{
					failOnWrite: false,
				}
				distributors, _ := prepareButDontStart(t, 1, test.numIngesters, limits, nil)
				for _, d := range distributors {
					d.cfg.KafkaEnabled = true
					d.cfg.IngesterEnabled = false
					d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
					d.kafkaWriter = kafkaWriter

					distributorLimits := &validation.Limits{}
					flagext.DefaultValues(distributorLimits)
					distributorLimits.IngestionPartitionsTenantShardSize = test.shardSize
					overrides, err := validation.NewOverrides(*distributorLimits, nil)
					require.NoError(t, err)
					validator, err := NewValidator(overrides, nil)
					require.NoError(t, err)
					d.validator = validator
				}
				startAndWaitRunningDistributors(t, distributors)

				for i := 0; i < 1000; i++ {
					_, err := distributors[0].Push(ctx, makeWriteRequestWithLabels(
						10, 64, []string{fmt.Sprintf(`{foo="%s"}`, strconv.Itoa(i))},
						false, false, false))
					require.NoError(t, err)
				}

				require.Greater(t, kafkaWriter.pushes, uint64(0))
				partitionCounts := map[int32]uint32{}
				for _, record := range kafkaWriter.records {
					partitionID := record.Partition
					partitionCounts[partitionID]++
				}
				require.Equal(t, test.expectedPartitionsShardedTo, len(partitionCounts))
			})
		}
	})
}

func Test_SortLabelsOnPush(t *testing.T) {
	t.Run("with service_name already present in labels", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		ingester := &mockIngester{}
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		request := makeWriteRequest(10, 10)
		request.Streams[0].Labels = `{buzz="f", service_name="foo", a="b"}`
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{a="b", buzz="f", service_name="foo"}`, topVal.Streams[0].Labels)
	})
}

func Test_TruncateLogLines(t *testing.T) {
	setup := func() (*validation.Limits, *mockIngester) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)

		limits.MaxLineSize = 5
		limits.MaxLineSizeTruncate = true
		return limits, &mockIngester{}
	}

	t.Run("it truncates lines to MaxLineSize when MaxLineSizeTruncate is true", func(t *testing.T) {
		limits, ingester := setup()
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		// reset metrics in case they were set from a previous test.
		validation.MutatedSamples.Reset()
		validation.MutatedBytes.Reset()

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Len(t, topVal.Streams[0].Entries[0].Line, 5)

		// Truncation must be observable via the mutated_* metrics: 1 line of 10
		// bytes truncated to 5 bytes => 1 sample, 5 bytes mutated.
		assert.Equal(t, float64(1), testutil.ToFloat64(validation.MutatedSamples.WithLabelValues(validation.LineTooLong, "test")))
		assert.Equal(t, float64(5), testutil.ToFloat64(validation.MutatedBytes.WithLabelValues(validation.LineTooLong, "test")))
	})

	t.Run("it truncates lines and adds suffix if configured", func(t *testing.T) {
		limits, ingester := setup()
		limits.MaxLineSize = 8
		limits.MaxLineSizeTruncateIdentifier = "[...]"

		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Len(t, topVal.Streams[0].Entries[0].Line, int(limits.MaxLineSize))
		require.Equal(t, "000[...]", topVal.Streams[0].Entries[0].Line)
	})
}

func Test_DiscardEmptyStreamsAfterValidation(t *testing.T) {
	setup := func() (*validation.Limits, *mockIngester) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)

		limits.MaxLineSize = 5
		return limits, &mockIngester{}
	}

	t.Run("it discards invalid entries and discards resulting empty streams completely", func(t *testing.T) {
		limits, ingester := setup()
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.Equal(t, err, httpgrpc.Errorf(http.StatusBadRequest, "%s", fmt.Sprintf(validation.LineTooLongErrorMsg, 5, "{foo=\"bar\"}", 10)))
		topVal := ingester.Peek()
		require.Nil(t, topVal)
	})

	t.Run("it returns unprocessable entity error if the streams is empty", func(t *testing.T) {
		limits, ingester := setup()
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		_, err := distributors[0].Push(ctx, makeWriteRequestWithLabels(1, 1, []string{}, false, false, false))
		require.Equal(t, err, httpgrpc.Errorf(http.StatusUnprocessableEntity, validation.MissingStreamsErrorMsg))
		topVal := ingester.Peek()
		require.Nil(t, topVal)
	})
}

func TestStreamShard(t *testing.T) {
	// setup base stream.
	baseStream := logproto.Stream{}
	baseLabels := "{app='myapp'}"
	lbs, err := syntax.ParseLabels(baseLabels)
	require.NoError(t, err)
	baseStream.Hash = labels.StableHash(lbs)
	baseStream.Labels = lbs.String()

	totalEntries := generateEntries(100)
	desiredRate := loki_flagext.ByteSize(300)

	for _, tc := range []struct {
		name       string
		entries    []logproto.Entry
		streamSize int

		wantDerivedStreamSize int
	}{
		{
			name:                  "zero shard because no entries",
			entries:               nil,
			streamSize:            50,
			wantDerivedStreamSize: 1,
		},
		{
			name:                  "one shard with one entry",
			streamSize:            1,
			entries:               totalEntries[0:1],
			wantDerivedStreamSize: 1,
		},
		{
			name:                  "two shards with 3 entries",
			streamSize:            desiredRate.Val() + 1, // pass the desired rate by 1 byte to force two shards.
			entries:               totalEntries[0:3],
			wantDerivedStreamSize: 2,
		},
		{
			name:                  "two shards with 5 entries",
			entries:               totalEntries[0:5],
			streamSize:            desiredRate.Val() + 1, // pass the desired rate for 1 byte to force two shards.
			wantDerivedStreamSize: 2,
		},
		{
			name:                  "one shard with 20 entries",
			entries:               totalEntries[0:20],
			streamSize:            1,
			wantDerivedStreamSize: 1,
		},
		{
			name:                  "two shards with 20 entries",
			entries:               totalEntries[0:20],
			streamSize:            desiredRate.Val() + 1, // pass desired rate by 1 to force two shards.
			wantDerivedStreamSize: 2,
		},
		{
			name:                  "four shards with 20 entries",
			entries:               totalEntries[0:20],
			streamSize:            1 + (desiredRate.Val() * 3), // force 4 shards.
			wantDerivedStreamSize: 4,
		},
		{
			name:                  "size for four shards with 2 entries, ends up with 4 shards ",
			streamSize:            1 + (desiredRate.Val() * 3), // force 4 shards.
			entries:               totalEntries[0:2],
			wantDerivedStreamSize: 2,
		},
		{
			name:                  "four shards with 1 entry, ends up with 1 shard only",
			entries:               totalEntries[0:1],
			wantDerivedStreamSize: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseStream.Entries = tc.entries

			distributorLimits := &validation.Limits{}
			flagext.DefaultValues(distributorLimits)
			distributorLimits.ShardStreams.DesiredRate = desiredRate

			overrides, err := validation.NewOverrides(*distributorLimits, nil)
			require.NoError(t, err)

			validator, err := NewValidator(overrides, nil)
			require.NoError(t, err)

			d := Distributor{
				rateStore:    &fakeRateStore{pushRate: 1},
				validator:    validator,
				m:            newMetrics(prometheus.NewPedanticRegistry()),
				shardTracker: NewShardTracker(),
			}

			derivedStreams := d.shardStream(*logproto.FromStream(baseStream), mustParseLabels(baseStream.Labels), tc.streamSize, "fake", "", d.validator.ShardStreams("fake"))
			require.Len(t, derivedStreams, tc.wantDerivedStreamSize)

			// Each shard must have its own ring token.
			ringKeys := map[uint32]struct{}{}
			for _, s := range derivedStreams {
				// Generate sorted labels
				lbls, err := syntax.ParseLabels(s.Stream.Labels)
				require.NoError(t, err)

				require.Equal(t, labels.StableHash(lbls), s.Stream.Hash)
				require.Equal(t, lbls.String(), s.Stream.Labels)
				ringKeys[s.HashKey] = struct{}{}
			}
			require.Len(t, ringKeys, tc.wantDerivedStreamSize, "shards share a ring token")
		})
	}
}

func TestStreamShardAcrossCalls(t *testing.T) {
	// setup base stream.
	baseStream := logproto.Stream{}
	baseLabels := "{app='myapp'}"
	lbs, err := syntax.ParseLabels(baseLabels)
	require.NoError(t, err)
	baseStream.Hash = labels.StableHash(lbs)
	baseStream.Labels = lbs.String()
	baseStream.Entries = generateEntries(2)

	streamRate := loki_flagext.ByteSize(400).Val()

	distributorLimits := &validation.Limits{}
	flagext.DefaultValues(distributorLimits)
	distributorLimits.ShardStreams.DesiredRate = loki_flagext.ByteSize(100)

	overrides, err := validation.NewOverrides(*distributorLimits, nil)
	require.NoError(t, err)

	validator, err := NewValidator(overrides, nil)
	require.NoError(t, err)

	t.Run("it generates 4 shards across 2 calls when calculated shards = 2 * entries per call", func(t *testing.T) {
		d := Distributor{
			rateStore:    &fakeRateStore{pushRate: 1},
			validator:    validator,
			m:            newMetrics(prometheus.NewPedanticRegistry()),
			shardTracker: NewShardTracker(),
		}

		derivedStreams := d.shardStream(*logproto.FromStream(baseStream), mustParseLabels(baseStream.Labels), streamRate, "fake", "", d.validator.ShardStreams("fake"))
		require.Len(t, derivedStreams, 2)

		for i, s := range derivedStreams {
			require.Len(t, s.Stream.FlatView().Entries, 1)
			lbls, err := syntax.ParseLabels(s.Stream.Labels)
			require.NoError(t, err)

			require.Equal(t, lbls.Get(ingester.ShardLbName), fmt.Sprint(i))
		}

		derivedStreams = d.shardStream(*logproto.FromStream(baseStream), mustParseLabels(baseStream.Labels), streamRate, "fake", "", d.validator.ShardStreams("fake"))
		require.Len(t, derivedStreams, 2)

		for i, s := range derivedStreams {
			require.Len(t, s.Stream.FlatView().Entries, 1)
			lbls, err := syntax.ParseLabels(s.Stream.Labels)
			require.NoError(t, err)

			require.Equal(t, lbls.Get(ingester.ShardLbName), fmt.Sprint(i+2))
		}
	})
}

func generateEntries(n int) []logproto.Entry {
	var entries []logproto.Entry
	for i := 0; i < n; i++ {
		entries = append(entries, logproto.Entry{
			Line:      fmt.Sprintf("log line %d", i),
			Timestamp: time.Now(),
		})
	}
	return entries
}

func BenchmarkShardStream(b *testing.B) {
	stream := logproto.Stream{}
	lbs, err := syntax.ParseLabels("{app='myapp', job='fizzbuzz'}")
	require.NoError(b, err)
	stream.Hash = labels.StableHash(lbs)
	stream.Labels = lbs.String()

	allEntries := generateEntries(25000)

	desiredRate := 3000

	distributorLimits := &validation.Limits{}
	flagext.DefaultValues(distributorLimits)
	distributorLimits.ShardStreams.DesiredRate = loki_flagext.ByteSize(desiredRate)

	overrides, err := validation.NewOverrides(*distributorLimits, nil)
	require.NoError(b, err)

	validator, err := NewValidator(overrides, nil)
	require.NoError(b, err)

	distributorBuilder := func(shards int) *Distributor {
		d := &Distributor{
			validator:    validator,
			m:            newMetrics(prometheus.NewPedanticRegistry()),
			shardTracker: NewShardTracker(),
			// streamSize is always zero, so number of shards will be dictated just by the rate returned from store.
			rateStore: &fakeRateStore{rate: int64(desiredRate*shards - 1)},
		}

		return d
	}

	b.Run("high number of entries, low number of shards", func(b *testing.B) {
		d := distributorBuilder(2)
		stream.Entries = allEntries

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(*logproto.FromStream(stream), mustParseLabels(stream.Labels), 0, "fake", "", d.validator.ShardStreams("fake")) //nolint:errcheck
		}
	})

	b.Run("low number of entries, low number of shards", func(b *testing.B) {
		d := distributorBuilder(2)
		stream.Entries = nil

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(*logproto.FromStream(stream), mustParseLabels(stream.Labels), 0, "fake", "", d.validator.ShardStreams("fake")) //nolint:errcheck
		}
	})

	b.Run("high number of entries, high number of shards", func(b *testing.B) {
		d := distributorBuilder(64)
		stream.Entries = allEntries

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(*logproto.FromStream(stream), mustParseLabels(stream.Labels), 0, "fake", "", d.validator.ShardStreams("fake")) //nolint:errcheck
		}
	})

	b.Run("low number of entries, high number of shards", func(b *testing.B) {
		d := distributorBuilder(64)
		stream.Entries = nil

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(*logproto.FromStream(stream), mustParseLabels(stream.Labels), 0, "fake", "", d.validator.ShardStreams("fake")) //nolint:errcheck
		}
	})
}

func Benchmark_SortLabelsOnPush(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	d := distributors[0]
	request := makeWriteRequest(10, 10)
	streamResolver := newRequestScopedStreamResolver("123", d.validator.Limits, nil)
	vCtx := d.validator.getValidationContextForTime(testTime, "123")
	for n := 0; n < b.N; n++ {
		stream := request.Streams[0]
		stream.Labels = `{buzz="f", a="b"}`
		_, _, _, _, _, err := d.parseStreamLabels(context.Background(), vCtx, stream.Labels, *logproto.FromStream(stream), streamResolver, constants.Loki)
		if err != nil {
			panic("parseStreamLabels fail,err:" + err.Error())
		}
	}
}

func TestParseStreamLabels(t *testing.T) {
	defaultLimit := &validation.Limits{}
	flagext.DefaultValues(defaultLimit)

	for _, tc := range []struct {
		name           string
		origLabels     string
		expectedLabels labels.Labels
		expectedErr    error
		generateLimits func() *validation.Limits
	}{
		{
			name:       "service name label should not get counted against max labels count",
			origLabels: `{foo="bar", service_name="unknown_service"}`,
			generateLimits: func() *validation.Limits {
				limits := &validation.Limits{}
				flagext.DefaultValues(limits)
				limits.MaxLabelNamesPerSeries = 1
				return limits
			},
			expectedLabels: labels.FromStrings(
				"foo", "bar",
				loghttp_push.LabelServiceName, loghttp_push.ServiceUnknown,
			),
		},
	} {
		limits := tc.generateLimits()
		distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
		d := distributors[0]

		vCtx := d.validator.getValidationContextForTime(testTime, "123")
		streamResolver := newRequestScopedStreamResolver("123", d.validator.Limits, nil)
		t.Run(tc.name, func(t *testing.T) {
			lbs, lbsString, hash, _, _, err := d.parseStreamLabels(context.Background(), vCtx, tc.origLabels, *logproto.FromStream(logproto.Stream{
				Labels: tc.origLabels,
			}), streamResolver, constants.Loki)
			if tc.expectedErr != nil {
				require.Equal(t, tc.expectedErr, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedLabels.String(), lbsString)
			require.Equal(t, tc.expectedLabels, lbs)
			require.Equal(t, labels.StableHash(tc.expectedLabels), hash)
		})
	}
}

func Benchmark_Push(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.IngestionBurstSizeMB = math.MaxInt32
	limits.CardinalityLimit = math.MaxInt32
	limits.IngestionRateMB = math.MaxInt32
	limits.MaxLineSize = math.MaxInt32
	limits.RejectOldSamples = true
	limits.RejectOldSamplesMaxAge = model.Duration(24 * time.Hour)
	limits.CreationGracePeriod = model.Duration(24 * time.Hour)
	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	b.ResetTimer()
	b.ReportAllocs()

	b.Run("no structured metadata", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			request := makeWriteRequestWithLabels(100000, 100, []string{`{foo="bar"}`}, false, false, false)
			_, err := distributors[0].Push(ctx, request)
			if err != nil {
				require.NoError(b, err)
			}
		}
	})

	b.Run("all valid structured metadata", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			request := makeWriteRequestWithLabels(100000, 100, []string{`{foo="bar"}`}, true, false, false)
			_, err := distributors[0].Push(ctx, request)
			if err != nil {
				require.NoError(b, err)
			}
		}
	})

	b.Run("structured metadata with invalid names", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			request := makeWriteRequestWithLabels(100000, 100, []string{`{foo="bar"}`}, true, true, false)
			_, err := distributors[0].Push(ctx, request)
			if err != nil {
				require.NoError(b, err)
			}
		}
	})

	b.Run("structured metadata with invalid values", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			request := makeWriteRequestWithLabels(100000, 100, []string{`{foo="bar"}`}, true, false, true)
			_, err := distributors[0].Push(ctx, request)
			if err != nil {
				require.NoError(b, err)
			}
		}
	})

	b.Run("structured metadata with invalid names and values", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			request := makeWriteRequestWithLabels(100000, 100, []string{`{foo="bar"}`}, true, true, true)
			_, err := distributors[0].Push(ctx, request)
			if err != nil {
				require.NoError(b, err)
			}
		}
	})
}

func TestShardCalculation(t *testing.T) {
	megabyte := 1000
	desiredRate := 3 * megabyte

	for _, tc := range []struct {
		name       string
		streamSize int
		rate       int64

		wantShards int
	}{
		{
			name:       "not enough data to be sharded, stream size (1mb) + ingested rate (0mb) < 3mb",
			streamSize: 1 * megabyte,
			rate:       0,
			wantShards: 1,
		},
		{
			name:       "enough data to have two shards, stream size (1mb) + ingested rate (4mb) > 3mb",
			streamSize: 1 * megabyte,
			rate:       int64(desiredRate + 1),
			wantShards: 2,
		},
		{
			name:       "enough data to have two shards, stream size (4mb) + ingested rate (0mb) > 3mb",
			streamSize: 4 * megabyte,
			rate:       0,
			wantShards: 2,
		},
		{
			name:       "a lot of shards, stream size (1mb) + ingested rate (300mb) > 3mb",
			streamSize: 1 * megabyte,
			rate:       int64(300 * megabyte),
			wantShards: 101,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateShards(tc.rate, tc.streamSize, desiredRate)
			require.Equal(t, tc.wantShards, got)
		})
	}
}

func TestShardCountFor(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stream      *logproto.Stream
		rate        int64
		pushRate    float64
		desiredRate loki_flagext.ByteSize

		pushSize   int // used for sanity check.
		wantShards int
		wantErr    bool
	}{
		{
			name:        "2 entries with zero rate and desired rate == 0, return 1 shard",
			stream:      &logproto.Stream{Hash: 1},
			rate:        0,
			desiredRate: 0, // in bytes
			pushSize:    2, // in bytes
			pushRate:    1,
			wantShards:  1,
			wantErr:     false,
		},
		{
			// although in this scenario we have enough size to be sharded, we can't divide the number of entries between the ingesters
			// because the number of entries is lower than the number of shards.
			name:        "not enough entries to be sharded, stream size (2b) + ingested rate (0b) < 3b = 1 shard but 0 entries",
			stream:      &logproto.Stream{Hash: 1, Entries: []logproto.Entry{{Line: "abcde"}}},
			rate:        0,
			desiredRate: 3, // in bytes
			pushSize:    2, // in bytes
			pushRate:    1,
			wantShards:  1,
			wantErr:     true,
		},
		{
			name:        "not enough data to be sharded, stream size (18b) + ingested rate (0b) < 20b",
			stream:      &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}}},
			rate:        0,
			desiredRate: 20, // in bytes
			pushSize:    18, // in bytes
			pushRate:    1,
			wantShards:  1,
			wantErr:     false,
		},
		{
			name:        "enough data to have two shards, stream size (36b) + ingested rate (24b) > 40b",
			stream:      &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:        24, // in bytes
			desiredRate: 40, // in bytes
			pushSize:    36, // in bytes
			pushRate:    1,
			wantShards:  2,
			wantErr:     false,
		},
		{
			// although the ingested rate by an ingester is 0, the stream is big enough to be sharded.
			name:        "enough data to have two shards, stream size (36b) + ingested rate (0b) > 22b",
			stream:      &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:        0,  // in bytes
			desiredRate: 22, // in bytes
			pushSize:    36, // in bytes
			pushRate:    1,
			wantShards:  2,
			wantErr:     false,
		},
		{
			name: "a lot of shards, stream size (90b) + ingested rate (300mb) > 3mb",
			stream: &logproto.Stream{Entries: []logproto.Entry{
				{Line: "a"}, {Line: "b"}, {Line: "c"}, {Line: "d"}, {Line: "e"},
			}},
			rate:        0,  // in bytes
			desiredRate: 22, // in bytes
			pushSize:    90, // in bytes
			pushRate:    1,
			wantShards:  5,
			wantErr:     false,
		},
		{
			name:        "take push rate into account. Only generate two shards even though this push is quite large",
			stream:      &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:        24,        // in bytes
			pushRate:    1.0 / 6.0, // one push every 6 seconds
			desiredRate: 40,        // in bytes
			pushSize:    200,       // in bytes
			wantShards:  2,
			wantErr:     false,
		},
		{
			name:        "If the push rate is 0, it's the first push of this stream. Don't shard",
			stream:      &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:        24, // in bytes
			pushRate:    0,
			desiredRate: 40,  // in bytes
			pushSize:    200, // in bytes
			wantShards:  1,
			wantErr:     false,
		},
		{
			name:        "If the push rate is greater than 1, use the payload size",
			stream:      &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:        24, // in bytes
			pushRate:    3,
			desiredRate: 40,  // in bytes
			pushSize:    200, // in bytes
			wantShards:  6,
			wantErr:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.ShardStreams.DesiredRate = tc.desiredRate

			d := &Distributor{
				rateStore: &fakeRateStore{tc.rate, tc.pushRate},
			}
			nested := *logproto.FromStream(*tc.stream)
			got := d.shardCountFor(util_log.Logger, nested, tc.pushSize, "fake", limits.ShardStreams)
			require.Equal(t, tc.wantShards, got)
		})
	}
}

func Benchmark_PushWithLineTruncation(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	limits.IngestionRateMB = math.MaxInt32
	limits.MaxLineSizeTruncate = true
	limits.MaxLineSize = 50

	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {

		_, err := distributors[0].Push(ctx, request)
		if err != nil {
			require.NoError(b, err)
		}
	}
}

func TestDistributor_PushIngestionRateLimiter(t *testing.T) {
	type testPush struct {
		bytes         int
		expectedError error
	}

	tests := map[string]struct {
		distributors          int
		ingestionRateStrategy string
		ingestionRateMB       float64
		ingestionBurstSizeMB  float64
		pushes                []testPush
	}{
		"local strategy: limit should be set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.LocalIngestionRateStrategy,
			ingestionRateMB:       datasize.ByteSize(100).MBytes(),
			ingestionBurstSizeMB:  datasize.ByteSize(100).MBytes(),
			pushes: []testPush{
				{bytes: 50, expectedError: nil},
				{bytes: 60, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 1, 60)},
				{bytes: 50, expectedError: nil},
				{bytes: 40, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 1, 40)},
			},
		},
		"global strategy: limit should be evenly shared across distributors": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       datasize.ByteSize(200).MBytes(),
			ingestionBurstSizeMB:  datasize.ByteSize(100).MBytes(),
			pushes: []testPush{
				{bytes: 60, expectedError: nil},
				{bytes: 50, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 1, 50)},
				{bytes: 40, expectedError: nil},
				{bytes: 30, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 1, 30)},
			},
		},
		"global strategy: burst should set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       datasize.ByteSize(100).MBytes(),
			ingestionBurstSizeMB:  datasize.ByteSize(200).MBytes(),
			pushes: []testPush{
				{bytes: 150, expectedError: nil},
				{bytes: 60, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 50, 1, 60)},
				{bytes: 50, expectedError: nil},
				{bytes: 30, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 50, 1, 30)},
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.IngestionRateStrategy = testData.ingestionRateStrategy
			limits.IngestionRateMB = testData.ingestionRateMB
			limits.IngestionBurstSizeMB = testData.ingestionBurstSizeMB

			distributors, _ := prepare(t, testData.distributors, 5, limits, nil)
			for _, push := range testData.pushes {
				request := makeWriteRequest(1, push.bytes)
				response, err := distributors[0].Push(ctx, request)

				if push.expectedError == nil {
					assert.NoError(t, err)
					assert.Equal(t, success, response)
				} else {
					assert.Nil(t, response)
					assert.Equal(t, push.expectedError, err)
				}
			}
		})
	}
}

func TestDistributor_PushIngestionRateLimitedByPolicy(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
	// Generous tenant-wide limit so the tenant bucket never rejects in this test.
	limits.IngestionRateMB = datasize.ByteSize(1000).MBytes()
	limits.IngestionBurstSizeMB = datasize.ByteSize(1000).MBytes()
	// {foo="bar"} resolves to the "finance" policy, which carries a strict ingestion rate
	// override that must REPLACE the tenant limit for those streams.
	limits.PolicyStreamMapping = validation.PolicyStreamMapping{
		"finance": []*validation.PriorityStream{{Selector: `{foo="bar"}`, Priority: 1}},
	}
	limits.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"finance": {
			IngestionRateMB:      ptr(datasize.ByteSize(50).MBytes()),
			IngestionBurstSizeMB: ptr(datasize.ByteSize(50).MBytes()),
		},
	}
	// Validate populates the stream-selector matchers used by PolicyFor (the real config-load
	// path does this); without it an empty matcher set would match every stream.
	require.NoError(t, limits.Validate())

	distributors, _ := prepare(t, 1, 5, limits, nil)

	// A push under the "finance" policy exceeding its 50-byte budget is rejected against the
	// per-policy limit (50), not the generous tenant limit.
	resp, err := distributors[0].Push(ctx, makeWriteRequestWithLabels(1, 60, []string{`{foo="bar"}`}, false, false, false))
	assert.Nil(t, resp)
	assert.Equal(t, httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedPolicyErrorMsg, "test", "finance", 50, 1, 60), err)

	// A push with labels not matched to any policy uses the tenant-wide bucket and is allowed.
	resp, err = distributors[0].Push(ctx, makeWriteRequestWithLabels(1, 60, []string{`{other="x"}`}, false, false, false))
	assert.NoError(t, err)
	assert.Equal(t, success, resp)
}

// TestDistributor_PushIngestionRateLimitPolicyAllOrNothing verifies that when a request mixes
// streams from two policies and only one is over its limit, rejecting the request does NOT
// consume tokens from the under-limit policy's bucket (the reservations are cancelled).
func TestDistributor_PushIngestionRateLimitPolicyAllOrNothing(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
	limits.IngestionRateMB = datasize.ByteSize(1000).MBytes()
	limits.IngestionBurstSizeMB = datasize.ByteSize(1000).MBytes()
	limits.PolicyStreamMapping = validation.PolicyStreamMapping{
		"finance": []*validation.PriorityStream{{Selector: `{app="finance"}`, Priority: 1}},
		"ops":     []*validation.PriorityStream{{Selector: `{app="ops"}`, Priority: 1}},
	}
	// Both policies get a 50-byte budget.
	limits.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"finance": {IngestionRateMB: ptr(datasize.ByteSize(50).MBytes()), IngestionBurstSizeMB: ptr(datasize.ByteSize(50).MBytes())},
		"ops":     {IngestionRateMB: ptr(datasize.ByteSize(50).MBytes()), IngestionBurstSizeMB: ptr(datasize.ByteSize(50).MBytes())},
	}
	require.NoError(t, limits.Validate())

	distributors, _ := prepare(t, 1, 5, limits, nil)

	// Request mixes a finance stream that is over its 50-byte budget (60 bytes) with an ops
	// stream that is under its budget (40 bytes). The whole request must be rejected because
	// finance is over limit.
	req := makeWriteRequestWithLabels(1, 60, []string{`{app="finance"}`}, false, false, false)
	opsStreams := makeWriteRequestWithLabels(1, 40, []string{`{app="ops"}`}, false, false, false)
	req.Streams = append(req.Streams, opsStreams.Streams...)

	resp, err := distributors[0].Push(ctx, req)
	assert.Nil(t, resp)
	assert.Equal(t, httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedPolicyErrorMsg, "test", "finance", 50, 1, 60), err)

	// The ops bucket must NOT have been drained by the rejected request: a fresh ops push that
	// fills its entire 50-byte budget still succeeds. (Without reservation cancellation, the
	// earlier 40 bytes would have been consumed and this 50-byte push would be rate limited.)
	resp, err = distributors[0].Push(ctx, makeWriteRequestWithLabels(1, 50, []string{`{app="ops"}`}, false, false, false))
	assert.NoError(t, err)
	assert.Equal(t, success, resp)
}

// TestDistributor_PushIngestionRateLimitMultiplePolicies verifies that when several policy
// buckets are over their limit in the same request, the 429 error deterministically enumerates
// all exceeded policies (sorted), regardless of map iteration order.
func TestDistributor_PushIngestionRateLimitMultiplePolicies(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
	limits.IngestionRateMB = datasize.ByteSize(1000).MBytes()
	limits.IngestionBurstSizeMB = datasize.ByteSize(1000).MBytes()
	limits.PolicyStreamMapping = validation.PolicyStreamMapping{
		"finance": []*validation.PriorityStream{{Selector: `{app="finance"}`, Priority: 1}},
		"ops":     []*validation.PriorityStream{{Selector: `{app="ops"}`, Priority: 1}},
	}
	// Both policies get a 50-byte budget; the request puts each well over (bytes > burst), so
	// both reservations are rejected deterministically on every attempt.
	limits.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"finance": {IngestionRateMB: ptr(datasize.ByteSize(50).MBytes()), IngestionBurstSizeMB: ptr(datasize.ByteSize(50).MBytes())},
		"ops":     {IngestionRateMB: ptr(datasize.ByteSize(50).MBytes()), IngestionBurstSizeMB: ptr(datasize.ByteSize(50).MBytes())},
	}
	require.NoError(t, limits.Validate())

	distributors, _ := prepare(t, 1, 5, limits, nil)

	req := makeWriteRequestWithLabels(1, 60, []string{`{app="finance"}`}, false, false, false)
	opsStreams := makeWriteRequestWithLabels(1, 70, []string{`{app="ops"}`}, false, false, false)
	req.Streams = append(req.Streams, opsStreams.Streams...)

	// The error must enumerate both policies in sorted order (finance before ops), with each
	// bucket's own limit/lines/bytes.
	expectedDetail := `policy "finance" (limit: 50 bytes/sec) ingesting 1 lines totaling 60 bytes; ` +
		`policy "ops" (limit: 50 bytes/sec) ingesting 1 lines totaling 70 bytes`
	expectedErr := httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedMultiErrorMsg, "test", expectedDetail)

	// Push twice to demonstrate the message is deterministic (independent of map iteration order).
	for i := 0; i < 2; i++ {
		resp, err := distributors[0].Push(ctx, req)
		assert.Nil(t, resp)
		assert.Equal(t, expectedErr, err)
	}
}

// TestDistributor_PushShardStreamsPolicyOverride verifies that a per-policy shard_streams
// override is applied in the push path: a policy that enables time sharding gets its streams
// split with a __time_shard__ label, while streams on the tenant default (time sharding off)
// do not.
func TestDistributor_PushShardStreamsPolicyOverride(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false // we push intentionally old logs to trigger time sharding
	// Tenant default leaves time sharding off; the "foo" policy turns it on via an override.
	limits.PolicyStreamMapping = validation.PolicyStreamMapping{
		"foo": []*validation.PriorityStream{{Selector: `{app="foo"}`, Priority: 1}},
	}
	timeOn := true
	limits.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"foo": {ShardStreams: &validation.PerPolicyConfigOverride{TimeShardingEnabled: &timeOn}},
	}
	require.NoError(t, limits.Validate())

	// prepare() builds distributors with ingester MaxChunkAge=2h → time-shard length 1h.
	distributors, ingesters := prepare(t, 1, 3, limits, nil)

	// Logs older than time_sharding_ignore_recent (40m default), spanning two 1h buckets.
	old := time.Now().Add(-3 * time.Hour)
	mkReq := func(lbls string) *logproto.PushRequest {
		return &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels: lbls,
			Entries: []logproto.Entry{
				{Timestamp: old, Line: "a"},
				{Timestamp: old.Add(time.Hour + time.Minute), Line: "b"},
			},
		}}}
	}

	_, err := distributors[0].Push(ctx, mkReq(`{app="foo"}`))
	require.NoError(t, err)
	_, err = distributors[0].Push(ctx, mkReq(`{app="other"}`))
	require.NoError(t, err)

	// The ingester writes are async (serviced by the distributor's ingester worker pool), so wait
	// until both requests' streams have reached the ingesters before asserting.
	streamPushed := func(app string) bool {
		for i := range ingesters {
			ingesters[i].mu.Lock()
			for _, pr := range ingesters[i].pushed {
				for _, st := range pr.Streams {
					if strings.Contains(st.Labels, `app="`+app+`"`) {
						ingesters[i].mu.Unlock()
						return true
					}
				}
			}
			ingesters[i].mu.Unlock()
		}
		return false
	}
	require.Eventually(t, func() bool {
		return streamPushed("foo") && streamPushed("other")
	}, time.Second, 10*time.Millisecond, "expected both streams to reach the ingesters")

	fooSharded, otherSharded := false, false
	for i := range ingesters {
		ingesters[i].mu.Lock()
		for _, pr := range ingesters[i].pushed {
			for _, st := range pr.Streams {
				if !strings.Contains(st.Labels, "__time_shard__") {
					continue
				}
				if strings.Contains(st.Labels, `app="foo"`) {
					fooSharded = true
				}
				if strings.Contains(st.Labels, `app="other"`) {
					otherSharded = true
				}
			}
		}
		ingesters[i].mu.Unlock()
	}
	require.True(t, fooSharded, "foo stream should be time-sharded via the per-policy override")
	require.False(t, otherSharded, "non-foo stream should not be time-sharded (tenant default off)")
}

// TestDistributor_PushBackfillBypassesRejectOldSamples verifies that streams carrying the internal
// backfill label skip the reject_old_samples validation (backfill data is old by definition), while
// regular streams on the same tenant are still subject to it and the too-far-in-future check stays
// in force for backfill streams.
func TestDistributor_PushBackfillBypassesRejectOldSamples(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = true
	require.NoError(t, limits.RejectOldSamplesMaxAge.Set("24h"))
	require.NoError(t, limits.Validate())

	distributors, ingesters := prepare(t, 1, 3, limits, nil)

	old := time.Now().Add(-48 * time.Hour)
	backfillLbls := fmt.Sprintf(`{app="backfilled", %s="true", %s="shard-1"}`, constants.BackfillLabel, constants.BackfillShardLabel)
	mkReq := func(lbls string, ts time.Time) *logproto.PushRequest {
		return &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels:  lbls,
			Entries: []logproto.Entry{{Timestamp: ts, Line: "a"}},
		}}}
	}

	// A regular stream with an entry older than reject_old_samples_max_age is rejected.
	_, err := distributors[0].Push(ctx, mkReq(`{app="regular"}`, old))
	require.Error(t, err)
	require.Contains(t, err.Error(), "timestamp too old")

	// A backfill stream with an entry too far in the future is still rejected.
	_, err = distributors[0].Push(ctx, mkReq(backfillLbls, time.Now().Add(time.Hour)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "timestamp too new")

	// The same old entry on a backfill stream is accepted and reaches the ingesters.
	_, err = distributors[0].Push(ctx, mkReq(backfillLbls, old))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for i := range ingesters {
			ingesters[i].mu.Lock()
			for _, pr := range ingesters[i].pushed {
				for _, st := range pr.Streams {
					if strings.Contains(st.Labels, `app="backfilled"`) {
						ingesters[i].mu.Unlock()
						return true
					}
				}
			}
			ingesters[i].mu.Unlock()
		}
		return false
	}, time.Second, 10*time.Millisecond, "expected the backfill stream to reach the ingesters")
}

func TestDistributor_PushTimeSharding(t *testing.T) {
	// prepare() uses a one-hour shard window and a 40-minute recent window.
	old := time.Now().Add(-3 * time.Hour).Truncate(time.Hour).Add(10 * time.Minute)
	recent := time.Now()
	entry := func(at time.Time, line string) logproto.Entry {
		return logproto.Entry{Timestamp: at, Line: line, StructuredMetadata: []logproto.LabelAdapter{{Name: "trace_id", Value: line}}}
	}
	stream := func(app string, entries []logproto.Entry, shardLabels ...string) logproto.Stream {
		ls := labels.FromStrings(append([]string{"app", app}, shardLabels...)...)
		return logproto.Stream{Labels: ls.String(), Hash: labels.StableHash(ls), Entries: entries}
	}
	window := func(at time.Time) string {
		start := at.Truncate(time.Hour)
		return fmt.Sprintf("%d_%d", start.Unix(), start.Add(time.Hour).Unix())
	}

	for _, tc := range []struct {
		name        string
		app         string
		entries     []logproto.Entry
		rate        *fakeRateStore
		maxChunkAge time.Duration
		want        []logproto.Stream
	}{
		{
			name: "old entries are bucketed by their window", app: "regular",
			entries: []logproto.Entry{entry(old.Add(time.Hour), "b"), entry(old, "a")},
			want: []logproto.Stream{
				stream("regular", []logproto.Entry{entry(old, "a")}, timeShardLabel, window(old)),
				stream("regular", []logproto.Entry{entry(old.Add(time.Hour), "b")}, timeShardLabel, window(old.Add(time.Hour))),
			},
		},
		{
			name: "recent entries are sorted before forwarding", app: "recent",
			entries: []logproto.Entry{entry(recent.Add(-10*time.Second), "third"), entry(recent.Add(-30*time.Second), "first"), entry(recent.Add(-20*time.Second), "second")},
			want:    []logproto.Stream{stream("recent", []logproto.Entry{entry(recent.Add(-30*time.Second), "first"), entry(recent.Add(-20*time.Second), "second"), entry(recent.Add(-10*time.Second), "third")})},
		},
		{
			name: "recent entries spanning multiple time-shard intervals are sorted", app: "recent-wide-window",
			maxChunkAge: time.Hour,
			entries:     []logproto.Entry{entry(recent, "newest"), entry(recent.Add(-35*time.Minute), "older")},
			want:        []logproto.Stream{stream("recent-wide-window", []logproto.Entry{entry(recent.Add(-35*time.Minute), "older"), entry(recent, "newest")})},
		},
		{
			name: "old and recent entries retain their metadata", app: "mixed",
			entries: []logproto.Entry{entry(recent, "newest"), entry(old, "old"), entry(recent.Add(-time.Minute), "recent")},
			want: []logproto.Stream{
				stream("mixed", []logproto.Entry{entry(old, "old")}, timeShardLabel, window(old)),
				stream("mixed", []logproto.Entry{entry(recent.Add(-time.Minute), "recent"), entry(recent, "newest")}),
			},
		},
		{
			name: "rate sharding keeps the window it divides", app: "compose",
			entries: []logproto.Entry{entry(old.Add(time.Second), "second"), entry(old, "first")},
			rate:    &fakeRateStore{rate: 1000, pushRate: 1},
			want: []logproto.Stream{
				stream("compose", []logproto.Entry{entry(old, "first")}, timeShardLabel, window(old), ingester.ShardLbName, "0"),
				stream("compose", []logproto.Entry{entry(old.Add(time.Second), "second")}, timeShardLabel, window(old), ingester.ShardLbName, "1"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			limits.ShardStreams.TimeShardingEnabled = true
			if tc.rate != nil {
				limits.ShardStreams.Enabled = true
				limits.ShardStreams.DesiredRate = 100
			}
			require.NoError(t, limits.Validate())

			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, limits, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
			d := distributors[0]
			if tc.rate != nil {
				d.rateStore = tc.rate
			}
			if tc.maxChunkAge != 0 {
				d.ingesterCfg.MaxChunkAge = tc.maxChunkAge
			}

			_, err := d.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{stream(tc.app, tc.entries)}})
			require.NoError(t, err)
			got := ing.Peek()
			require.NotNil(t, got, "nothing reached the ingesters")
			require.ElementsMatch(t, tc.want, got.Streams)
		})
	}
}

func TestDistributor_SharedMetadataLimits(t *testing.T) {
	at := time.Now().Add(-3 * time.Hour).Truncate(time.Hour).Add(10 * time.Minute)
	for _, timeSharding := range []bool{false, true} {
		for _, tc := range []struct {
			name          string
			burst         int
			metadataSize  int
			metadataCount int
			wantErr       string
		}{
			{name: "at all limits", burst: 42, metadataSize: 22, metadataCount: 3},
			{name: "over ingestion limit", burst: 41, metadataSize: 22, metadataCount: 3, wantErr: "ingestion rate limit exceeded"},
			{name: "over metadata size", burst: 42, metadataSize: 21, metadataCount: 3, wantErr: "structured metadata too large"},
			{name: "over metadata count", burst: 42, metadataSize: 22, metadataCount: 2, wantErr: "too many structured metadata labels"},
		} {
			t.Run(fmt.Sprintf("%s/time-sharding=%t", tc.name, timeSharding), func(t *testing.T) {
				lim := &validation.Limits{}
				flagext.DefaultValues(lim)
				lim.RejectOldSamples = false
				lim.DiscoverLogLevels = false
				lim.IngestionRateMB = float64(tc.burst) / (1024 * 1024)
				lim.IngestionBurstSizeMB = lim.IngestionRateMB
				lim.MaxStructuredMetadataSize = loki_flagext.ByteSize(tc.metadataSize)
				lim.MaxStructuredMetadataEntriesCount = tc.metadataCount
				lim.ShardStreams.Enabled = true
				lim.ShardStreams.DesiredRate = 50
				lim.ShardStreams.TimeShardingEnabled = timeSharding
				ing := &mockIngester{}
				distributors, _ := prepare(t, 1, 3, lim, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
				d := distributors[0]
				d.rateStore = &fakeRateStore{rate: 1, pushRate: 1}

				resourceAttrs := []logproto.LabelAdapter{{Name: "r", Value: "123456789"}}
				scopeAttrs := []logproto.LabelAdapter{{Name: "s", Value: "123456789"}}
				entry := func(line string) logproto.Entry {
					return logproto.Entry{Timestamp: at, Line: line, StructuredMetadata: []logproto.LabelAdapter{{Name: "m", Value: "v"}}}
				}
				// 42 bytes before expansion and 92 after: two rate shards at 50 bytes each.
				stream := logproto.InternalStreamAdapter{Labels: `{app="shared"}`, ResourceLogs: []logproto.ResourceLogs{{Attrs: resourceAttrs, ScopeLogs: []logproto.ScopeLogs{
					{Attrs: scopeAttrs, Entries: []logproto.Entry{entry("a"), entry("b")}},
					{Attrs: scopeAttrs, Entries: []logproto.Entry{entry("c"), entry("d")}},
				}}}}
				_, err := d.pushWithResolver(ctx, &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{stream}}, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
				if tc.wantErr != "" {
					require.ErrorContains(t, err, tc.wantErr)
					require.Nil(t, ing.Peek())
					return
				}
				require.NoError(t, err)
				var want []logproto.Stream
				for i, lines := range [][]string{{"a", "b"}, {"c", "d"}} {
					ls := labels.FromStrings("app", "shared", ingester.ShardLbName, strconv.Itoa(i))
					if timeSharding {
						start := at.Truncate(time.Hour)
						ls = labels.NewBuilder(ls).Set(timeShardLabel, fmt.Sprintf("%d_%d", start.Unix(), start.Add(time.Hour).Unix())).Labels()
					}
					out := logproto.Stream{Labels: ls.String(), Hash: labels.StableHash(ls)}
					for _, line := range lines {
						e := entry(line)
						e.StructuredMetadata = append(e.StructuredMetadata, scopeAttrs...)
						e.StructuredMetadata = append(e.StructuredMetadata, resourceAttrs...)
						out.Entries = append(out.Entries, e)
					}
					want = append(want, out)
				}
				got := ing.Peek()
				require.NotNil(t, got)
				require.ElementsMatch(t, want, got.Streams)
			})
		}
	}
}

// TestDistributor_PushBackfillDisablesTimeSharding verifies that streams carrying the internal
// backfill label are not time-sharded by Loki even when time sharding is enabled, because backfill
// workers implement time sharding on the client side (via constants.BackfillShardLabel). A regular
// old stream on the same tenant is still time-sharded.
func TestDistributor_PushBackfillDisablesTimeSharding(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false // we push intentionally old logs to trigger time sharding
	limits.ShardStreams.TimeShardingEnabled = true
	require.NoError(t, limits.Validate())

	// prepare() builds distributors with ingester MaxChunkAge=2h → time-shard length 1h.
	distributors, ingesters := prepare(t, 1, 3, limits, nil)

	// Logs older than time_sharding_ignore_recent (40m default), spanning two 1h buckets.
	old := time.Now().Add(-3 * time.Hour)
	mkReq := func(lbls string) *logproto.PushRequest {
		return &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels: lbls,
			Entries: []logproto.Entry{
				{Timestamp: old, Line: "a"},
				{Timestamp: old.Add(time.Hour + time.Minute), Line: "b"},
			},
		}}}
	}

	// A regular old stream (should be time-sharded) and a backfill stream (should not).
	_, err := distributors[0].Push(ctx, mkReq(`{app="regular"}`))
	require.NoError(t, err)
	_, err = distributors[0].Push(ctx, mkReq(fmt.Sprintf(`{app="backfilled", %s="true", %s="shard-1"}`, constants.BackfillLabel, constants.BackfillShardLabel)))
	require.NoError(t, err)

	streamPushed := func(app string) bool {
		for i := range ingesters {
			ingesters[i].mu.Lock()
			for _, pr := range ingesters[i].pushed {
				for _, st := range pr.Streams {
					if strings.Contains(st.Labels, `app="`+app+`"`) {
						ingesters[i].mu.Unlock()
						return true
					}
				}
			}
			ingesters[i].mu.Unlock()
		}
		return false
	}
	require.Eventually(t, func() bool {
		return streamPushed("regular") && streamPushed("backfilled")
	}, time.Second, 10*time.Millisecond, "expected both streams to reach the ingesters")

	regularSharded, backfilledSharded := false, false
	for i := range ingesters {
		ingesters[i].mu.Lock()
		for _, pr := range ingesters[i].pushed {
			for _, st := range pr.Streams {
				if !strings.Contains(st.Labels, "__time_shard__") {
					continue
				}
				if strings.Contains(st.Labels, `app="regular"`) {
					regularSharded = true
				}
				if strings.Contains(st.Labels, `app="backfilled"`) {
					backfilledSharded = true
				}
			}
		}
		ingesters[i].mu.Unlock()
	}
	require.True(t, regularSharded, "regular old stream should be time-sharded")
	require.False(t, backfilledSharded, "backfill stream should not be time-sharded by Loki")
}

func TestDistributor_PushIngestionBlocked(t *testing.T) {
	for _, tc := range []struct {
		name               string
		blockUntil         time.Time
		blockStatusCode    int
		expectError        bool
		expectedStatusCode int
	}{
		{
			name:               "not configured",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "not blocked",
			blockUntil:         time.Now().Add(-1 * time.Hour),
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "blocked",
			blockUntil:         time.Now().Add(1 * time.Hour),
			blockStatusCode:    456,
			expectError:        true,
			expectedStatusCode: 456,
		},
		{
			name:               "blocked with status code 200",
			blockUntil:         time.Now().Add(1 * time.Hour),
			blockStatusCode:    http.StatusOK,
			expectError:        false,
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "blocked with status code 260",
			blockUntil:         time.Now().Add(1 * time.Hour),
			blockStatusCode:    260,
			expectError:        true,
			expectedStatusCode: 260,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.BlockIngestionUntil = flagext.Time(tc.blockUntil)
			limits.BlockIngestionStatusCode = tc.blockStatusCode

			distributors, _ := prepare(t, 1, 5, limits, nil)
			request := makeWriteRequest(1, 1024)
			response, err := distributors[0].Push(ctx, request)

			if tc.expectError {
				expectedErr := fmt.Sprintf(validation.BlockedIngestionErrorMsg, "test", tc.blockUntil.Format(time.RFC3339), tc.blockStatusCode)
				require.ErrorContains(t, err, expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, success, response)
			}
		})
	}
}

func TestDistributor_PushIngestionBlockedByPolicy(t *testing.T) {
	now := time.Now()
	defaultErrCode := 260

	for _, tc := range []struct {
		name             string
		blockUntil       map[string]time.Time
		policy           string
		labels           string
		expectError      bool
		expectedErrorMsg string
		yes              bool
	}{
		{
			name:        "not blocked - no policy block configured",
			policy:      "test-policy",
			labels:      `{foo="bar"}`,
			expectError: false,
		},
		{
			name: "not blocked - policy block expired",
			blockUntil: map[string]time.Time{
				"test-policy": now.Add(-1 * time.Hour),
			},
			policy:      "test-policy",
			labels:      `{foo="bar"}`,
			expectError: false,
		},
		{
			name: "blocked - policy block active",
			blockUntil: map[string]time.Time{
				"test-policy": now.Add(1 * time.Hour),
			},
			policy:           "test-policy",
			labels:           `{foo="bar"}`,
			expectError:      true,
			expectedErrorMsg: fmt.Sprintf(validation.BlockedIngestionPolicyErrorMsg, "test", "test-policy", now.Add(1*time.Hour).Format(time.RFC3339), defaultErrCode),
			yes:              true,
		},
		{
			name: "not blocked - different policy",
			blockUntil: map[string]time.Time{
				"blocked-policy": now.Add(1 * time.Hour),
			},
			policy:      "test-policy",
			labels:      `{foo="bar"}`,
			expectError: false,
		},
		{
			name: "blocked - custom status code",
			blockUntil: map[string]time.Time{
				"test-policy": now.Add(1 * time.Hour),
			},
			policy:           "test-policy",
			labels:           `{foo="bar"}`,
			expectError:      true,
			expectedErrorMsg: fmt.Sprintf(validation.BlockedIngestionPolicyErrorMsg, "test", "test-policy", now.Add(1*time.Hour).Format(time.RFC3339), defaultErrCode),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.yes {
				return
			}
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)

			// Configure policy mapping
			limits.PolicyStreamMapping = validation.PolicyStreamMapping{
				tc.policy: []*validation.PriorityStream{
					{
						Selector: tc.labels,
						Priority: 1,
					},
				},
			}

			// Configure policy blocks
			if tc.blockUntil != nil {
				limits.BlockIngestionPolicyUntil = make(map[string]flagext.Time)
				for policy, until := range tc.blockUntil {
					limits.BlockIngestionPolicyUntil[policy] = flagext.Time(until)
				}
			}

			distributors, _ := prepare(t, 1, 3, limits, nil)
			request := makeWriteRequestWithLabels(1, 1024, []string{tc.labels}, false, false, false)
			response, err := distributors[0].Push(ctx, request)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrorMsg)
			} else {
				require.NoError(t, err)
				require.Equal(t, success, response)
			}
		})
	}
}

func prepare(t *testing.T, numDistributors, numIngesters int, limits *validation.Limits, factory func(addr string) (ring_client.PoolClient, error)) ([]*Distributor, []mockIngester) {
	t.Helper()
	distributors, ingesters := prepareButDontStart(t, numDistributors, numIngesters, limits, factory)
	startAndWaitRunningDistributors(t, distributors)
	return distributors, ingesters
}

func prepareButDontStart(t *testing.T, numDistributors, numIngesters int, limits *validation.Limits, factory func(addr string) (ring_client.PoolClient, error)) ([]*Distributor, []mockIngester) {
	t.Helper()

	ingesters := make([]mockIngester, numIngesters)
	for i := 0; i < numIngesters; i++ {
		ingesters[i] = mockIngester{}
	}

	ingesterByAddr := map[string]*mockIngester{}
	ingesterDescs := map[string]ring.InstanceDesc{}

	for i := range ingesters {
		addr := fmt.Sprintf("ingester-%d", i)
		ingesterDescs[addr] = ring.InstanceDesc{
			Addr:                addr,
			State:               ring.ACTIVE,
			Timestamp:           time.Now().Unix(),
			RegisteredTimestamp: time.Now().Add(-10 * time.Minute).Unix(),
			Tokens:              []uint32{uint32((math.MaxUint32 / numIngesters) * i)},
		}
		ingesterByAddr[addr] = &ingesters[i]
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)

	err := kvStore.CAS(context.Background(), ingester.RingKey,
		func(_ interface{}) (interface{}, bool, error) {
			return &ring.Desc{
				Ingesters: ingesterDescs,
			}, true, nil
		},
	)
	require.NoError(t, err)

	ingestersRing, err := ring.New(ring.Config{
		KVStore: kv.Config{
			Mock: kvStore,
		},
		HeartbeatTimeout:  60 * time.Minute,
		ReplicationFactor: 3,
	}, ingester.RingKey, ingester.RingKey, nil, nil)

	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), ingestersRing))

	partitions := map[int32]ring.PartitionDesc{}
	owners := map[string]ring.OwnerDesc{}
	numPartitions := max(1, numIngesters)
	for i := 0; i < numPartitions; i++ {
		partitions[int32(i)] = ring.PartitionDesc{
			Id:             int32(i),
			Tokens:         []uint32{uint32((math.MaxUint32 / numPartitions) * i)},
			State:          ring.PartitionActive,
			StateTimestamp: time.Now().Unix(),
		}
		owners[fmt.Sprintf("owner%d", i)] = ring.OwnerDesc{
			OwnedPartition:   int32(i),
			State:            ring.OwnerActive,
			UpdatedTimestamp: time.Now().Unix(),
		}
	}
	partitionRing, err := ring.NewPartitionRing(ring.PartitionRingDesc{
		Partitions: partitions,
		Owners:     owners,
	})
	require.NoError(t, err)
	partitionRingReader := mockPartitionRingReader{
		ring: partitionRing,
	}

	limitsFrontendRing, err := ring.New(ring.Config{
		KVStore: kv.Config{
			Mock: kvStore,
		},
		HeartbeatTimeout:  60 * time.Minute,
		ReplicationFactor: 1,
	}, limits_frontend.RingKey, limits_frontend.RingKey, nil, nil)
	require.NoError(t, err)

	loopbackName, err := loki_net.LoopbackInterfaceName()
	require.NoError(t, err)

	distributors := make([]*Distributor, numDistributors)
	for i := 0; i < numDistributors; i++ {
		var distributorConfig Config
		var clientConfig client.Config
		flagext.DefaultValues(&distributorConfig, &clientConfig)

		distributorConfig.DistributorRing.HeartbeatPeriod = 100 * time.Millisecond
		distributorConfig.DistributorRing.InstanceID = strconv.Itoa(rand.Int())
		distributorConfig.DistributorRing.KVStore.Mock = kvStore
		distributorConfig.DistributorRing.InstanceAddr = "127.0.0.1"
		distributorConfig.DistributorRing.InstanceInterfaceNames = []string{loopbackName}
		factoryWrap := ring_client.PoolAddrFunc(factory)
		distributorConfig.factory = factoryWrap
		if factoryWrap == nil {
			distributorConfig.factory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
				return ingesterByAddr[addr], nil
			})
		}

		overrides, err := validation.NewOverrides(*limits, nil)
		require.NoError(t, err)

		ingesterConfig := ingester.Config{MaxChunkAge: 2 * time.Hour}
		limitsFrontendCfg := limits_frontend_client.Config{}

		d, err := New(distributorConfig, ingesterConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, partitionRingReader, overrides, prometheus.NewPedanticRegistry(), constants.Loki, nil, nil, limitsFrontendCfg, limitsFrontendRing, 1, nil, nil, "", log.NewNopLogger())
		require.NoError(t, err)
		distributors[i] = d
	}

	t.Cleanup(func() {
		assert.NoError(t, closer.Close())
		for _, d := range distributors {
			assert.NoError(t, services.StopAndAwaitTerminated(context.Background(), d))
		}
		ingestersRing.StopAsync()
	})

	return distributors, ingesters
}

func startAndWaitRunningDistributors(t *testing.T, distributors []*Distributor) {
	for _, d := range distributors {
		require.NoError(t, services.StartAndAwaitRunning(context.Background(), d))
	}

	if distributors[0].distributorsLifecycler != nil {
		test.Poll(t, time.Second, len(distributors), func() interface{} {
			return distributors[0].HealthyInstancesCount()
		})
	}
}

func makeWriteRequestWithLabelsWithLevel(lines, size int, labels []string, level string) *logproto.PushRequest {
	streams := make([]logproto.Stream, len(labels))
	for i := 0; i < len(labels); i++ {
		stream := logproto.Stream{Labels: labels[i]}

		for j := 0; j < lines; j++ {
			// Construct the log line, honoring the input size
			line := "msg=an error occurred " + strconv.Itoa(j) + strings.Repeat("0", size) + " severity=" + level

			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp: time.Now().Add(time.Duration(j) * time.Millisecond),
				Line:      line,
			})
		}

		streams[i] = stream
	}

	return &logproto.PushRequest{
		Streams: streams,
	}
}

func makeWriteRequestWithLabels(lines, size int, labels []string, addStructuredMetadata, invalidName, invalidValue bool) *logproto.PushRequest {
	streams := make([]logproto.Stream, len(labels))
	for i := 0; i < len(labels); i++ {
		stream := logproto.Stream{Labels: labels[i]}

		for j := 0; j < lines; j++ {
			// Construct the log line, honoring the input size
			line := strconv.Itoa(j) + strings.Repeat("0", size)
			line = line[:size]
			entry := logproto.Entry{
				Timestamp: time.Now().Add(time.Duration(j) * time.Millisecond),
				Line:      line,
			}
			if addStructuredMetadata {
				name := smValidName
				value := smValidValue
				if invalidName {
					name = smInvalidName
				}
				if invalidValue {
					value = smInvalidValue
				}
				entry.StructuredMetadata = push.LabelsAdapter{
					{Name: name, Value: value},
				}
			}
			stream.Entries = append(stream.Entries, entry)
		}

		streams[i] = stream
	}

	return &logproto.PushRequest{
		Streams: streams,
	}
}

func makeWriteRequest(lines, size int) *logproto.PushRequest {
	return makeWriteRequestWithLabels(lines, size, []string{`{foo="bar"}`}, false, false, false)
}

type mockKafkaProducer struct {
	failOnWrite     bool
	pushes          uint64
	records         []*kgo.Record
	recordsPerTopic map[string][]*kgo.Record
	mu              sync.Mutex
}

func (m *mockKafkaProducer) ProduceSync(_ context.Context, records []*kgo.Record) kgo.ProduceResults {
	m.mu.Lock()
	defer m.mu.Unlock()
	results := make(kgo.ProduceResults, 0, len(records))
	if m.failOnWrite {
		// We must append a result for each record that has both the record and the
		// error, as this is how it works in [kgo].
		for _, record := range records {
			results = append(results, kgo.ProduceResult{
				Record: record,
				Err:    kgo.ErrRecordTimeout,
			})
		}
	} else {
		m.pushes++
		m.records = append(m.records, records...)
		if m.recordsPerTopic == nil {
			m.recordsPerTopic = make(map[string][]*kgo.Record)
		}
		for _, record := range records {
			m.recordsPerTopic[record.Topic] = append(m.recordsPerTopic[record.Topic], record)
			results = append(results, kgo.ProduceResult{
				Record: record,
			})
		}
	}
	return results
}

func (m *mockKafkaProducer) Close() {}

type mockPartitionRingReader struct {
	ring *ring.PartitionRing
}

func (m mockPartitionRingReader) PartitionRing() *ring.PartitionRing {
	return m.ring
}

type mockIngester struct {
	grpc_health_v1.HealthClient
	logproto.PusherClient
	logproto.StreamDataClient

	failAfter    time.Duration
	succeedAfter time.Duration
	mu           sync.Mutex
	pushed       []*logproto.PushRequest
}

func (i *mockIngester) Push(_ context.Context, in *logproto.PushRequest, _ ...grpc.CallOption) (*logproto.PushResponse, error) {
	if i.failAfter > 0 {
		time.Sleep(i.failAfter)
		return nil, fmt.Errorf("push request failed")
	}
	if i.succeedAfter > 0 {
		time.Sleep(i.succeedAfter)
	}

	labelNamer := otlptranslator.LabelNamer{}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, s := range in.Streams {
		for _, e := range s.Entries {
			for _, sm := range e.StructuredMetadata {
				if strings.ContainsRune(sm.Value, utf8.RuneError) {
					return nil, fmt.Errorf("sm value was not sanitized before being pushed to ignester, invalid utf 8 rune %d", utf8.RuneError)
				}
				name, err := labelNamer.Build(sm.Name)
				if err != nil {
					return nil, err
				}
				if sm.Name != name {
					return nil, fmt.Errorf("sm name was not sanitized before being sent to ingester, contained characters %s", sm.Name)

				}
			}
		}
	}

	i.pushed = append(i.pushed, in)
	return nil, nil
}

func (i *mockIngester) Peek() *logproto.PushRequest {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(i.pushed) == 0 {
		return nil
	}

	return i.pushed[0]
}

func (i *mockIngester) GetStreamRates(_ context.Context, _ *logproto.StreamRatesRequest, _ ...grpc.CallOption) (*logproto.StreamRatesResponse, error) {
	return &logproto.StreamRatesResponse{}, nil
}

func (i *mockIngester) Close() error {
	return nil
}

type fakeRateStore struct {
	rate     int64
	pushRate float64
}

func (s *fakeRateStore) RateFor(_ string, _ uint64) (int64, float64) {
	return s.rate, s.pushRate
}

type mockTee struct {
	mu         sync.Mutex
	duplicated [][]KeyedStream
	tenant     string
}

func (mt *mockTee) Duplicate(_ context.Context, tenant string, streams []KeyedStream, _ *PushTracker) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.duplicated = append(mt.duplicated, streams)
	mt.tenant = tenant
}

func (mt *mockTee) Register(_ context.Context, _ string, _ []KeyedStream, _ *PushTracker) {
}

// mockFailingTee is a mock tee that always fails with an error.
type mockFailingTee struct {
	err error
}

func (mt *mockFailingTee) Duplicate(_ context.Context, _ string, streams []KeyedStream, pushTracker *PushTracker) {
	// Report failure for each stream
	for range streams {
		pushTracker.doneWithResult(mt.err)
	}
}

func (mt *mockFailingTee) Register(_ context.Context, _ string, streams []KeyedStream, pushTracker *PushTracker) {
	pushTracker.streamsPending.Add(int32(len(streams)))
}

func TestDistributorTee(t *testing.T) {
	data := []*logproto.PushRequest{
		{
			Streams: []logproto.Stream{
				{
					Labels: "{job=\"foo\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(123456, 0), Line: "line 1"},
						{Timestamp: time.Unix(123457, 0), Line: "line 2"},
					},
				},
			},
		},
		{
			Streams: []logproto.Stream{
				{
					Labels: "{job=\"foo\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(123458, 0), Line: "line 3"},
						{Timestamp: time.Unix(123459, 0), Line: "line 4"},
					},
				},
				{
					Labels: "{job=\"bar\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(123458, 0), Line: "line 5"},
						{Timestamp: time.Unix(123459, 0), Line: "line 6"},
					},
				},
			},
		},
	}

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	distributors, _ := prepare(t, 1, 3, limits, nil)

	tee := mockTee{}
	distributors[0].tee = &tee

	for i, td := range data {
		_, err := distributors[0].Push(ctx, td)
		require.NoError(t, err)

		for j, streams := range td.Streams {
			assert.Equal(t, tee.duplicated[i][j].Stream.FlatView().Entries, streams.Entries)
		}

		require.Equal(t, "test", tee.tenant)
	}
}

func TestDistributorTeeFailure(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	distributors, _ := prepare(t, 1, 3, limits, nil)

	expectedErr := errors.New("tee failure")
	tee := &mockFailingTee{err: expectedErr}
	distributors[0].tee = tee

	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: "{job=\"foo\"}",
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(123456, 0), Line: "line 1"},
				},
			},
		},
	}

	_, err := distributors[0].Push(ctx, req)
	require.Error(t, err)
	require.ErrorIs(t, err, expectedErr)
}

func TestDistributor_StructuredMetadataSanitization(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	for _, tc := range []struct {
		req              *logproto.PushRequest
		expectedResponse *logproto.PushResponse
		numSanitizations float64
	}{
		{
			makeWriteRequestWithLabels(10, 10, []string{`{foo="bar"}`}, true, false, false),
			success,
			0,
		},
		{
			makeWriteRequestWithLabels(10, 10, []string{`{foo="bar"}`}, true, true, false),
			success,
			10,
		},
		{
			makeWriteRequestWithLabels(10, 10, []string{`{foo="bar"}`}, true, false, true),
			success,
			10,
		},
		{
			makeWriteRequestWithLabels(10, 10, []string{`{foo="bar"}`}, true, true, true),
			success,
			20,
		},
	} {
		distributors, _ := prepare(t, 1, 5, limits, nil)

		var request logproto.PushRequest
		request.Streams = append(request.Streams, tc.req.Streams[0])

		// the error would happen in the ingester mock, it's set to reject SM that has not been sanitized
		response, err := distributors[0].Push(ctx, &request)
		require.NoError(t, err)
		assert.Equal(t, tc.expectedResponse, response)
		assert.Equal(t, tc.numSanitizations, testutil.ToFloat64(distributors[0].m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki)))
	}
}

func TestDistributor_NormalizeStructuredMetadata(t *testing.T) {
	for _, tc := range []struct {
		name              string
		metadata          []logproto.LabelAdapter
		normalizeLevel    bool
		want              labels.Labels
		wantChanged       bool
		wantSanitizations float64
		wantErr           string
	}{
		{
			name: "empty metadata",
			want: labels.EmptyLabels(),
		},
		{
			name:     "valid metadata remains unchanged",
			metadata: buildNestedAttrs("z", "私", "a", "value"),
			want:     labels.FromStrings("a", "value", "z", "私"),
		},
		{
			name:              "invalid name characters",
			metadata:          buildNestedAttrs("service.name", "loki"),
			want:              labels.FromStrings("service_name", "loki"),
			wantChanged:       true,
			wantSanitizations: 1,
		},
		{
			name:              "numeric name gains a prefix",
			metadata:          buildNestedAttrs("1", "value"),
			want:              labels.FromStrings("key_1", "value"),
			wantChanged:       true,
			wantSanitizations: 1,
		},
		{
			name:              "invalid UTF-8 and replacement runes",
			metadata:          buildNestedAttrs("value", "a\xffb�c"),
			want:              labels.FromStrings("value", "a b c"),
			wantChanged:       true,
			wantSanitizations: 1,
		},
		{
			name:              "name and value sanitized separately",
			metadata:          buildNestedAttrs("service.name", "lo�ki"),
			want:              labels.FromStrings("service_name", "lo ki"),
			wantChanged:       true,
			wantSanitizations: 2,
		},
		{
			name:        "empty value removed",
			metadata:    buildNestedAttrs("empty", "", "keep", "v"),
			want:        labels.FromStrings("keep", "v"),
			wantChanged: true,
		},
		{
			name:              "normalized name replaces an existing name",
			metadata:          buildNestedAttrs("a.b", "normalized", "a_b", "original"),
			want:              labels.FromStrings("a_b", "normalized"),
			wantChanged:       true,
			wantSanitizations: 1,
		},
		{
			name:     "level normalization disabled",
			metadata: buildNestedAttrs(constants.LevelLabel, "WARNING"),
			want:     labels.FromStrings(constants.LevelLabel, "WARNING"),
		},
		{
			name:           "level normalization enabled",
			metadata:       buildNestedAttrs(constants.LevelLabel, "WARNING"),
			normalizeLevel: true,
			want:           labels.FromStrings(constants.LevelLabel, "warn"),
			wantChanged:    true,
		},
		{
			name:              "level normalization after name sanitization",
			metadata:          buildNestedAttrs("detected.level", "WARN"),
			normalizeLevel:    true,
			want:              labels.FromStrings(constants.LevelLabel, "warn"),
			wantChanged:       true,
			wantSanitizations: 1,
		},
		{
			name:           "unknown level preserved",
			metadata:       buildNestedAttrs(constants.LevelLabel, "CUSTOM"),
			normalizeLevel: true,
			want:           labels.FromStrings(constants.LevelLabel, "CUSTOM"),
		},
		{
			name:     "empty name rejected",
			metadata: buildNestedAttrs("", "value"),
			want:     labels.EmptyLabels(),
			wantErr:  "label name is empty",
		},
		{
			name:              "invalid name rejected without returning partial results",
			metadata:          buildNestedAttrs("service.name", "loki", "__", "value"),
			want:              labels.EmptyLabels(),
			wantSanitizations: 1,
			wantErr:           "normalization for label name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Distributor{m: newMetrics(prometheus.NewRegistry())}
			before := append([]logproto.LabelAdapter(nil), tc.metadata...)

			got, changed, err := d.normalizeStructuredMetadata(tc.metadata, "test", constants.Loki, tc.normalizeLevel)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.True(t, labels.Equal(tc.want, got), "want %s, got %s", tc.want, got)
			require.Equal(t, tc.wantChanged, changed)
			require.Equal(t, before, tc.metadata, "normalization must not mutate shared input")
			require.Equal(t, tc.wantSanitizations, testutil.ToFloat64(d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki)))
		})
	}
}

func TestDistributor_NormalizeMetadataGroup(t *testing.T) {
	for _, tc := range []struct {
		name               string
		attrs              []logproto.LabelAdapter
		normalizeLevel     bool
		want               []logproto.LabelAdapter
		wantOriginalSize   int
		wantNormalizedSize int
		wantSanitizations  float64
	}{
		{name: "empty group"},
		{
			name:               "unchanged attributes reuse their slice",
			attrs:              buildNestedAttrs("keep", "v"),
			want:               buildNestedAttrs("keep", "v"),
			wantOriginalSize:   5,
			wantNormalizedSize: 5,
		},
		{
			name:               "name prefix increases normalized size",
			attrs:              buildNestedAttrs("1", "v"),
			want:               buildNestedAttrs("key_1", "v"),
			wantOriginalSize:   2,
			wantNormalizedSize: 6,
			wantSanitizations:  1,
		},
		{
			name:               "collisions and empty values reduce normalized size and count",
			attrs:              buildNestedAttrs("a.b", "v", "a_b", "other", "empty", ""),
			want:               buildNestedAttrs("a_b", "v"),
			wantOriginalSize:   17,
			wantNormalizedSize: 4,
			wantSanitizations:  1,
		},
		{
			name:               "detected level is excluded from size accounting",
			attrs:              buildNestedAttrs(constants.LevelLabel, "WARNING"),
			normalizeLevel:     true,
			want:               buildNestedAttrs(constants.LevelLabel, "warn"),
			wantOriginalSize:   0,
			wantNormalizedSize: 0,
		},
		{
			name:               "sanitized detected level is excluded from normalized size",
			attrs:              buildNestedAttrs("detected.level", "WARNING"),
			normalizeLevel:     true,
			want:               buildNestedAttrs(constants.LevelLabel, "warn"),
			wantOriginalSize:   21,
			wantNormalizedSize: 0,
			wantSanitizations:  1,
		},
		{
			name:               "cached group is not normalized again",
			attrs:              buildNestedAttrs(constants.LevelLabel, "WARNING"),
			want:               buildNestedAttrs(constants.LevelLabel, "WARNING"),
			wantOriginalSize:   0,
			wantNormalizedSize: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Distributor{m: newMetrics(prometheus.NewRegistry())}
			attrs := tc.attrs
			before := append([]logproto.LabelAdapter(nil), attrs...)
			group := newMetadataGroup(&attrs)

			require.NoError(t, d.normalizeMetadataGroup(&group, "test", constants.Loki, tc.normalizeLevel))
			require.Equal(t, tc.want, attrs)
			require.Equal(t, before, tc.attrs, "other groups can still reference the original attributes")
			require.True(t, group.normalized)
			require.True(t, labels.Equal(logproto.FromLabelAdaptersToLabels(tc.want), group.labels))
			require.Equal(t, tc.wantOriginalSize, group.originalSize)
			require.Equal(t, len(before), group.originalCount)
			require.Equal(t, tc.wantNormalizedSize, group.normalizedSize)
			if len(attrs) > 0 && labels.Equal(logproto.FromLabelAdaptersToLabels(before), group.labels) {
				require.Same(t, &tc.attrs[0], &attrs[0])
			}

			// Reusing a group must not normalize it again or replace its attributes.
			normalized := attrs
			require.NoError(t, d.normalizeMetadataGroup(&group, "test", constants.Loki, !tc.normalizeLevel))
			require.Equal(t, tc.want, attrs)
			if len(attrs) > 0 {
				require.Same(t, &normalized[0], &attrs[0])
			}
			require.Equal(t, tc.wantSanitizations, testutil.ToFloat64(d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki)))
		})
	}

	t.Run("error leaves group unchanged", func(t *testing.T) {
		d := &Distributor{m: newMetrics(prometheus.NewRegistry())}
		attrs := buildNestedAttrs("service.name", "loki", "__", "invalid")
		before := append([]logproto.LabelAdapter(nil), attrs...)
		group := newMetadataGroup(&attrs)
		beforeGroup := group

		err := d.normalizeMetadataGroup(&group, "test", constants.Loki, true)
		require.ErrorContains(t, err, "normalization for label name")
		require.Equal(t, before, attrs)
		require.Equal(t, beforeGroup, group, "failed normalization must not be cached")
	})
}

func TestDistributor_SharedMetadataNormalization(t *testing.T) {
	for _, discoverLevels := range []bool{false, true} {
		t.Run(fmt.Sprintf("discover-levels=%t", discoverLevels), func(t *testing.T) {
			lim := &validation.Limits{}
			flagext.DefaultValues(lim)
			lim.DiscoverLogLevels = discoverLevels
			lim.DiscoverGenericFields.Fields = map[string][]string{"scope_id": {"scope_name"}, "resource_id": {"key_9resource_name"}}
			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, lim, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
			d := distributors[0]
			resourceAttrs := buildNestedAttrs("9resource.name", "r�s", "detected.level", "WARN")
			scopeAttrs := buildNestedAttrs("scope.name", "scope�")
			resourceBefore := append([]logproto.LabelAdapter(nil), resourceAttrs...)
			scopeBefore := append([]logproto.LabelAdapter(nil), scopeAttrs...)
			at := time.Now()
			entries := func() []logproto.Entry {
				return []logproto.Entry{
					{Timestamp: at, Line: "first", StructuredMetadata: buildNestedAttrs("entry.name", "entry")},
					{Timestamp: at.Add(time.Second), Line: "second", StructuredMetadata: buildNestedAttrs("entry.name", "entry")},
				}
			}
			req := &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{{
				Labels: `{app="shared"}`,
				ResourceLogs: []logproto.ResourceLogs{{Attrs: resourceAttrs, ScopeLogs: []logproto.ScopeLogs{
					{Attrs: scopeAttrs, Entries: entries()},
					{Attrs: scopeAttrs, Entries: entries()},
				}}},
			}}}
			_, err := d.pushWithResolver(ctx, req, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
			require.NoError(t, err)
			require.Equal(t, resourceBefore, resourceAttrs, "resource attributes can be shared with other streams")
			require.Equal(t, scopeBefore, scopeAttrs, "scope attributes can be shared with other groups")

			level := "WARN"
			if discoverLevels {
				level = "warn"
			}
			wantResource := buildNestedAttrs("key_9resource_name", "r s", "detected_level", level)
			wantScope := buildNestedAttrs("scope_name", "scope ")
			wantOwn := buildNestedAttrs("entry_name", "entry", "scope_id", "scope ", "resource_id", "r s")
			resource := req.Streams[0].ResourceLogs[0]
			require.ElementsMatch(t, wantResource, resource.Attrs)
			for _, scope := range resource.ScopeLogs {
				require.ElementsMatch(t, wantScope, scope.Attrs)
				for _, entry := range scope.Entries {
					require.ElementsMatch(t, wantOwn, entry.StructuredMetadata, "shared attributes remain on their groups")
				}
			}
			got := ing.Peek()
			require.NotNil(t, got)
			require.Len(t, got.Streams, 1)
			require.Len(t, got.Streams[0].Entries, 4)
			wantMetadata := append(append(append([]logproto.LabelAdapter(nil), wantOwn...), wantScope...), wantResource...)
			for _, entry := range got.Streams[0].Entries {
				require.ElementsMatch(t, wantMetadata, entry.StructuredMetadata)
			}
			// Three resource changes, two per scope, and one per entry.
			require.Equal(t, float64(11), testutil.ToFloat64(d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki)))
		})
	}
}

func TestDistributor_SharedMetadataNormalizationLimits(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		resourceName, scopeName string
		entryName               string
		maxMetadataSize         int
		burst                   int
		wantErr                 string
	}{
		{name: "validate raw size and meter normalized size", maxMetadataSize: 4, burst: 30},
		{name: "reject raw metadata over the limit", maxMetadataSize: 3, burst: 30, wantErr: "structured metadata too large"},
		{name: "normalized bytes exceed ingestion limit", maxMetadataSize: 4, burst: 29, wantErr: "ingestion rate limit exceeded"},
		{name: "invalid resource name", resourceName: "__", maxMetadataSize: 32, burst: 100, wantErr: "normalization for label name"},
		{name: "invalid scope name", scopeName: "__", maxMetadataSize: 32, burst: 100, wantErr: "normalization for label name"},
		{name: "invalid entry name", entryName: "__", maxMetadataSize: 32, burst: 100, wantErr: "normalization for label name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lim := &validation.Limits{}
			flagext.DefaultValues(lim)
			lim.DiscoverLogLevels = false
			lim.MaxStructuredMetadataSize = loki_flagext.ByteSize(tc.maxMetadataSize)
			lim.IngestionRateMB = float64(tc.burst) / (1024 * 1024)
			lim.IngestionBurstSizeMB = lim.IngestionRateMB
			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, lim, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
			d := distributors[0]
			at := time.Now()
			resourceName, scopeName := tc.resourceName, tc.scopeName
			if resourceName == "" {
				resourceName = "1"
			}
			if scopeName == "" {
				scopeName = "2"
			}
			// Numeric names gain a four-byte prefix; validation must still use their original sizes.
			req := &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{{Labels: `{app="shared"}`, ResourceLogs: []logproto.ResourceLogs{{
				Attrs: buildNestedAttrs(resourceName, "r"),
				ScopeLogs: []logproto.ScopeLogs{
					{Attrs: buildNestedAttrs(scopeName, "s"), Entries: []logproto.Entry{{Timestamp: at, Line: "aaa"}, {Timestamp: at, Line: "bbb"}}},
					{Attrs: buildNestedAttrs("3", "s"), Entries: []logproto.Entry{{Timestamp: at, Line: "ccc"}, {Timestamp: at, Line: "ddd"}}},
				},
			}}}}}
			if tc.entryName != "" {
				req.Streams[0].ResourceLogs[0].ScopeLogs[0].Entries[0].StructuredMetadata = buildNestedAttrs(tc.entryName, "entry")
			}
			_, err := d.pushWithResolver(ctx, req, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, ing.Peek())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, ing.Peek())
			require.Len(t, ing.Peek().Streams[0].Entries, 4)
		})
	}
}

func TestDistributor_SharedMetadataCountAfterNormalization(t *testing.T) {
	for _, tc := range []struct {
		name              string
		attrs, normalized []logproto.LabelAdapter
	}{
		{name: "empty value removed", attrs: buildNestedAttrs("keep", "v", "empty", ""), normalized: buildNestedAttrs("keep", "v")},
		{name: "names collide", attrs: buildNestedAttrs("a.b", "v", "a_b", "v"), normalized: buildNestedAttrs("a_b", "v")},
	} {
		for _, separateScopes := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/separate-scopes=%t", tc.name, separateScopes), func(t *testing.T) {
				lim := &validation.Limits{}
				flagext.DefaultValues(lim)
				lim.DiscoverLogLevels = false
				lim.MaxStructuredMetadataEntriesCount = 2
				ing := &mockIngester{}
				distributors, _ := prepare(t, 1, 3, lim, func(_ string) (ring_client.PoolClient, error) { return ing, nil })
				d := distributors[0]
				at := time.Now()
				first := logproto.Entry{Timestamp: at, Line: "kept"}
				second := logproto.Entry{Timestamp: at, Line: "rejected", StructuredMetadata: buildNestedAttrs("own", "v")}
				scopes := []logproto.ScopeLogs{{Entries: []logproto.Entry{first, second}}}
				if separateScopes {
					scopes = []logproto.ScopeLogs{{Entries: []logproto.Entry{first}}, {Entries: []logproto.Entry{second}}}
				}
				req := &logproto.InternalPushRequest{Streams: []logproto.InternalStreamAdapter{{
					Labels: `{app="shared"}`, ResourceLogs: []logproto.ResourceLogs{{Attrs: tc.attrs, ScopeLogs: scopes}},
				}}}

				// Validation must count all three submitted attributes, even after normalization removes one.
				_, err := d.pushWithResolver(ctx, req, newRequestScopedStreamResolver("test", d.validator.Limits, nil), constants.Loki)
				require.ErrorContains(t, err, "too many structured metadata labels")
				require.Len(t, req.Streams[0].ResourceLogs, 1)
				require.Len(t, req.Streams[0].ResourceLogs[0].ScopeLogs, 1)
				require.Equal(t, tc.normalized, req.Streams[0].ResourceLogs[0].Attrs)
				got := ing.Peek()
				require.NotNil(t, got)
				require.Len(t, got.Streams, 1)
				first.StructuredMetadata = tc.normalized
				require.Equal(t, []logproto.Entry{first}, got.Streams[0].Entries)
			})
		}
	}
}

func BenchmarkDistributor_PushWithPolicies(b *testing.B) {
	baselineLimits := &validation.Limits{}
	flagext.DefaultValues(baselineLimits)
	lbs := `{foo="bar", env="prod", daz="baz", container="loki", pod="loki-0"}`

	b.Run("push without policies", func(b *testing.B) {
		limits := baselineLimits
		limits.PolicyStreamMapping = make(validation.PolicyStreamMapping)
		distributors, _ := prepare(&testing.T{}, 1, 3, limits, nil)
		req := makeWriteRequestWithLabels(10, 10, []string{lbs}, false, false, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			distributors[0].Push(ctx, req) //nolint:errcheck
		}
	})

	for numPolicies := 1; numPolicies <= 100; numPolicies *= 10 {
		b.Run(fmt.Sprintf("push with %d policies", numPolicies), func(b *testing.B) {
			limits := baselineLimits
			limits.PolicyStreamMapping = make(validation.PolicyStreamMapping)
			for i := 1; i <= numPolicies; i++ {
				limits.PolicyStreamMapping[fmt.Sprintf("policy%d", i)] = []*validation.PriorityStream{
					{
						Selector: `{foo="bar"}`, Priority: i,
					},
				}
			}

			req := makeWriteRequestWithLabels(10, 10, []string{lbs}, false, false, false)
			distributors, _ := prepare(&testing.T{}, 1, 3, limits, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				distributors[0].Push(ctx, req) //nolint:errcheck
			}
		})
	}

	for numMatchers := 1; numMatchers <= 100; numMatchers *= 10 {
		b.Run(fmt.Sprintf("push with %d matchers", numMatchers), func(b *testing.B) {
			limits := baselineLimits
			limits.PolicyStreamMapping = make(validation.PolicyStreamMapping)
			for i := 1; i <= numMatchers; i++ {
				limits.PolicyStreamMapping["policy0"] = append(limits.PolicyStreamMapping["policy0"], &validation.PriorityStream{
					Selector: `{foo="bar"}`,
					Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")},
					Priority: i,
				})
			}

			req := makeWriteRequestWithLabels(10, 10, []string{lbs}, false, false, false)
			distributors, _ := prepare(&testing.T{}, 1, 3, limits, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				distributors[0].Push(ctx, req) //nolint:errcheck
			}
		})
	}
}

func TestRequestScopedStreamResolver(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	limits.RetentionPeriod = model.Duration(24 * time.Hour)
	limits.StreamRetention = []validation.StreamRetention{
		{
			Period:   model.Duration(48 * time.Hour),
			Selector: `{env="prod"}`,
		},
	}
	limits.PolicyStreamMapping = validation.PolicyStreamMapping{
		"policy0": []*validation.PriorityStream{
			{
				Selector: `{env="prod"}`,
			},
		},
	}

	// Load matchers
	require.NoError(t, limits.Validate())

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	resolver := newRequestScopedStreamResolver("123", overrides, nil)

	retentionHours := resolver.RetentionHoursFor(labels.FromStrings("env", "prod"))
	require.Equal(t, "48", retentionHours)
	retentionPeriod := resolver.RetentionPeriodFor(labels.FromStrings("env", "prod"))
	require.Equal(t, 48*time.Hour, retentionPeriod)

	retentionHours = resolver.RetentionHoursFor(labels.FromStrings("env", "dev"))
	require.Equal(t, "24", retentionHours)
	retentionPeriod = resolver.RetentionPeriodFor(labels.FromStrings("env", "dev"))
	require.Equal(t, 24*time.Hour, retentionPeriod)

	policy := resolver.PolicyFor(t.Context(), labels.FromStrings("env", "prod"))
	require.Equal(t, "policy0", policy)

	policy = resolver.PolicyFor(t.Context(), labels.FromStrings("env", "dev"))
	require.Empty(t, policy)

	// We now modify the underlying limits to test that the resolver is not affected by changes to the limits
	limits.RetentionPeriod = model.Duration(36 * time.Hour)
	limits.StreamRetention = []validation.StreamRetention{
		{
			Period:   model.Duration(72 * time.Hour),
			Selector: `{env="dev"}`,
		},
	}
	limits.PolicyStreamMapping = validation.PolicyStreamMapping{
		"policy1": []*validation.PriorityStream{
			{
				Selector: `{env="dev"}`,
			},
		},
	}

	// Load matchers
	require.NoError(t, limits.Validate())

	newOverrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	// overwrite the overrides we passed to the resolver by the new ones
	*overrides = *newOverrides

	// All should be the same as before
	retentionHours = resolver.RetentionHoursFor(labels.FromStrings("env", "prod"))
	require.Equal(t, "48", retentionHours)
	retentionPeriod = resolver.RetentionPeriodFor(labels.FromStrings("env", "prod"))
	require.Equal(t, 48*time.Hour, retentionPeriod)

	retentionHours = resolver.RetentionHoursFor(labels.FromStrings("env", "dev"))
	require.Equal(t, "24", retentionHours)
	retentionPeriod = resolver.RetentionPeriodFor(labels.FromStrings("env", "dev"))
	require.Equal(t, 24*time.Hour, retentionPeriod)

	policy = resolver.PolicyFor(t.Context(), labels.FromStrings("env", "prod"))
	require.Equal(t, "policy0", policy)

	policy = resolver.PolicyFor(t.Context(), labels.FromStrings("env", "dev"))
	require.Empty(t, policy)

	// But a new resolver should return the new values
	newResolver := newRequestScopedStreamResolver("123", overrides, nil)

	retentionHours = newResolver.RetentionHoursFor(labels.FromStrings("env", "prod"))
	require.Equal(t, "36", retentionHours)
	retentionPeriod = newResolver.RetentionPeriodFor(labels.FromStrings("env", "prod"))
	require.Equal(t, 36*time.Hour, retentionPeriod)

	retentionHours = newResolver.RetentionHoursFor(labels.FromStrings("env", "dev"))
	require.Equal(t, "72", retentionHours)
	retentionPeriod = newResolver.RetentionPeriodFor(labels.FromStrings("env", "dev"))
	require.Equal(t, 72*time.Hour, retentionPeriod)

	policy = newResolver.PolicyFor(t.Context(), labels.FromStrings("env", "prod"))
	require.Empty(t, policy)

	policy = newResolver.PolicyFor(t.Context(), labels.FromStrings("env", "dev"))
	require.Equal(t, "policy1", policy)
}

func TestDistributor_PushIngestLimits(t *testing.T) {
	tests := []struct {
		name                      string
		ingestLimitsEnabled       bool
		ingestLimitsDryRunEnabled bool
		tenant                    string
		streams                   logproto.PushRequest
		expectedLimitsCalls       uint64
		expectedLimitsRequest     *limitsproto.ExceedsLimitsRequest
		limitsResponse            *limitsproto.ExceedsLimitsResponse
		limitsResponseErr         error
		expectedResponse          *logproto.PushResponse
		expectedErr               string
		expectedDiscardedSamples  float64
		expectedDiscardedBytes    float64
	}{{
		name:                "limits are not checked when disabled",
		ingestLimitsEnabled: false,
		tenant:              "test",
		streams: logproto.PushRequest{
			Streams: []logproto.Stream{{
				Labels: "{foo=\"bar\"}",
			}},
		},
		expectedLimitsCalls:      0,
		expectedResponse:         success,
		expectedDiscardedSamples: 0,
		expectedDiscardedBytes:   0,
	}, {
		name:                "limits are checked",
		ingestLimitsEnabled: true,
		tenant:              "test",
		streams: logproto.PushRequest{
			Streams: []logproto.Stream{{
				Labels: "{foo=\"bar\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "baz",
				}},
			}},
		},
		expectedLimitsCalls: 1,
		expectedLimitsRequest: &limitsproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*limitsproto.StreamMetadata{{
				StreamHash: 0x90eb45def17f924,
				TotalSize:  0x3,
			}},
		},
		limitsResponse: &limitsproto.ExceedsLimitsResponse{
			Results: []*limitsproto.ExceedsLimitsResult{},
		},
		expectedResponse:         success,
		expectedDiscardedSamples: 0,
		expectedDiscardedBytes:   0,
	}, {
		name:                "one of two streams exceed max stream limit, request is accepted",
		ingestLimitsEnabled: true,
		tenant:              "test",
		streams: logproto.PushRequest{
			Streams: []logproto.Stream{{
				Labels: "{foo=\"bar\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "baz",
				}},
			}, {
				Labels: "{bar=\"baz\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "qux",
				}},
			}},
		},
		expectedLimitsCalls: 1,
		expectedLimitsRequest: &limitsproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*limitsproto.StreamMetadata{{
				StreamHash: 0x90eb45def17f924,
				TotalSize:  0x3,
			}, {
				StreamHash: 0x11561609feba8cf6,
				TotalSize:  0x3,
			}},
		},
		limitsResponse: &limitsproto.ExceedsLimitsResponse{
			Results: []*limitsproto.ExceedsLimitsResult{{
				StreamHash: 0x90eb45def17f924,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		// Note: When some streams are rejected, validationErr is set and returned.
		// The request will succeed (streams are written) but validationErr is returned.
		expectedErr:              fmt.Sprintf("rpc error: code = Code(429) desc = %s", fmt.Sprintf(validation.StreamLimitErrorMsg, "{foo=\"bar\"}", "test")),
		expectedResponse:         success, // Response is returned even when some streams are rejected
		expectedDiscardedSamples: 1,       // 1 entry from "{foo=\"bar\"}" stream is discarded
		expectedDiscardedBytes:   3,       // "baz" = 3 bytes
	}, {
		name:                "all streams exceed max stream limit, request is rejected",
		ingestLimitsEnabled: true,
		tenant:              "test",
		streams: logproto.PushRequest{
			Streams: []logproto.Stream{{
				Labels: "{foo=\"bar\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "baz",
				}},
			}, {
				Labels: "{bar=\"baz\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "qux",
				}},
			}},
		},
		expectedLimitsCalls: 1,
		expectedLimitsRequest: &limitsproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*limitsproto.StreamMetadata{{
				StreamHash: 0x90eb45def17f924,
				TotalSize:  0x3,
			}, {
				StreamHash: 0x11561609feba8cf6,
				TotalSize:  0x3,
			}},
		},
		limitsResponse: &limitsproto.ExceedsLimitsResponse{
			Results: []*limitsproto.ExceedsLimitsResult{{
				StreamHash: 0x90eb45def17f924,
				Reason:     uint32(limits.ReasonMaxStreams),
			}, {
				StreamHash: 0x11561609feba8cf6,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expectedErr:              fmt.Sprintf("rpc error: code = Code(429) desc = %s", fmt.Sprintf(validation.StreamLimitErrorMsg, "{foo=\"bar\"}", "test")),
		expectedResponse:         nil, // Early return when all streams are rejected
		expectedDiscardedSamples: 2,   // 2 entries (1 from each stream) are discarded
		expectedDiscardedBytes:   6,   // "baz" (3 bytes) + "qux" (3 bytes) = 6 bytes
	}, {
		name:                      "dry-run does not enforce limits",
		ingestLimitsEnabled:       true,
		ingestLimitsDryRunEnabled: true,
		tenant:                    "test",
		streams: logproto.PushRequest{
			Streams: []logproto.Stream{{
				Labels: "{foo=\"bar\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "baz",
				}},
			}},
		},
		expectedLimitsCalls: 1,
		expectedLimitsRequest: &limitsproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*limitsproto.StreamMetadata{{
				StreamHash: 0x90eb45def17f924,
				TotalSize:  0x3,
			}},
		},
		limitsResponse: &limitsproto.ExceedsLimitsResponse{
			Results: []*limitsproto.ExceedsLimitsResult{{
				StreamHash: 1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expectedResponse:         success, // Dry-run doesn't enforce, so request succeeds
		expectedDiscardedSamples: 0,       // Dry-run doesn't track discarded data
		expectedDiscardedBytes:   0,
	}, {
		name:                "error checking limits",
		ingestLimitsEnabled: true,
		tenant:              "test",
		streams: logproto.PushRequest{
			Streams: []logproto.Stream{{
				Labels: "{foo=\"bar\"}",
				Entries: []logproto.Entry{{
					Timestamp: time.Now(),
					Line:      "baz",
				}},
			}},
		},
		expectedLimitsCalls: 1,
		expectedLimitsRequest: &limitsproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*limitsproto.StreamMetadata{{
				StreamHash: 0x90eb45def17f924,
				TotalSize:  0x3,
			}},
		},
		limitsResponseErr:        errors.New("failed to check limits"),
		expectedResponse:         success, // When EnforceLimits returns error, request continues
		expectedDiscardedSamples: 0,       // When EnforceLimits errors, streams are accepted
		expectedDiscardedBytes:   0,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Reset metrics before each test
			validation.DiscardedSamples.Reset()
			validation.DiscardedBytes.Reset()

			validationLimits := &validation.Limits{}
			flagext.DefaultValues(validationLimits)
			distributors, _ := prepare(t, 1, 3, validationLimits, nil)
			d := distributors[0]
			d.cfg.IngestLimitsEnabled = test.ingestLimitsEnabled
			d.cfg.IngestLimitsDryRunEnabled = test.ingestLimitsDryRunEnabled

			mockClient := mockIngestLimitsFrontendClient{
				t:                            t,
				expectedExceedsLimitsRequest: test.expectedLimitsRequest,
				exceedsLimitsResponse:        test.limitsResponse,
				exceedsLimitsResponseErr:     test.limitsResponseErr,
			}
			l := newIngestLimits(&mockClient, prometheus.NewRegistry())
			d.ingestLimits = l

			ctx = user.InjectOrgID(context.Background(), test.tenant)
			resp, err := d.Push(ctx, &test.streams)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
			} else {
				require.Nil(t, err)
			}
			if test.expectedResponse == nil {
				require.Nil(t, resp)
			} else {
				require.Equal(t, test.expectedResponse, resp)
			}
			require.Equal(t, test.expectedLimitsCalls, mockClient.calls.Load())

			// Note, the ToFloat64 panics if it doesn't find exactly one metric, if you are debugging a panic here
			// you might need to check that both of these metrics were updated when the validation failure happened.
			if test.expectedDiscardedSamples > 0 || test.expectedDiscardedBytes > 0 {
				discardedSamples := testutil.ToFloat64(validation.DiscardedSamples)
				discardedBytes := testutil.ToFloat64(validation.DiscardedBytes)

				assert.Equal(t, test.expectedDiscardedSamples, discardedSamples, "DiscardedSamples should match expected value")
				assert.Equal(t, test.expectedDiscardedBytes, discardedBytes, "DiscardedBytes should match expected value")
			}
		})
	}
}

func TestDistributorMaxInflightBytesLimit(t *testing.T) {
	validationLimits := &validation.Limits{}
	flagext.DefaultValues(validationLimits)
	distributors, _ := prepare(t, 1, 3, validationLimits, nil)
	d := distributors[0]
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{{
			Labels: "{foo=\"bar\"}",
			Entries: []logproto.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("a", 1025),
			}},
		}},
	}
	_, err := d.Push(ctx, req)
	require.NoError(t, err)
	// Set the max inflight bytes to 1KB, the same request should be rejected.
	d.cfg.MaxInflightBytes = 1024
	_, err = d.Push(ctx, req)
	require.ErrorIs(t, err, errServiceUnavailableMaxLoad)

}

func ptr[T any](v T) *T { return &v }
