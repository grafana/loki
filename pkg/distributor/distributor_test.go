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
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/runtime"
	fe "github.com/grafana/loki/pkg/util/flagext"
	loki_flagext "github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
	loki_net "github.com/grafana/loki/pkg/util/net"
	"github.com/grafana/loki/pkg/util/test"
	"github.com/grafana/loki/pkg/validation"
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
		mangleLabels     bool
		expectedResponse *logproto.PushResponse
		expectedError    error
	}{
		{
			lines:            10,
			expectedResponse: success,
		},
		{
			lines:         100,
			expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 100, 1000),
		},
		{
			lines:            100,
			maxLineSize:      1,
			expectedResponse: success,
			expectedError:    httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg, 1, "{foo=\"bar\"}", 10),
		},
		{
			lines:            100,
			mangleLabels:     true,
			expectedResponse: success,
			expectedError:    httpgrpc.Errorf(http.StatusBadRequest, validation.InvalidLabelsErrorMsg, "{ab\"", "1:4: parse error: unterminated quoted string"),
		},
	} {
		t.Run(fmt.Sprintf("[%d](lines=%v)", i, tc.lines), func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.IngestionRateMB = ingestionRateLimit
			limits.IngestionBurstSizeMB = ingestionRateLimit
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

			distributors, _ := prepare(t, 1, 5, limits, nil)

			request := makeWriteRequest(tc.lines, 10)

			if tc.mangleLabels {
				request.Streams[0].Labels = `{ab"`
			}

			response, err := distributors[i%len(distributors)].Push(ctx, request)
			assert.Equal(t, tc.expectedResponse, response)
			assert.Equal(t, tc.expectedError, err)
		})
	}
}

func Test_IncrementTimestamp(t *testing.T) {
	incrementingDisabled := &validation.Limits{}
	flagext.DefaultValues(incrementingDisabled)
	incrementingDisabled.RejectOldSamples = false

	incrementingEnabled := &validation.Limits{}
	flagext.DefaultValues(incrementingEnabled)
	incrementingEnabled.RejectOldSamples = false
	incrementingEnabled.IncrementDuplicateTimestamp = true

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
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			ing := &mockIngester{}
			distributors, _ := prepare(t, 1, 3, testData.limits, func(addr string) (ring_client.PoolClient, error) { return ing, nil })
			_, err := distributors[0].Push(ctx, testData.push)
			assert.NoError(t, err)
			assert.Equal(t, testData.expectedPush, ing.pushed[0])
		})
	}
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
		pushed := ingesters[i].pushed
		counter = counter + len(pushed)
		for _, pr := range pushed {
			for _, st := range pr.Streams {
				labels[st.Labels] = labels[st.Labels] + 1
			}
		}
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
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	ingester := &mockIngester{}
	distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

	request := makeWriteRequest(10, 10)
	request.Streams[0].Labels = `{buzz="f", a="b"}`
	_, err := distributors[0].Push(ctx, request)
	require.NoError(t, err)
	require.Equal(t, `{a="b", buzz="f"}`, ingester.pushed[0].Streams[0].Labels)
}

func Test_TruncateLogLines(t *testing.T) {
	setup := func() (*validation.Limits, *mockIngester) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)

		limits.EnforceMetricName = false
		limits.MaxLineSize = 5
		limits.MaxLineSizeTruncate = true
		return limits, &mockIngester{}
	}

	t.Run("it truncates lines to MaxLineSize when MaxLineSizeTruncate is true", func(t *testing.T) {
		limits, ingester := setup()
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		require.Len(t, ingester.pushed[0].Streams[0].Entries[0].Line, 5)
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

			validator, err := NewValidator(overrides)
			require.NoError(t, err)

			d := Distributor{
				rateStore:        &fakeRateStore{},
				validator:        validator,
				streamShardCount: prometheus.NewCounter(prometheus.CounterOpts{}),
				shardTracker:     NewShardTracker(),
			}

			_, derivedStreams := d.shardStream(baseStream, tc.streamSize, "fake")
			require.Len(t, derivedStreams, tc.wantDerivedStreamSize)

			for _, s := range derivedStreams {
				// Generate sorted labels
				lbls, err := syntax.ParseLabels(s.stream.Labels)
				require.NoError(t, err)

				require.Equal(t, lbls.Hash(), s.stream.Hash)
				require.Equal(t, lbls.String(), s.stream.Labels)
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

	validator, err := NewValidator(overrides)
	require.NoError(t, err)

	t.Run("it generates 4 shards across 2 calls when calculated shards = 2 * entries per call", func(t *testing.T) {
		d := Distributor{
			rateStore:        &fakeRateStore{},
			validator:        validator,
			streamShardCount: prometheus.NewCounter(prometheus.CounterOpts{}),
			shardTracker:     NewShardTracker(),
		}

		_, derivedStreams := d.shardStream(baseStream, streamRate, "fake")
		require.Len(t, derivedStreams, 2)

		for i, s := range derivedStreams {
			require.Len(t, s.stream.Entries, 1)
			lbls, err := syntax.ParseLabels(s.stream.Labels)
			require.NoError(t, err)

			require.Equal(t, lbls[0].Value, fmt.Sprint(i))
		}

		_, derivedStreams = d.shardStream(baseStream, streamRate, "fake")
		require.Len(t, derivedStreams, 2)

		for i, s := range derivedStreams {
			require.Len(t, s.stream.Entries, 1)
			lbls, err := syntax.ParseLabels(s.stream.Labels)
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

	validator, err := NewValidator(overrides)
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
	limits.EnforceMetricName = false
	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	d := distributors[0]
	request := makeWriteRequest(10, 10)
	vCtx := d.validator.getValidationContextForTime(testTime, "123")
	for n := 0; n < b.N; n++ {
		stream := request.Streams[0]
		stream.Labels = `{buzz="f", a="b"}`
		_, _, err := d.parseStreamLabels(vCtx, stream.Labels, &stream)
		if err != nil {
			panic("parseStreamLabels fail,err:" + err.Error())
		}
	}
}

func Benchmark_Push(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.IngestionBurstSizeMB = math.MaxInt32
	limits.CardinalityLimit = math.MaxInt32
	limits.IngestionRateMB = math.MaxInt32
	limits.EnforceMetricName = false
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
		desiredRate loki_flagext.ByteSize

		wantStreamSize int // used for sanity check.
		wantShards     int
		wantErr        bool
	}{
		{
			name:           "2 entries with zero rate and desired rate == 0, return 1 shard",
			stream:         &logproto.Stream{Hash: 1},
			rate:           0,
			desiredRate:    0, // in bytes
			wantStreamSize: 2, // in bytes
			wantShards:     1,
			wantErr:        false,
		},
		{
			// although in this scenario we have enough size to be sharded, we can't divide the number of entries between the ingesters
			// because the number of entries is lower than the number of shards.
			name:           "not enough entries to be sharded, stream size (2b) + ingested rate (0b) < 3b = 1 shard but 0 entries",
			stream:         &logproto.Stream{Hash: 1, Entries: []logproto.Entry{{Line: "abcde"}}},
			rate:           0,
			desiredRate:    3, // in bytes
			wantStreamSize: 2, // in bytes
			wantShards:     1,
			wantErr:        true,
		},
		{
			name:           "not enough data to be sharded, stream size (18b) + ingested rate (0b) < 20b",
			stream:         &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}}},
			rate:           0,
			desiredRate:    20, // in bytes
			wantStreamSize: 18, // in bytes
			wantShards:     1,
			wantErr:        false,
		},
		{
			name:           "enough data to have two shards, stream size (36b) + ingested rate (24b) > 40b",
			stream:         &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:           24, // in bytes
			desiredRate:    40, // in bytes
			wantStreamSize: 36, // in bytes
			wantShards:     2,
			wantErr:        false,
		},
		{
			// although the ingested rate by an ingester is 0, the stream is big enough to be sharded.
			name:           "enough data to have two shards, stream size (36b) + ingested rate (0b) > 22b",
			stream:         &logproto.Stream{Entries: []logproto.Entry{{Line: "a"}, {Line: "b"}}},
			rate:           0,  // in bytes
			desiredRate:    22, // in bytes
			wantStreamSize: 36, // in bytes
			wantShards:     2,
			wantErr:        false,
		},
		{
			name: "a lot of shards, stream size (1mb) + ingested rate (300mb) > 3mb",
			stream: &logproto.Stream{Entries: []logproto.Entry{
				{Line: "a"}, {Line: "b"}, {Line: "c"}, {Line: "d"}, {Line: "e"},
			}},
			rate:           0,  // in bytes
			desiredRate:    22, // in bytes
			wantStreamSize: 90, // in bytes
			wantShards:     5,
			wantErr:        false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.ShardStreams.DesiredRate = tc.desiredRate

			d := &Distributor{
				rateStore: &fakeRateStore{tc.rate},
			}
			got := d.shardCountFor(util_log.Logger, tc.stream, tc.wantStreamSize, "fake", limits.ShardStreams)
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
			limits.EnforceMetricName = false
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
		distributorConfig.factory = factory
		if factory == nil {
			distributorConfig.factory = func(addr string) (ring_client.PoolClient, error) {
				return ingesterByAddr[addr], nil
			}
		}

		overrides, err := validation.NewOverrides(*limits, nil)
		require.NoError(t, err)

		d, err := New(distributorConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, overrides, prometheus.NewPedanticRegistry())
		require.NoError(t, err)
		require.NoError(t, services.StartAndAwaitRunning(context.Background(), d))
		distributors[i] = d
	}

	if distributors[0].distributorsLifecycler != nil {
		test.Poll(t, time.Second, numDistributors, func() interface{} {
			return distributors[0].distributorsLifecycler.HealthyInstancesCount()
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

func (i *mockIngester) Push(ctx context.Context, in *logproto.PushRequest, opts ...grpc.CallOption) (*logproto.PushResponse, error) {
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

func (i *mockIngester) GetStreamRates(ctx context.Context, in *logproto.StreamRatesRequest, opts ...grpc.CallOption) (*logproto.StreamRatesResponse, error) {
	return &logproto.StreamRatesResponse{}, nil
}

func (i *mockIngester) Close() error {
	return nil
}

type fakeRateStore struct {
	rate int64
}

func (s *fakeRateStore) RateFor(_ string, _ uint64) int64 {
	return s.rate
}
