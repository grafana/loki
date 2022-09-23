package distributor

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/runtime"
	fe "github.com/grafana/loki/pkg/util/flagext"
	loki_net "github.com/grafana/loki/pkg/util/net"
	"github.com/grafana/loki/pkg/util/test"
	"github.com/grafana/loki/pkg/validation"
)

const (
	numIngesters = 5
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
		t.Run(fmt.Sprintf("[%d](samples=%v)", i, tc.lines), func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.IngestionRateMB = ingestionRateLimit
			limits.IngestionBurstSizeMB = ingestionRateLimit
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

			d := prepare(t, limits, nil, nil)
			defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck

			request := makeWriteRequest(tc.lines, 10)

			if tc.mangleLabels {
				request.Streams[0].Labels = `{ab"`
			}

			response, err := d.Push(ctx, request)
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
			ingester := &mockIngester{}
			d := prepare(t, testData.limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
			defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
			_, err := d.Push(ctx, testData.push)
			assert.NoError(t, err)
			assert.Equal(t, testData.expectedPush, ingester.pushed[0])
		})
	}
}

func Test_SortLabelsOnPush(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	ingester := &mockIngester{}
	d := prepare(t, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck

	request := makeWriteRequest(10, 10)
	request.Streams[0].Labels = `{buzz="f", a="b"}`
	_, err := d.Push(ctx, request)
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

		d := prepare(t, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
		defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck

		_, err := d.Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		require.Len(t, ingester.pushed[0].Streams[0].Entries[0].Line, 5)
	})
}

func TestStreamShard(t *testing.T) {
	// setup base stream.
	baseStream := logproto.Stream{}
	baseLabels := "{app='myapp', job='fizzbuzz'}"
	lbs, err := syntax.ParseLabels(baseLabels)
	require.NoError(t, err)
	baseStream.Hash = lbs.Hash()
	baseStream.Labels = lbs.String()

	// helper funcs
	generateEntries := func(n int) []logproto.Entry {
		var entries []logproto.Entry
		for i := 0; i < n; i++ {
			entries = append(entries, logproto.Entry{
				Line:      fmt.Sprintf("log line %d", i),
				Timestamp: time.Now(),
			})
		}
		return entries
	}
	generateShardLabels := func(baseLabels string, idx int) labels.Labels {
		// append a shard label to the given labels. The shard value will be 'idx'.
		lbs, err := syntax.ParseLabels(baseLabels)
		require.NoError(t, err)
		lbs = append(lbs, labels.Label{Name: ShardLbName, Value: fmt.Sprintf("%d", idx)})
		return lbs
	}

	totalEntries := generateEntries(100)

	for _, tc := range []struct {
		name              string
		entries           []logproto.Entry
		shards            int // stub call to ShardCountFor.
		wantDerivedStream []streamTracker
	}{
		{
			name:              "one shard with no entries",
			entries:           nil,
			shards:            1,
			wantDerivedStream: []streamTracker{{stream: baseStream}},
		},
		{
			name:    "one shard with one entry",
			shards:  1,
			entries: totalEntries[0:1],
			wantDerivedStream: []streamTracker{
				{
					stream: logproto.Stream{
						Entries: []logproto.Entry{totalEntries[0]},
						Labels:  baseStream.Labels,
						Hash:    baseStream.Hash,
					},
				},
			},
		},
		{
			name:    "two shards with 3 entries",
			shards:  2,
			entries: totalEntries[0:3],
			wantDerivedStream: []streamTracker{
				{ // shard 1.
					stream: logproto.Stream{
						Entries: totalEntries[0:1],
						Labels:  generateShardLabels(baseLabels, 0).String(),
						Hash:    generateShardLabels(baseLabels, 0).Hash(),
					},
				}, // shard 2.
				{
					stream: logproto.Stream{
						Entries: totalEntries[1:3],
						Labels:  generateShardLabels(baseLabels, 1).String(),
						Hash:    generateShardLabels(baseLabels, 1).Hash(),
					},
				},
			},
		},
		{
			name:    "two shards with 5 entries",
			shards:  2,
			entries: totalEntries[0:5],
			wantDerivedStream: []streamTracker{
				{ // shard 1.
					stream: logproto.Stream{
						Entries: totalEntries[0:2],
						Labels:  generateShardLabels(baseLabels, 0).String(),
						Hash:    generateShardLabels(baseLabels, 0).Hash(),
					},
				}, // shard 2.
				{
					stream: logproto.Stream{
						Entries: totalEntries[2:5],
						Labels:  generateShardLabels(baseLabels, 1).String(),
						Hash:    generateShardLabels(baseLabels, 1).Hash(),
					},
				},
			},
		},
		{
			name:    "one shard with 20 entries",
			shards:  1,
			entries: totalEntries[0:20],
			wantDerivedStream: []streamTracker{
				{ // shard 1.
					stream: logproto.Stream{
						Entries: totalEntries[0:20],
						Labels:  baseStream.Labels,
						Hash:    baseStream.Hash,
					},
				},
			},
		},
		{
			name:    "two shards with 20 entries",
			shards:  2,
			entries: totalEntries[0:20],
			wantDerivedStream: []streamTracker{
				{ // shard 1.
					stream: logproto.Stream{
						Entries: totalEntries[0:10],
						Labels:  generateShardLabels(baseLabels, 0).String(),
						Hash:    generateShardLabels(baseLabels, 0).Hash(),
					},
				}, // shard 2.
				{
					stream: logproto.Stream{
						Entries: totalEntries[10:20],
						Labels:  generateShardLabels(baseLabels, 1).String(),
						Hash:    generateShardLabels(baseLabels, 1).Hash(),
					},
				},
			},
		},
		{
			name:    "four shards with 20 entries",
			shards:  4,
			entries: totalEntries[0:20],
			wantDerivedStream: []streamTracker{
				{ // shard 1.
					stream: logproto.Stream{
						Entries: totalEntries[0:5],
						Labels:  generateShardLabels(baseLabels, 0).String(),
						Hash:    generateShardLabels(baseLabels, 0).Hash(),
					},
				},
				{ // shard 2.
					stream: logproto.Stream{
						Entries: totalEntries[5:10],
						Labels:  generateShardLabels(baseLabels, 1).String(),
						Hash:    generateShardLabels(baseLabels, 1).Hash(),
					},
				},
				{ // shard 3.
					stream: logproto.Stream{
						Entries: totalEntries[10:15],
						Labels:  generateShardLabels(baseLabels, 2).String(),
						Hash:    generateShardLabels(baseLabels, 2).Hash(),
					},
				},
				{ // shard 4.
					stream: logproto.Stream{
						Entries: totalEntries[15:20],
						Labels:  generateShardLabels(baseLabels, 3).String(),
						Hash:    generateShardLabels(baseLabels, 3).Hash(),
					},
				},
			},
		},
		{
			name:    "four shards with 2 entries",
			shards:  4,
			entries: totalEntries[0:2],
			wantDerivedStream: []streamTracker{
				{
					stream: logproto.Stream{
						Entries: []logproto.Entry{},
						Labels:  generateShardLabels(baseLabels, 0).String(),
						Hash:    generateShardLabels(baseLabels, 0).Hash(),
					},
				},
				{
					stream: logproto.Stream{
						Entries: totalEntries[0:1],
						Labels:  generateShardLabels(baseLabels, 1).String(),
						Hash:    generateShardLabels(baseLabels, 1).Hash(),
					},
				},
				{
					stream: logproto.Stream{
						Entries: []logproto.Entry{},
						Labels:  generateShardLabels(baseLabels, 2).String(),
						Hash:    generateShardLabels(baseLabels, 2).Hash(),
					},
				},
				{
					stream: logproto.Stream{
						Entries: totalEntries[1:2],
						Labels:  generateShardLabels(baseLabels, 3).String(),
						Hash:    generateShardLabels(baseLabels, 3).Hash(),
					},
				},
			},
		},
		{
			name:    "four shards with 1 entry",
			shards:  4,
			entries: totalEntries[0:1],
			wantDerivedStream: []streamTracker{
				{
					stream: logproto.Stream{
						Labels:  generateShardLabels(baseLabels, 0).String(),
						Hash:    generateShardLabels(baseLabels, 0).Hash(),
						Entries: []logproto.Entry{},
					},
				},
				{
					stream: logproto.Stream{
						Labels:  generateShardLabels(baseLabels, 1).String(),
						Hash:    generateShardLabels(baseLabels, 1).Hash(),
						Entries: []logproto.Entry{},
					},
				},
				{
					stream: logproto.Stream{
						Labels:  generateShardLabels(baseLabels, 2).String(),
						Hash:    generateShardLabels(baseLabels, 2).Hash(),
						Entries: []logproto.Entry{},
					},
				},
				{
					stream: logproto.Stream{
						Entries: totalEntries[0:1],
						Labels:  generateShardLabels(baseLabels, 3).String(),
						Hash:    generateShardLabels(baseLabels, 3).Hash(),
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseStream.Entries = tc.entries

			d := Distributor{
				sharder: NewStreamSharderMock(tc.shards).ShardCountFor,
			}

			_, derivedStreams, err := d.shardStream(baseStream, "fake")
			require.NoError(t, err)

			require.Equal(t, tc.wantDerivedStream, derivedStreams)
		})
	}
}

func BenchmarkShardStream(b *testing.B) {
	stream := logproto.Stream{}
	labels := "{app='myapp', job='fizzbuzz'}"
	lbs, err := syntax.ParseLabels(labels)
	require.NoError(b, err)
	stream.Hash = lbs.Hash()
	stream.Labels = lbs.String()

	// helper funcs
	generateEntries := func(n int) []logproto.Entry {
		var entries []logproto.Entry
		for i := 0; i < n; i++ {
			entries = append(entries, logproto.Entry{
				Line:      fmt.Sprintf("log line %d", i),
				Timestamp: time.Now(),
			})
		}
		return entries
	}
	allEntries := generateEntries(25000)

	b.Run("high number of entries, low number of shards", func(b *testing.B) {
		d := Distributor{
			sharder: NewStreamSharderMock(2).ShardCountFor,
		}
		stream.Entries = allEntries

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, "fake") //nolint:errcheck
		}
	})

	b.Run("low number of entries, low number of shards", func(b *testing.B) {
		d := Distributor{
			sharder: NewStreamSharderMock(2).ShardCountFor,
		}
		stream.Entries = nil

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, "fake") //nolint:errcheck

		}
	})

	b.Run("high number of entries, high number of shards", func(b *testing.B) {
		d := Distributor{
			sharder: NewStreamSharderMock(64).ShardCountFor,
		}
		stream.Entries = allEntries

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, "fake") //nolint:errcheck
		}
	})

	b.Run("low number of entries, high number of shards", func(b *testing.B) {
		d := Distributor{
			sharder: NewStreamSharderMock(64).ShardCountFor,
		}
		stream.Entries = nil

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			d.shardStream(stream, "fake") //nolint:errcheck
		}
	})
}

func Benchmark_SortLabelsOnPush(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	ingester := &mockIngester{}
	d := prepare(&testing.T{}, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
	request := makeWriteRequest(10, 10)
	vCtx := d.validator.getValidationContextForTime(testTime, "123")
	for n := 0; n < b.N; n++ {
		stream := request.Streams[0]
		stream.Labels = `{buzz="f", a="b"}`
		_, err := d.parseStreamLabels(vCtx, stream.Labels, &stream)
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
	ingester := &mockIngester{}
	d := prepare(&testing.T{}, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {

		_, err := d.Push(ctx, request)
		if err != nil {
			require.NoError(b, err)
		}
	}
}

func Benchmark_PushWithLineTruncation(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	limits.IngestionRateMB = math.MaxInt32
	limits.MaxLineSizeTruncate = true
	limits.MaxLineSize = 50

	ingester := &mockIngester{}
	d := prepare(&testing.T{}, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {

		_, err := d.Push(ctx, request)
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

			// Init a shared KVStore
			kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
			t.Cleanup(func() { assert.NoError(t, closer.Close()) })

			// Start all expected distributors
			distributors := make([]*Distributor, testData.distributors)
			for i := 0; i < testData.distributors; i++ {
				distributors[i] = prepare(t, limits, kvStore, nil)
				defer services.StopAndAwaitTerminated(context.Background(), distributors[i]) //nolint:errcheck
			}

			// If the distributors ring is setup, wait until the first distributor
			// updates to the expected size
			if distributors[0].distributorsLifecycler != nil {
				test.Poll(t, time.Second, testData.distributors, func() interface{} {
					return distributors[0].distributorsLifecycler.HealthyInstancesCount()
				})
			}

			// Push samples in multiple requests to the first distributor
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

func prepare(t *testing.T, limits *validation.Limits, kvStore kv.Client, factory func(addr string) (ring_client.PoolClient, error)) *Distributor {
	var (
		distributorConfig Config
		clientConfig      client.Config
	)
	flagext.DefaultValues(&distributorConfig, &clientConfig)

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	// Mock the ingesters ring
	ingesters := map[string]*mockIngester{}
	for i := 0; i < numIngesters; i++ {
		ingesters[fmt.Sprintf("ingester%d", i)] = &mockIngester{}
	}

	ingestersRing := &mockRing{
		replicationFactor: 3,
	}
	for addr := range ingesters {
		ingestersRing.ingesters = append(ingestersRing.ingesters, ring.InstanceDesc{
			Addr: addr,
		})
	}

	loopbackName, err := loki_net.LoopbackInterfaceName()
	require.NoError(t, err)

	distributorConfig.DistributorRing.HeartbeatPeriod = 100 * time.Millisecond
	distributorConfig.DistributorRing.InstanceID = strconv.Itoa(rand.Int())
	distributorConfig.DistributorRing.KVStore.Mock = kvStore
	distributorConfig.DistributorRing.KVStore.Store = "inmemory"
	distributorConfig.DistributorRing.InstanceInterfaceNames = []string{loopbackName}
	distributorConfig.factory = factory
	if factory == nil {
		distributorConfig.factory = func(addr string) (ring_client.PoolClient, error) {
			return ingesters[addr], nil
		}
	}

	d, err := New(distributorConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, overrides, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), d))

	return d
}

func makeWriteRequest(lines int, size int) *logproto.PushRequest {
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
			},
		},
	}

	for i := 0; i < lines; i++ {
		// Construct the log line, honoring the input size
		line := strconv.Itoa(i) + strings.Repeat(" ", size)
		line = line[:size]

		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
			Line:      line,
		})
	}
	return &req
}

type mockIngester struct {
	grpc_health_v1.HealthClient
	logproto.PusherClient

	pushed []*logproto.PushRequest
}

func (i *mockIngester) Push(ctx context.Context, in *logproto.PushRequest, opts ...grpc.CallOption) (*logproto.PushResponse, error) {
	i.pushed = append(i.pushed, in)
	return nil, nil
}

func (i *mockIngester) Close() error {
	return nil
}

// Copied from Cortex; TODO(twilkie) - factor this our and share it.
// mockRing doesn't do virtual nodes, just returns mod(key) + replicationFactor
// ingesters.
type mockRing struct {
	prometheus.Counter
	ingesters         []ring.InstanceDesc
	replicationFactor uint32
}

func (r mockRing) Get(key uint32, op ring.Operation, buf []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	result := ring.ReplicationSet{
		MaxErrors: 1,
		Instances: buf[:0],
	}
	for i := uint32(0); i < r.replicationFactor; i++ {
		n := (key + i) % uint32(len(r.ingesters))
		result.Instances = append(result.Instances, r.ingesters[n])
	}
	return result, nil
}

func (r mockRing) GetAllHealthy(op ring.Operation) (ring.ReplicationSet, error) {
	return r.GetReplicationSetForOperation(op)
}

func (r mockRing) GetReplicationSetForOperation(op ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{
		Instances: r.ingesters,
		MaxErrors: 1,
	}, nil
}

func (r mockRing) ReplicationFactor() int {
	return int(r.replicationFactor)
}

func (r mockRing) InstancesCount() int {
	return len(r.ingesters)
}

func (r mockRing) Subring(key uint32, n int) ring.ReadRing {
	return r
}

func (r mockRing) HasInstance(instanceID string) bool {
	for _, ing := range r.ingesters {
		if ing.Addr != instanceID {
			return true
		}
	}
	return false
}

func (r mockRing) ShuffleShard(identifier string, size int) ring.ReadRing {
	// take advantage of pass by value to bound to size:
	r.ingesters = r.ingesters[:size]
	return r
}

func (r mockRing) ShuffleShardWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) ring.ReadRing {
	return r
}

func (r mockRing) CleanupShuffleShardCache(identifier string) {}

func (r mockRing) GetInstanceState(instanceID string) (ring.InstanceState, error) {
	return 0, nil
}
