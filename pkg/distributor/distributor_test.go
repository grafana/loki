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
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus/testutil"

	otlptranslate "github.com/prometheus/prometheus/storage/remote/otlptranslator/prometheus"

	"github.com/grafana/loki/pkg/push"

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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

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
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

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

func Test_MissingEnforcedLabels(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	limits.EnforcedLabels = []string{"app", "env"}

	distributors, _ := prepare(t, 1, 5, limits, nil)

	// request with all required labels.
	lbs := labels.FromMap(map[string]string{"app": "foo", "env": "prod"})
	missing, missingLabels := distributors[0].missingEnforcedLabels(lbs, "test")
	assert.False(t, missing)
	assert.Empty(t, missingLabels)

	// request missing the `app` label.
	lbs = labels.FromMap(map[string]string{"env": "prod"})
	missing, missingLabels = distributors[0].missingEnforcedLabels(lbs, "test")
	assert.True(t, missing)
	assert.EqualValues(t, []string{"app"}, missingLabels)

	// request missing all required labels.
	lbs = labels.FromMap(map[string]string{"pod": "distributor-abc"})
	missing, missingLabels = distributors[0].missingEnforcedLabels(lbs, "test")
	assert.True(t, missing)
	assert.EqualValues(t, []string{"app", "env"}, missingLabels)
}

func Test_PushWithEnforcedLabels(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	// makeWriteRequest only contains a `{foo="bar"}` label.
	req := makeWriteRequest(100, 100)
	limits.EnforcedLabels = []string{"app", "env"}
	distributors, _ := prepare(t, 1, 3, limits, nil)
	// enforced labels configured, but all labels are missing.
	_, err := distributors[0].Push(ctx, req)
	require.Error(t, err)
	expectedErr := httpgrpc.Errorf(http.StatusBadRequest, validation.MissingEnforcedLabelsErrorMsg, "app,env", "test")
	require.EqualError(t, err, expectedErr.Error())

	// enforced labels, but all labels are present.
	req = makeWriteRequestWithLabels(100, 100, []string{`{app="foo", env="prod"}`}, false, false, false)
	_, err = distributors[0].Push(ctx, req)
	require.NoError(t, err)

	// no enforced labels, so no errors.
	limits.EnforcedLabels = []string{}
	distributors, _ = prepare(t, 1, 3, limits, nil)
	_, err = distributors[0].Push(ctx, req)
	require.NoError(t, err)
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
		kafkaWriter := &mockKafkaWriter{
			failOnWrite: true,
		}
		distributors, _ := prepare(t, 1, 0, limits, nil)
		for _, d := range distributors {
			d.cfg.KafkaEnabled = true
			d.cfg.IngesterEnabled = false
			d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
			d.kafkaWriter = kafkaWriter
		}

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.Error(t, err)
	})

	t.Run("with kafka, no failures is successful", func(t *testing.T) {
		kafkaWriter := &mockKafkaWriter{
			failOnWrite: false,
		}
		distributors, _ := prepare(t, 1, 0, limits, nil)
		for _, d := range distributors {
			d.cfg.KafkaEnabled = true
			d.cfg.IngesterEnabled = false
			d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
			d.kafkaWriter = kafkaWriter
		}

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)

		require.Equal(t, 1, kafkaWriter.pushed)
	})

	t.Run("with kafka and ingesters, both must complete", func(t *testing.T) {
		kafkaWriter := &mockKafkaWriter{
			failOnWrite: false,
		}
		distributors, ingesters := prepare(t, 1, 3, limits, nil)
		ingesters[0].succeedAfter = 5 * time.Millisecond
		ingesters[1].succeedAfter = 10 * time.Millisecond
		ingesters[2].succeedAfter = 15 * time.Millisecond

		for _, d := range distributors {
			d.cfg.KafkaEnabled = true
			d.cfg.IngesterEnabled = true
			d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1000
			d.kafkaWriter = kafkaWriter
		}

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)

		require.Equal(t, 1, kafkaWriter.pushed)

		require.Equal(t, 1, len(ingesters[0].pushed))
		require.Equal(t, 1, len(ingesters[1].pushed))
		require.Eventually(t, func() bool {
			ingesters[2].mu.Lock()
			defer ingesters[2].mu.Unlock()
			return len(ingesters[2].pushed) == 1
		}, time.Second, 10*time.Millisecond)
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

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Len(t, topVal.Streams[0].Entries[0].Line, 5)
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

func TestStreamShardByTime(t *testing.T) {
	baseTimestamp := time.Date(2024, 10, 31, 12, 34, 56, 0, time.UTC)
	t.Logf("Base timestamp: %s (unix %d)", baseTimestamp.Format(time.RFC3339Nano), baseTimestamp.Unix())

	for _, tc := range []struct {
		test         string
		labels       string
		entries      []logproto.Entry
		timeShardLen time.Duration
		ignoreFrom   time.Time
		expResult    []streamWithTimeShard
	}{
		{
			test:         "zero shard because no entries",
			labels:       "{app='myapp'}",
			entries:      nil,
			timeShardLen: time.Hour,
			expResult:    nil,
		},
		{
			test:   "single shard with one entry",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(1 * time.Second),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
				}}, linesTotalLen: 3},
			},
		},
		{
			test:   "one entry that is ignored",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(-10 * time.Minute),
			expResult:    nil,
		},
		{
			test:   "single shard with two entries",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(2 * time.Second),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				}}, linesTotalLen: 6},
			},
		},
		{
			test:   "one shard and another stream with original labels",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				{Timestamp: baseTimestamp, Line: "foo"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(1 * time.Second),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
				}}, linesTotalLen: 3},
				{Stream: logproto.Stream{Labels: `{app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				}}, linesTotalLen: 3},
			},
		},
		{
			test:   "single shard with two entries reversed",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				{Timestamp: baseTimestamp, Line: "foo"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(2 * time.Second),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				}}, linesTotalLen: 6},
			},
		},
		{
			test:   "two shards without a gap",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				{Timestamp: baseTimestamp.Add(time.Hour), Line: "baz"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(2 * time.Hour),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				}}, linesTotalLen: 6},
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730379600_1730383200", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Add(time.Hour), Line: "baz"},
				}}, linesTotalLen: 3},
			},
		},
		{
			test:   "two shards with a gap",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				{Timestamp: baseTimestamp.Add(4 * time.Hour), Line: "baz"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(5 * time.Hour),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				}}, linesTotalLen: 6},
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730390400_1730394000", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Add(4 * time.Hour), Line: "baz"},
				}}, linesTotalLen: 3},
			},
		},
		{
			test:   "bigger shard len",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				{Timestamp: baseTimestamp.Add(6 * time.Hour), Line: "baz"},
			},
			timeShardLen: 24 * time.Hour,
			ignoreFrom:   baseTimestamp.Add(7 * time.Hour),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730332800_1730419200", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
					{Timestamp: baseTimestamp.Add(6 * time.Hour), Line: "baz"},
				}}, linesTotalLen: 9},
			},
		},
		{
			test:   "bigger shard len with some unsharded",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp, Line: "foo"},
				{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				{Timestamp: baseTimestamp.Add(6 * time.Hour), Line: "baz"},
			},
			timeShardLen: 24 * time.Hour,
			ignoreFrom:   baseTimestamp.Add(5 * time.Hour),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730332800_1730419200", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp, Line: "foo"},
					{Timestamp: baseTimestamp.Add(time.Second), Line: "bar"},
				}}, linesTotalLen: 6},
				{Stream: logproto.Stream{Labels: `{app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Add(6 * time.Hour), Line: "baz"},
				}}, linesTotalLen: 3},
			},
		},
		{
			test:   "longer messy gaps",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "11"},
				{Timestamp: baseTimestamp, Line: "13"},
				{Timestamp: baseTimestamp, Line: "14"},
				{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "12"},
				{Timestamp: baseTimestamp.Add(2 * time.Hour), Line: "32"},
				{Timestamp: baseTimestamp.Truncate(time.Hour).Add(2 * time.Hour), Line: "31"},
				{Timestamp: baseTimestamp.Add(5 * time.Hour), Line: "41"},
				{Timestamp: baseTimestamp.Truncate(time.Hour).Add(time.Hour), Line: "21"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Add(7 * time.Hour),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "11"},
					{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "12"},
					{Timestamp: baseTimestamp, Line: "13"},
					{Timestamp: baseTimestamp, Line: "14"},
				}}, linesTotalLen: 8},
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730379600_1730383200", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Truncate(time.Hour).Add(time.Hour), Line: "21"},
				}}, linesTotalLen: 2},
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730383200_1730386800", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Truncate(time.Hour).Add(2 * time.Hour), Line: "31"},
					{Timestamp: baseTimestamp.Add(2 * time.Hour), Line: "32"},
				}}, linesTotalLen: 4},
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730394000_1730397600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Add(5 * time.Hour), Line: "41"},
				}}, linesTotalLen: 2},
			},
		},
		{
			test:   "longer messy with a couple ofc unsharded",
			labels: `{app="myapp"}`,
			entries: []logproto.Entry{
				{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "11"},
				{Timestamp: baseTimestamp, Line: "13"},
				{Timestamp: baseTimestamp, Line: "14"},
				{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "12"},
				{Timestamp: baseTimestamp.Add(2 * time.Hour), Line: "32"},
				{Timestamp: baseTimestamp.Truncate(time.Hour).Add(2 * time.Hour), Line: "31"},
				{Timestamp: baseTimestamp.Add(5 * time.Hour), Line: "41"},
				{Timestamp: baseTimestamp.Truncate(time.Hour).Add(time.Hour), Line: "21"},
			},
			timeShardLen: time.Hour,
			ignoreFrom:   baseTimestamp.Truncate(time.Hour).Add(1*time.Hour + 35*time.Minute),
			expResult: []streamWithTimeShard{
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730376000_1730379600", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "11"},
					{Timestamp: baseTimestamp.Truncate(time.Hour), Line: "12"},
					{Timestamp: baseTimestamp, Line: "13"},
					{Timestamp: baseTimestamp, Line: "14"},
				}}, linesTotalLen: 8},
				{Stream: logproto.Stream{Labels: `{__time_shard__="1730379600_1730383200", app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Truncate(time.Hour).Add(time.Hour), Line: "21"},
				}}, linesTotalLen: 2},
				{Stream: logproto.Stream{Labels: `{app="myapp"}`, Entries: []logproto.Entry{
					{Timestamp: baseTimestamp.Truncate(time.Hour).Add(2 * time.Hour), Line: "31"},
					{Timestamp: baseTimestamp.Add(2 * time.Hour), Line: "32"},
					{Timestamp: baseTimestamp.Add(5 * time.Hour), Line: "41"},
				}}, linesTotalLen: 6},
			},
		},
	} {
		t.Run(tc.test, func(t *testing.T) {
			lbls, err := syntax.ParseLabels(tc.labels)
			require.NoError(t, err)
			stream := logproto.Stream{
				Labels:  tc.labels,
				Hash:    lbls.Hash(),
				Entries: tc.entries,
			}

			shardedStreams, ok := shardStreamByTime(stream, lbls, tc.timeShardLen, tc.ignoreFrom)
			if tc.expResult == nil {
				assert.False(t, ok)
				assert.Nil(t, shardedStreams)
				return
			}
			require.True(t, ok)
			require.Len(t, shardedStreams, len(tc.expResult))

			for i, ss := range shardedStreams {
				assert.Equal(t, tc.expResult[i].linesTotalLen, ss.linesTotalLen)
				assert.Equal(t, tc.expResult[i].Labels, ss.Labels)
				assert.EqualValues(t, tc.expResult[i].Entries, ss.Entries)
			}
		})
	}
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
			name:       "service name label should not get counted against max labels count",
			origLabels: `{foo="bar", service_name="unknown_service"}`,
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
					Name:  loghttp_push.LabelServiceName,
					Value: loghttp_push.ServiceUnknown,
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
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.Equal(t, success, response)
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

	partitionRing := ring.NewPartitionRing(ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{
			1: {
				Id:             1,
				Tokens:         []uint32{1},
				State:          ring.PartitionActive,
				StateTimestamp: time.Now().Unix(),
			},
		},
		Owners: map[string]ring.OwnerDesc{
			"test": {
				OwnedPartition:   1,
				State:            ring.OwnerActive,
				UpdatedTimestamp: time.Now().Unix(),
			},
		},
	})
	partitionRingReader := mockPartitionRingReader{
		ring: partitionRing,
	}

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

		d, err := New(distributorConfig, ingesterConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, partitionRingReader, overrides, prometheus.NewPedanticRegistry(), constants.Loki, nil, nil, log.NewNopLogger())
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

type mockKafkaWriter struct {
	failOnWrite bool
	pushed      int
}

func (m *mockKafkaWriter) ProduceSync(_ context.Context, _ []*kgo.Record) kgo.ProduceResults {
	if m.failOnWrite {
		return kgo.ProduceResults{
			{
				Err: kgo.ErrRecordTimeout,
			},
		}
	}
	m.pushed++
	return kgo.ProduceResults{
		{
			Err: nil,
		},
	}
}

func (m *mockKafkaWriter) Close() {}

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

	i.mu.Lock()
	defer i.mu.Unlock()
	for _, s := range in.Streams {
		for _, e := range s.Entries {
			for _, sm := range e.StructuredMetadata {
				if strings.ContainsRune(sm.Value, utf8.RuneError) {
					return nil, fmt.Errorf("sm value was not sanitized before being pushed to ignester, invalid utf 8 rune %d", utf8.RuneError)
				}
				if sm.Name != otlptranslate.NormalizeLabel(sm.Name) {
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
		assert.Equal(t, tc.numSanitizations, testutil.ToFloat64(distributors[0].tenantPushSanitizedStructuredMetadata.WithLabelValues("test")))
	}
}
