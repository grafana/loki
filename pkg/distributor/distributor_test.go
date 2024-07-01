package distributor

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util/constants"
	fe "github.com/grafana/loki/v3/pkg/util/flagext"
	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	loki_net "github.com/grafana/loki/v3/pkg/util/net"
	"github.com/grafana/loki/v3/pkg/util/test"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	success = &logproto.PushResponse{}
	ctx     = user.InjectOrgID(context.Background(), "test")
)

func TestDistributor(t *testing.T) {
	ingestionRateLimit := 0.000096 // 100 Bytes/s limit

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
			expectedErrors: []error{httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 100, 1000)},
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
			limits.DiscoverServiceName = nil
			limits.IngestionRateMB = ingestionRateLimit
			limits.IngestionBurstSizeMB = ingestionRateLimit
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

			distributors, _ := prepare(t, 1, 5, limits, nil)

			var request logproto.PushRequest
			for i := 0; i < tc.streams; i++ {
				req := makeWriteRequest(tc.lines, 10)
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
						assert.Equal(t, err, expectedError)
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

func Test_IncrementTimestamp(t *testing.T) {
	incrementingDisabled := &validation.Limits{}
	flagext.DefaultValues(incrementingDisabled)
	incrementingDisabled.DiscoverServiceName = nil
	incrementingDisabled.RejectOldSamples = false
	incrementingDisabled.DiscoverLogLevels = false

	incrementingEnabled := &validation.Limits{}
	flagext.DefaultValues(incrementingEnabled)
	incrementingEnabled.DiscoverServiceName = nil
	incrementingEnabled.RejectOldSamples = false
	incrementingEnabled.IncrementDuplicateTimestamp = true
	incrementingEnabled.DiscoverLogLevels = false

	defaultLimits := &validation.Limits{}
	flagext.DefaultValues(defaultLimits)
	now := time.Now()
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
		"default limit adding service_name label": {
			limits: defaultLimits,
			push: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\"}",
						Entries: []logproto.Entry{
							{Timestamp: now.Add(-2 * time.Second), Line: "hey1"},
							{Timestamp: now.Add(-time.Second), Line: "hey2"},
							{Timestamp: now, Line: "hey3"},
						},
					},
				},
			},
			expectedPush: &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: "{job=\"foo\", service_name=\"foo\"}",
						Hash:   0x86ca305b6d86e8b0,
						Entries: []logproto.Entry{
							{Timestamp: now.Add(-2 * time.Second), Line: "hey1"},
							{Timestamp: now.Add(-time.Second), Line: "hey2"},
							{Timestamp: now, Line: "hey3"},
						},
					},
				},
			},
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, testData.limits, func(addr string) (ring_client.PoolClient, error) { return ing, nil })
			_, err := distributors[0].Push(ctx, testData.push)
			assert.NoError(t, err)
			topVal := ing.Peek()
			assert.Equal(t, testData.expectedPush, topVal)
		})
	}
}

func TestDistributorPushConcurrently(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.DiscoverServiceName = nil

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
				},
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

func Test_SortLabelsOnPush(t *testing.T) {
	t.Run("with service_name already present in labels", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		ingester := &mockIngester{}
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		request := makeWriteRequest(10, 10)
		request.Streams[0].Labels = `{buzz="f", service_name="foo", a="b"}`
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{a="b", buzz="f", service_name="foo"}`, topVal.Streams[0].Labels)
	})

	t.Run("with service_name added during ingestion", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		ingester := &mockIngester{}
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		request := makeWriteRequest(10, 10)
		request.Streams[0].Labels = `{buzz="f", x="y", a="b"}`
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{a="b", buzz="f", service_name="unknown_service", x="y"}`, topVal.Streams[0].Labels)
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
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Len(t, topVal.Streams[0].Entries[0].Line, 5)
	})
}

func TestStreamShard(t *testing.T) {
	// setup base stream.
	baseStream := logproto.Stream{}
	baseLabels := "{app='myapp'}"
	lbs, err := syntax.ParseLabels(baseLabels)
	require.NoError(t, err)
	baseStream.Hash = lbs.Hash()
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
				rateStore:        &fakeRateStore{pushRate: 1},
				validator:        validator,
				streamShardCount: prometheus.NewCounter(prometheus.CounterOpts{}),
				shardTracker:     NewShardTracker(),
			}

			derivedStreams := d.shardStream(baseStream, tc.streamSize, "fake")
			require.Len(t, derivedStreams, tc.wantDerivedStreamSize)

			for _, s := range derivedStreams {
				// Generate sorted labels
				lbls, err := syntax.ParseLabels(s.Stream.Labels)
				require.NoError(t, err)

				require.Equal(t, lbls.Hash(), s.Stream.Hash)
				require.Equal(t, lbls.String(), s.Stream.Labels)
			}
		})
	}
}

func TestStreamShardAcrossCalls(t *testing.T) {
	// setup base stream.
	baseStream := logproto.Stream{}
	baseLabels := "{app='myapp'}"
	lbs, err := syntax.ParseLabels(baseLabels)
	require.NoError(t, err)
	baseStream.Hash = lbs.Hash()
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
			rateStore:        &fakeRateStore{pushRate: 1},
			validator:        validator,
			streamShardCount: prometheus.NewCounter(prometheus.CounterOpts{}),
			shardTracker:     NewShardTracker(),
		}

		derivedStreams := d.shardStream(baseStream, streamRate, "fake")
		require.Len(t, derivedStreams, 2)

		for i, s := range derivedStreams {
			require.Len(t, s.Stream.Entries, 1)
			lbls, err := syntax.ParseLabels(s.Stream.Labels)
			require.NoError(t, err)

			require.Equal(t, lbls[0].Value, fmt.Sprint(i))
		}

		derivedStreams = d.shardStream(baseStream, streamRate, "fake")
		require.Len(t, derivedStreams, 2)

		for i, s := range derivedStreams {
			require.Len(t, s.Stream.Entries, 1)
			lbls, err := syntax.ParseLabels(s.Stream.Labels)
			require.NoError(t, err)

			require.Equal(t, lbls[0].Value, fmt.Sprint(i+2))
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
	labels := "{app='myapp', job='fizzbuzz'}"
	lbs, err := syntax.ParseLabels(labels)
	require.NoError(b, err)
	stream.Hash = lbs.Hash()
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
			validator:        validator,
			streamShardCount: prometheus.NewCounter(prometheus.CounterOpts{}),
			shardTracker:     NewShardTracker(),
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
			d.shardStream(stream, 0, "fake") //nolint:errcheck
		}
	})

	b.Run("low number of entries, low number of shards", func(b *testing.B) {
		d := distributorBuilder(2)
		stream.Entries = nil

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, 0, "fake") //nolint:errcheck
		}
	})

	b.Run("high number of entries, high number of shards", func(b *testing.B) {
		d := distributorBuilder(64)
		stream.Entries = allEntries

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, 0, "fake") //nolint:errcheck
		}
	})

	b.Run("low number of entries, high number of shards", func(b *testing.B) {
		d := distributorBuilder(64)
		stream.Entries = nil

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, 0, "fake") //nolint:errcheck
		}
	})
}

func Benchmark_SortLabelsOnPush(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	d := distributors[0]
	request := makeWriteRequest(10, 10)
	vCtx := d.validator.getValidationContextForTime(testTime, "123")
	for n := 0; n < b.N; n++ {
		stream := request.Streams[0]
		stream.Labels = `{buzz="f", a="b"}`
		_, _, _, err := d.parseStreamLabels(vCtx, stream.Labels, stream)
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
			name: "service name label mapping disabled",
			generateLimits: func() *validation.Limits {
				limits := &validation.Limits{}
				flagext.DefaultValues(limits)
				limits.DiscoverServiceName = nil
				return limits
			},
			origLabels: `{foo="bar"}`,
			expectedLabels: labels.Labels{
				{
					Name:  "foo",
					Value: "bar",
				},
			},
		},
		{
			name: "no labels defined - service name label mapping disabled",
			generateLimits: func() *validation.Limits {
				limits := &validation.Limits{}
				flagext.DefaultValues(limits)
				limits.DiscoverServiceName = nil
				return limits
			},
			origLabels:  `{}`,
			expectedErr: fmt.Errorf(validation.MissingLabelsErrorMsg),
		},
		{
			name:       "service name label enabled",
			origLabels: `{foo="bar"}`,
			generateLimits: func() *validation.Limits {
				return defaultLimit
			},
			expectedLabels: labels.Labels{
				{
					Name:  "foo",
					Value: "bar",
				},
				{
					Name:  labelServiceName,
					Value: serviceUnknown,
				},
			},
		},
		{
			name:       "service name label should not get counted against max labels count",
			origLabels: `{foo="bar"}`,
			generateLimits: func() *validation.Limits {
				limits := &validation.Limits{}
				flagext.DefaultValues(limits)
				limits.MaxLabelNamesPerSeries = 1
				return limits
			},
			expectedLabels: labels.Labels{
				{
					Name:  "foo",
					Value: "bar",
				},
				{
					Name:  labelServiceName,
					Value: serviceUnknown,
				},
			},
		},
		{
			name:       "use label service as service name",
			origLabels: `{container="nginx", foo="bar", service="auth"}`,
			generateLimits: func() *validation.Limits {
				return defaultLimit
			},
			expectedLabels: labels.Labels{
				{
					Name:  "container",
					Value: "nginx",
				},
				{
					Name:  "foo",
					Value: "bar",
				},
				{
					Name:  "service",
					Value: "auth",
				},
				{
					Name:  labelServiceName,
					Value: "auth",
				},
			},
		},
	} {
		limits := tc.generateLimits()
		distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
		d := distributors[0]

		vCtx := d.validator.getValidationContextForTime(testTime, "123")

		t.Run(tc.name, func(t *testing.T) {
			lbs, lbsString, hash, err := d.parseStreamLabels(vCtx, tc.origLabels, logproto.Stream{
				Labels: tc.origLabels,
			})
			if tc.expectedErr != nil {
				require.Equal(t, tc.expectedErr, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedLabels.String(), lbsString)
			require.Equal(t, tc.expectedLabels, lbs)
			require.Equal(t, tc.expectedLabels.Hash(), hash)
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
			got := d.shardCountFor(util_log.Logger, tc.stream, tc.pushSize, "fake", limits.ShardStreams)
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
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  10 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 5, expectedError: nil},
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 10, 1, 6)},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 10, 1, 1)},
			},
		},
		"global strategy: limit should be evenly shared across distributors": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  5 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 3, expectedError: nil},
				{bytes: 3, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 3)},
				{bytes: 2, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 1)},
			},
		},
		"global strategy: burst should set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  20 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 15, expectedError: nil},
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 6)},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 1)},
			},
		},
	}

	for testName, testData := range tests {
		testData := testData

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
					assert.Equal(t, success, response)
					assert.Nil(t, err)
				} else {
					assert.Nil(t, response)
					assert.Equal(t, push.expectedError, err)
				}
			}
		})
	}
}

func prepare(t *testing.T, numDistributors, numIngesters int, limits *validation.Limits, factory func(addr string) (ring_client.PoolClient, error)) ([]*Distributor, []mockIngester) {
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

	test.Poll(t, time.Second, numIngesters, func() interface{} {
		return ingestersRing.InstancesCount()
	})

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

		d, err := New(distributorConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, overrides, prometheus.NewPedanticRegistry(), constants.Loki, nil, nil, log.NewNopLogger())
		require.NoError(t, err)
		require.NoError(t, services.StartAndAwaitRunning(context.Background(), d))
		distributors[i] = d
	}

	if distributors[0].distributorsLifecycler != nil {
		test.Poll(t, time.Second, numDistributors, func() interface{} {
			return distributors[0].HealthyInstancesCount()
		})
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

func makeWriteRequestWithLabelsWithLevel(lines, size int, labels []string, level string) *logproto.PushRequest {
	streams := make([]logproto.Stream, len(labels))
	for i := 0; i < len(labels); i++ {
		stream := logproto.Stream{Labels: labels[i]}

		for j := 0; j < lines; j++ {
			// Construct the log line, honoring the input size
			line := "msg=" + strconv.Itoa(j) + strings.Repeat("0", size) + " severity=" + level

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

func makeWriteRequestWithLabels(lines, size int, labels []string) *logproto.PushRequest {
	streams := make([]logproto.Stream, len(labels))
	for i := 0; i < len(labels); i++ {
		stream := logproto.Stream{Labels: labels[i]}

		for j := 0; j < lines; j++ {
			// Construct the log line, honoring the input size
			line := strconv.Itoa(j) + strings.Repeat("0", size)
			line = line[:size]

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

func makeWriteRequest(lines, size int) *logproto.PushRequest {
	return makeWriteRequestWithLabels(lines, size, []string{`{foo="bar"}`})
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

	i.mu.Lock()
	defer i.mu.Unlock()

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

func (mt *mockTee) Duplicate(tenant string, streams []KeyedStream) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.duplicated = append(mt.duplicated, streams)
	mt.tenant = tenant
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
			assert.Equal(t, tee.duplicated[i][j].Stream.Entries, streams.Entries)
		}

		require.Equal(t, "test", tee.tenant)
	}
}

func Test_DetectLogLevels(t *testing.T) {
	setup := func(discoverLogLevels bool) (*validation.Limits, *mockIngester) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)

		limits.DiscoverLogLevels = discoverLogLevels
		limits.DiscoverServiceName = nil
		limits.AllowStructuredMetadata = true
		return limits, &mockIngester{}
	}

	t.Run("log level detection disabled", func(t *testing.T) {
		limits, ingester := setup(false)
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`})
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		require.Len(t, topVal.Streams[0].Entries[0].StructuredMetadata, 0)
	})

	t.Run("log level detection enabled but level cannot be detected", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`})
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		require.Len(t, topVal.Streams[0].Entries[0].StructuredMetadata, 0)
	})

	t.Run("log level detection enabled and warn logs", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabelsWithLevel(1, 10, []string{`{foo="bar"}`}, "warn")
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		require.Equal(t, push.LabelsAdapter{
			{
				Name:  levelLabel,
				Value: logLevelWarn,
			},
		}, topVal.Streams[0].Entries[0].StructuredMetadata)
	})

	t.Run("log level detection enabled but log level already present in stream", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar", level="debug"}`})
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar", level="debug"}`, topVal.Streams[0].Labels)
		sm := topVal.Streams[0].Entries[0].StructuredMetadata
		require.Len(t, sm, 1)
		require.Equal(t, sm[0].Name, levelLabel)
		require.Equal(t, sm[0].Value, logLevelDebug)
	})

	t.Run("log level detection enabled but log level already present as structured metadata", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`})
		writeReq.Streams[0].Entries[0].StructuredMetadata = push.LabelsAdapter{
			{
				Name:  "severity",
				Value: logLevelWarn,
			},
		}
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		sm := topVal.Streams[0].Entries[0].StructuredMetadata
		require.Equal(t, push.LabelsAdapter{
			{
				Name:  "severity",
				Value: logLevelWarn,
			}, {
				Name:  levelLabel,
				Value: logLevelWarn,
			},
		}, sm)
	})
}

func Test_detectLogLevelFromLogEntry(t *testing.T) {
	for _, tc := range []struct {
		name             string
		entry            logproto.Entry
		expectedLogLevel string
	}{
		{
			name: "use severity number from otlp logs",
			entry: logproto.Entry{
				Line: "error",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  loghttp_push.OTLPSeverityNumber,
						Value: fmt.Sprintf("%d", plog.SeverityNumberDebug3),
					},
				},
			},
			expectedLogLevel: logLevelDebug,
		},
		{
			name: "invalid severity number should not cause any issues",
			entry: logproto.Entry{
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  loghttp_push.OTLPSeverityNumber,
						Value: "foo",
					},
				},
			},
			expectedLogLevel: logLevelInfo,
		},
		{
			name: "non otlp without any of the log level keywords in log line",
			entry: logproto.Entry{
				Line: "foo",
			},
			expectedLogLevel: logLevelUnknown,
		},
		{
			name: "non otlp with log level keywords in log line",
			entry: logproto.Entry{
				Line: "this is a warning log",
			},
			expectedLogLevel: logLevelWarn,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword error but it should not get picked up","level":"critical"}`,
			},
			expectedLogLevel: logLevelCritical,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"FOO":"bar","MSG":"message with keyword error but it should not get picked up","LEVEL":"Critical"}`,
			},
			expectedLogLevel: logLevelCritical,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","level":"warn"}`,
			},
			expectedLogLevel: logLevelWarn,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","SEVERITY":"FATAL"}`,
			},
			expectedLogLevel: logLevelFatal,
		},
		{
			name: "json log line with an error in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","level":"ERR"}`,
			},
			expectedLogLevel: logLevelError,
		},
		{
			name: "json log line with an INFO in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword INFO get picked up"}`,
			},
			expectedLogLevel: logLevelInfo,
		},
		{
			name: "logfmt log line with an INFO and not level returns info log level",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with info and not level should get picked up"`,
			},
			expectedLogLevel: logLevelInfo,
		},
		{
			name: "logfmt log line with a warn",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=warn`,
			},
			expectedLogLevel: logLevelWarn,
		},
		{
			name: "logfmt log line with a warn with camel case",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=Warn`,
			},
			expectedLogLevel: logLevelWarn,
		},
		{
			name: "logfmt log line with a trace",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=Trace`,
			},
			expectedLogLevel: logLevelTrace,
		},
		{
			name: "logfmt log line with some other level returns unknown log level",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword but it should not get picked up" level=NA`,
			},
			expectedLogLevel: logLevelUnknown,
		},
		{
			name: "logfmt log line with label Severity is allowed for level detection",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword but it should not get picked up" severity=critical`,
			},
			expectedLogLevel: logLevelCritical,
		},
		{
			name: "logfmt log line with label Severity with camelcase is allowed for level detection",
			entry: logproto.Entry{
				Line: `Foo=bar MSG="Message with keyword but it should not get picked up" Severity=critical`,
			},
			expectedLogLevel: logLevelCritical,
		},
		{
			name: "logfmt log line with a info with non standard case",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=inFO`,
			},
			expectedLogLevel: logLevelInfo,
		},
		{
			name: "logfmt log line with a info with non block case for level",
			entry: logproto.Entry{
				Line: `FOO=bar MSG="message with keyword error but it should not get picked up" LEVEL=inFO`,
			},
			expectedLogLevel: logLevelInfo,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detectedLogLevel := detectLogLevelFromLogEntry(tc.entry, logproto.FromLabelAdaptersToLabels(tc.entry.StructuredMetadata))
			require.Equal(t, tc.expectedLogLevel, detectedLogLevel)
		})
	}
}

func Benchmark_extractLogLevelFromLogLine(b *testing.B) {
	// looks scary, but it is some random text of about 1000 chars from charset a-zA-Z0-9
	logLine := "dGzJ6rKk Zj U04SWEqEK4Uwho8 DpNyLz0 Nfs61HJ fz5iKVigg 44 kabOz7ghviGmVONriAdz4lA 7Kis1OTvZGT3 " +
		"ZB6ioK4fgJLbzm AuIcbnDZKx3rZ aeZJQzRb3zhrn vok8Efav6cbyzbRUQ PYsEdQxCpdCDcGNsKG FVwe61 nhF06t9hXSNySEWa " +
		"gBAXP1J8oEL grep1LfeKjA23ntszKA A772vNyxjQF SjWfJypwI7scxk oLlqRzrDl ostO4CCwx01wDB7Utk0 64A7p5eQDITE6zc3 " +
		"rGL DrPnD K2oj Vro2JEvI2YScstnMx SVu H o GUl8fxZJJ1HY0 C  QOA HNJr5XtsCNRrLi 0w C0Pd8XWbVZyQkSlsRm zFw1lW  " +
		"c8j6JFQuQnnB EyL20z0 2Duo0dvynnAGD 45ut2Z Jrz8Nd7Pmg 5oQ09r9vnmy U2 mKHO5uBfndPnbjbr  mzOvQs9bM1 9e " +
		"yvNSfcbPyhuWvB VKJt2kp8IoTVc XCe Uva5mp9NrGh3TEbjQu1 C  Zvdk uPr7St2m kwwMRcS9eC aS6ZuL48eoQUiKo VBPd4m49ymr " +
		"eQZ0fbjWpj6qA A6rYs4E 58dqh9ntu8baziDJ4c 1q6aVEig YrMXTF hahrlt 6hKVHfZLFZ V 9hEVN0WKgcpu6L zLxo6YC57 XQyfAGpFM " +
		"Wm3 S7if5qCXPzvuMZ2 gNHdst Z39s9uNc58QBDeYRW umyIF BDqEdqhE tAs2gidkqee3aux8b NLDb7 ZZLekc0cQZ GUKQuBg2pL2y1S " +
		"RJtBuW ABOqQHLSlNuUw ZlM2nGS2 jwA7cXEOJhY 3oPv4gGAz  Uqdre16MF92C06jOH dayqTCK8XmIilT uvgywFSfNadYvRDQa " +
		"iUbswJNcwqcr6huw LAGrZS8NGlqqzcD2wFU rm Uqcrh3TKLUCkfkwLm  5CIQbxMCUz boBrEHxvCBrUo YJoF2iyif4xq3q yk "

	for i := 0; i < b.N; i++ {
		level := extractLogLevelFromLogLine(logLine)
		require.Equal(b, logLevelUnknown, level)
	}
}

func Benchmark_optParseExtractLogLevelFromLogLineJson(b *testing.B) {
	logLine := `{"msg": "something" , "level": "error", "id": "1"}`

	for i := 0; i < b.N; i++ {
		level := extractLogLevelFromLogLine(logLine)
		require.Equal(b, logLevelError, level)
	}
}

func Benchmark_optParseExtractLogLevelFromLogLineLogfmt(b *testing.B) {
	logLine := `FOO=bar MSG="message with keyword error but it should not get picked up" LEVEL=inFO`

	for i := 0; i < b.N; i++ {
		level := extractLogLevelFromLogLine(logLine)
		require.Equal(b, logLevelInfo, level)
	}
}
