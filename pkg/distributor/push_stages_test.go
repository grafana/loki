package distributor

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/limits"
	limitsproto "github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// recordingLimitsClient records the requests it gets and rejects the stream hashes a test selects.
type recordingLimitsClient struct {
	mu sync.Mutex

	exceedsRequests []*limitsproto.ExceedsLimitsRequest
	checkRequests   []*limitsproto.CheckLimitsAndShardRequest

	exceedsReasons map[uint64]uint32
	exceedsErr     error
	checkResults   map[uint64]*limitsproto.StreamShardResult
	checkErr       error
}

func (c *recordingLimitsClient) ExceedsLimits(_ context.Context, req *limitsproto.ExceedsLimitsRequest) (*limitsproto.ExceedsLimitsResponse, error) {
	c.mu.Lock()
	c.exceedsRequests = append(c.exceedsRequests, req)
	err := c.exceedsErr
	reasons := c.exceedsReasons
	c.mu.Unlock()

	if err != nil {
		return nil, err
	}

	resp := &limitsproto.ExceedsLimitsResponse{}
	for _, stream := range req.Streams {
		if reason, ok := reasons[stream.StreamHash]; ok {
			resp.Results = append(resp.Results, &limitsproto.ExceedsLimitsResult{
				StreamHash: stream.StreamHash,
				Reason:     reason,
			})
		}
	}
	return resp, nil
}

func (c *recordingLimitsClient) CheckLimitsAndShard(_ context.Context, req *limitsproto.CheckLimitsAndShardRequest) (*limitsproto.CheckLimitsAndShardResponse, error) {
	c.mu.Lock()
	c.checkRequests = append(c.checkRequests, req)
	err := c.checkErr
	results := c.checkResults
	c.mu.Unlock()

	if err != nil {
		return nil, err
	}

	resp := &limitsproto.CheckLimitsAndShardResponse{}
	for _, stream := range req.Streams {
		if result, ok := results[stream.StreamHash]; ok {
			resp.Results = append(resp.Results, result)
		}
	}
	return resp, nil
}

func (c *recordingLimitsClient) stagesExceedsRequests() []*limitsproto.ExceedsLimitsRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*limitsproto.ExceedsLimitsRequest(nil), c.exceedsRequests...)
}

func (c *recordingLimitsClient) stagesCheckRequests() []*limitsproto.CheckLimitsAndShardRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*limitsproto.CheckLimitsAndShardRequest(nil), c.checkRequests...)
}

type stagesDiscard struct {
	reason  string
	policy  string
	samples float64
	bytes   float64
}

func stagesNewLimits() *validation.Limits {
	lim := &validation.Limits{}
	flagext.DefaultValues(lim)
	lim.DiscoverLogLevels = false
	return lim
}

func stagesResetDiscardMetrics(t *testing.T) {
	t.Helper()
	validation.DiscardedSamples.Reset()
	validation.DiscardedBytes.Reset()
	t.Cleanup(func() {
		validation.DiscardedSamples.Reset()
		validation.DiscardedBytes.Reset()
	})
}

func stagesAssertDiscards(t *testing.T, want []stagesDiscard) {
	t.Helper()

	require.Equal(t, len(want), testutil.CollectAndCount(validation.DiscardedSamples))
	require.Equal(t, len(want), testutil.CollectAndCount(validation.DiscardedBytes))
	for _, discard := range want {
		require.Equal(t, discard.samples, testutil.ToFloat64(
			validation.DiscardedSamples.WithLabelValues(discard.reason, "test", "0", discard.policy, constants.Loki),
		), "%s samples for policy %q", discard.reason, discard.policy)
		require.Equal(t, discard.bytes, testutil.ToFloat64(
			validation.DiscardedBytes.WithLabelValues(discard.reason, "test", "0", discard.policy, constants.Loki),
		), "%s bytes for policy %q", discard.reason, discard.policy)
	}
}

func stagesAssertRejectedPartialWrites(t *testing.T, d *Distributor, policy string, want float64) {
	t.Helper()

	if want == 0 {
		require.Zero(t, testutil.CollectAndCount(d.m.rejectedPartialWrites))
		return
	}

	require.Equal(t, 1, testutil.CollectAndCount(d.m.rejectedPartialWrites))
	require.Equal(t, want, testutil.ToFloat64(d.m.rejectedPartialWrites.WithLabelValues("test", policy)))
}

func stagesHTTPStatus(t *testing.T, err error) int32 {
	t.Helper()

	require.Error(t, err)
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok)
	return resp.Code
}

func stagesRequestHashes(streams []*limitsproto.StreamMetadata) []uint64 {
	hashes := make([]uint64, 0, len(streams))
	for _, stream := range streams {
		hashes = append(hashes, stream.StreamHash)
	}
	return hashes
}

func stagesAllIngestersHaveStreams(ingesters []mockIngester, want []string) bool {
	want = append([]string(nil), want...)
	slices.Sort(want)

	for i := range ingesters {
		ingester := &ingesters[i]
		ingester.mu.Lock()
		if len(ingester.pushed) != 1 {
			ingester.mu.Unlock()
			return false
		}

		got := make([]string, 0, len(ingester.pushed[0].Streams))
		for _, stream := range ingester.pushed[0].Streams {
			got = append(got, stream.Labels)
		}
		ingester.mu.Unlock()

		slices.Sort(got)
		if !slices.Equal(want, got) {
			return false
		}
	}
	return true
}

func stagesRequireWrites(t *testing.T, ingesters []mockIngester, want []string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return stagesAllIngestersHaveStreams(ingesters, want)
	}, time.Second, time.Millisecond)
}

func stagesAssertNoWrites(t *testing.T, ingesters []mockIngester) {
	t.Helper()
	for i := range ingesters {
		require.Nil(t, ingesters[i].Peek())
	}
}

func TestDistributor_PushErrorPrecedence(t *testing.T) {
	written := labels.FromStrings("app", "written")
	limited := labels.FromStrings("app", "limited")
	const invalidLabels = `{broken`

	tests := []struct {
		name              string
		request           func() *logproto.PushRequest
		exceedsReasons    map[uint64]uint32
		exceedsErr        error
		wantStatus        int32
		wantErrorContains string
		wantWritten       []string
		wantCheckedHashes []uint64
		wantDiscards      []stagesDiscard
	}{
		{
			name: "stream limit wins over validation and still writes the accepted stream",
			request: func() *logproto.PushRequest {
				return &logproto.PushRequest{Streams: []logproto.Stream{
					{Labels: written.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "keep"}}},
					{Labels: invalidLabels, Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "bad"}}},
					{Labels: limited.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "limit"}}},
				}}
			},
			exceedsReasons: map[uint64]uint32{
				labels.StableHash(limited): uint32(limits.ReasonMaxStreams),
			},
			wantStatus:        http.StatusTooManyRequests,
			wantErrorContains: limited.String(),
			wantWritten:       []string{written.String()},
			wantCheckedHashes: []uint64{labels.StableHash(written), labels.StableHash(limited)},
			wantDiscards: []stagesDiscard{
				{reason: validation.InvalidLabels, samples: 1, bytes: float64(len("bad"))},
				{reason: validation.StreamLimit, samples: 1, bytes: float64(len("limit"))},
			},
		},
		{
			name: "limits client error fails open but validation error remains",
			request: func() *logproto.PushRequest {
				return &logproto.PushRequest{Streams: []logproto.Stream{
					{Labels: written.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "keep"}}},
					{Labels: invalidLabels, Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "bad"}}},
				}}
			},
			exceedsErr:        errors.New("limits service unavailable"),
			wantStatus:        http.StatusBadRequest,
			wantWritten:       []string{written.String()},
			wantCheckedHashes: []uint64{labels.StableHash(written)},
			wantDiscards: []stagesDiscard{
				{reason: validation.InvalidLabels, samples: 1, bytes: float64(len("bad"))},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stagesResetDiscardMetrics(t)

			lim := stagesNewLimits()
			require.NoError(t, lim.Validate())

			distributors, ingesters := prepare(t, 1, 3, lim, nil)
			d := distributors[0]
			d.cfg.IngestLimitsEnabled = true

			client := &recordingLimitsClient{
				exceedsReasons: tc.exceedsReasons,
				exceedsErr:     tc.exceedsErr,
			}
			d.ingestLimits = newIngestLimits(client, prometheus.NewRegistry())

			resp, err := d.Push(user.InjectOrgID(context.Background(), "test"), tc.request())
			require.Equal(t, success, resp)
			require.Equal(t, tc.wantStatus, stagesHTTPStatus(t, err))
			if tc.wantErrorContains != "" {
				require.ErrorContains(t, err, tc.wantErrorContains)
			}

			exceeds := client.stagesExceedsRequests()
			require.Len(t, exceeds, 1)
			require.ElementsMatch(t, tc.wantCheckedHashes, stagesRequestHashes(exceeds[0].Streams))
			require.Empty(t, client.stagesCheckRequests())

			stagesRequireWrites(t, ingesters, tc.wantWritten)
			stagesAssertDiscards(t, tc.wantDiscards)
			stagesAssertRejectedPartialWrites(t, d, "", 0)
		})
	}
}

func TestDistributor_PushRejectPartialWritesOnStreamLimit(t *testing.T) {
	accepted := labels.FromStrings("app", "accepted")
	limited := labels.FromStrings("app", "limited")

	tests := []struct {
		name         string
		rejectPolicy string
	}{
		{name: "policy on accepted stream rejects the whole push", rejectPolicy: "accepted"},
		{name: "policy on stream-limit rejected stream rejects the whole push", rejectPolicy: "limited"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stagesResetDiscardMetrics(t)

			lim := stagesNewLimits()
			lim.PolicyStreamMapping = validation.PolicyStreamMapping{
				"accepted": []*validation.PriorityStream{{Selector: accepted.String(), Priority: 1}},
				"limited":  []*validation.PriorityStream{{Selector: limited.String(), Priority: 1}},
			}
			lim.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
				tc.rejectPolicy: {RejectPartialWrites: true},
			}
			require.NoError(t, lim.Validate())

			distributors, ingesters := prepare(t, 1, 3, lim, nil)
			d := distributors[0]
			d.cfg.IngestLimitsEnabled = true

			client := &recordingLimitsClient{
				exceedsReasons: map[uint64]uint32{
					labels.StableHash(limited): uint32(limits.ReasonMaxStreams),
				},
			}
			d.ingestLimits = newIngestLimits(client, prometheus.NewRegistry())

			resp, err := d.Push(user.InjectOrgID(context.Background(), "test"), &logproto.PushRequest{
				Streams: []logproto.Stream{
					{Labels: accepted.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "accepted"}}},
					{Labels: limited.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "limited"}}},
				},
			})
			require.Nil(t, resp)
			require.Equal(t, int32(http.StatusTooManyRequests), stagesHTTPStatus(t, err))

			exceeds := client.stagesExceedsRequests()
			require.Len(t, exceeds, 1)
			require.ElementsMatch(t, []uint64{labels.StableHash(accepted), labels.StableHash(limited)}, stagesRequestHashes(exceeds[0].Streams))

			stagesAssertNoWrites(t, ingesters)
			stagesAssertDiscards(t, []stagesDiscard{
				{reason: validation.StreamLimit, policy: "limited", samples: 1, bytes: float64(len("limited"))},
			})
			stagesAssertRejectedPartialWrites(t, d, tc.rejectPolicy, 1)
		})
	}
}

func TestDistributor_PushLiveDryRunWritesRejectedStreamUnsharded(t *testing.T) {
	stagesResetDiscardMetrics(t)

	stream := labels.FromStrings("app", "live")
	streamHash := labels.StableHash(stream)

	lim := stagesNewLimits()
	lim.ShardStreams.Enabled = true
	lim.ShardStreams.DesiredRate = 10
	lim.ShardStreams.LimitsServiceStreamShardingMode = shardstreams.LimitsServiceStreamShardingModeLive
	require.NoError(t, lim.Validate())

	distributors, ingesters := prepare(t, 1, 3, lim, nil)
	d := distributors[0]
	d.cfg.IngestLimitsEnabled = true
	d.cfg.IngestLimitsDryRunEnabled = true
	d.rateStore = &fakeRateStore{rate: 30, pushRate: 1}

	client := &recordingLimitsClient{
		checkResults: map[uint64]*limitsproto.StreamShardResult{
			streamHash: {
				StreamHash:   streamHash,
				Shards:       0,
				RejectReason: limits.ReasonMaxStreams.String(),
			},
		},
	}
	d.ingestLimits = newIngestLimits(client, prometheus.NewRegistry())

	resp, err := d.Push(user.InjectOrgID(context.Background(), "test"), &logproto.PushRequest{
		Streams: []logproto.Stream{{
			Labels:  stream.String(),
			Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "live"}},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, success, resp)

	checks := client.stagesCheckRequests()
	require.Len(t, checks, 1)
	require.Equal(t, []uint64{streamHash}, stagesRequestHashes(checks[0].Streams))
	require.Empty(t, client.stagesExceedsRequests())

	// The original labels show the stream was written unsharded, not rate-sharded.
	stagesRequireWrites(t, ingesters, []string{stream.String()})
	stagesAssertDiscards(t, nil)
	stagesAssertRejectedPartialWrites(t, d, "", 0)
}

func TestDistributor_PushLiveRejectionWinsOverEnforceRejection(t *testing.T) {
	stagesResetDiscardMetrics(t)

	live := labels.FromStrings("app", "live")
	enforced := labels.FromStrings("app", "enforced")
	liveHash := labels.StableHash(live)
	enforcedHash := labels.StableHash(enforced)
	shardingOff := false

	lim := stagesNewLimits()
	lim.ShardStreams.Enabled = true
	lim.ShardStreams.DesiredRate = 10
	lim.ShardStreams.LimitsServiceStreamShardingMode = shardstreams.LimitsServiceStreamShardingModeLive
	lim.PolicyStreamMapping = validation.PolicyStreamMapping{
		"live":     []*validation.PriorityStream{{Selector: live.String(), Priority: 1}},
		"enforced": []*validation.PriorityStream{{Selector: enforced.String(), Priority: 1}},
	}
	lim.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"enforced": {
			ShardStreams: &validation.PerPolicyConfigOverride{Enabled: &shardingOff},
		},
	}
	require.NoError(t, lim.Validate())

	distributors, ingesters := prepare(t, 1, 3, lim, nil)
	d := distributors[0]
	d.cfg.IngestLimitsEnabled = true
	d.rateStore = &fakeRateStore{rate: 30, pushRate: 1}

	client := &recordingLimitsClient{
		exceedsReasons: map[uint64]uint32{
			enforcedHash: uint32(limits.ReasonMaxStreams),
		},
		checkResults: map[uint64]*limitsproto.StreamShardResult{
			liveHash: {
				StreamHash:   liveHash,
				Shards:       0,
				RejectReason: limits.ReasonMaxStreams.String(),
			},
		},
	}
	d.ingestLimits = newIngestLimits(client, prometheus.NewRegistry())

	resp, err := d.Push(user.InjectOrgID(context.Background(), "test"), &logproto.PushRequest{
		Streams: []logproto.Stream{
			{Labels: live.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "live"}}},
			{Labels: enforced.String(), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "enforced"}}},
		},
	})
	require.Nil(t, resp)
	require.Equal(t, int32(http.StatusTooManyRequests), stagesHTTPStatus(t, err))
	require.ErrorContains(t, err, live.String())
	require.NotContains(t, err.Error(), enforced.String())

	checks := client.stagesCheckRequests()
	require.Len(t, checks, 1)
	require.Equal(t, []uint64{liveHash}, stagesRequestHashes(checks[0].Streams))

	exceeds := client.stagesExceedsRequests()
	require.Len(t, exceeds, 1)
	require.Equal(t, []uint64{enforcedHash}, stagesRequestHashes(exceeds[0].Streams))
	require.Equal(t, "enforced", exceeds[0].Streams[0].IngestionPolicy)

	stagesAssertNoWrites(t, ingesters)
	stagesAssertDiscards(t, []stagesDiscard{
		{reason: validation.StreamLimit, policy: "live", samples: 1, bytes: float64(len("live"))},
		{reason: validation.StreamLimit, policy: "enforced", samples: 1, bytes: float64(len("enforced"))},
	})
	stagesAssertRejectedPartialWrites(t, d, "", 0)
}

func TestDistributor_PushRateLimitErrorListsTenantBucketFirst(t *testing.T) {
	stagesResetDiscardMetrics(t)

	const (
		defaultLabels = `{app="default"}`
		alphaLabels   = `{app="alpha"}`
		zebraLabels   = `{app="zebra"}`
	)

	rate := datasize.ByteSize(50).MBytes()
	lim := stagesNewLimits()
	lim.IngestionRateStrategy = validation.LocalIngestionRateStrategy
	lim.IngestionRateMB = rate
	lim.IngestionBurstSizeMB = rate
	lim.PolicyStreamMapping = validation.PolicyStreamMapping{
		"alpha": []*validation.PriorityStream{{Selector: alphaLabels, Priority: 1}},
		"zebra": []*validation.PriorityStream{{Selector: zebraLabels, Priority: 1}},
	}
	lim.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"alpha": {IngestionRateMB: &rate, IngestionBurstSizeMB: &rate},
		"zebra": {IngestionRateMB: &rate, IngestionBurstSizeMB: &rate},
	}
	require.NoError(t, lim.Validate())

	distributors, ingesters := prepare(t, 1, 3, lim, nil)
	d := distributors[0]

	expectedDetail := `the tenant default (limit: 50 bytes/sec) ingesting 1 lines totaling 60 bytes; ` +
		`policy "alpha" (limit: 50 bytes/sec) ingesting 1 lines totaling 70 bytes; ` +
		`policy "zebra" (limit: 50 bytes/sec) ingesting 1 lines totaling 80 bytes`
	expectedErr := httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedMultiErrorMsg, "test", expectedDetail)

	// Twice, as the buckets are kept in a map: the order must not follow its iteration order.
	for i := 0; i < 2; i++ {
		request := makeWriteRequestWithLabels(1, 60, []string{defaultLabels}, false, false, false)
		request.Streams = append(request.Streams,
			makeWriteRequestWithLabels(1, 70, []string{alphaLabels}, false, false, false).Streams...,
		)
		request.Streams = append(request.Streams,
			makeWriteRequestWithLabels(1, 80, []string{zebraLabels}, false, false, false).Streams...,
		)

		resp, err := d.Push(user.InjectOrgID(context.Background(), "test"), request)
		require.Nil(t, resp)
		require.Equal(t, expectedErr, err)
	}

	stagesAssertNoWrites(t, ingesters)
	stagesAssertDiscards(t, []stagesDiscard{
		{reason: validation.RateLimited, samples: 2, bytes: 120},
		{reason: validation.RateLimited, policy: "alpha", samples: 2, bytes: 140},
		{reason: validation.RateLimited, policy: "zebra", samples: 2, bytes: 160},
	})
	stagesAssertRejectedPartialWrites(t, d, "", 0)
}
