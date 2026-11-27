package distributor

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	client2 "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func rateSnapshot(partitionID int32, instanceID string, rate int64) *logproto.StreamRatesResponse {
	return &logproto.StreamRatesResponse{Kafka: &logproto.KafkaRateSnapshot{PartitionID: partitionID, InstanceID: instanceID, SessionID: "session-1", StartOffset: 10, EndOffset: 20, HighWatermark: 20, Ready: true, ProgressValid: true}, StreamRates: []*logproto.StreamRate{{Tenant: "tenant", StreamHash: 9, StreamHashNoShard: 7, Rate: rate, Pushes: 1}}}
}

// laggingRateSnapshots places replica a behind replica b's completed interval.
func laggingRateSnapshots(partitionID int32, rateA, rateB int64) (*logproto.StreamRatesResponse, *logproto.StreamRatesResponse) {
	a, b := rateSnapshot(partitionID, "a", rateA), rateSnapshot(partitionID, "b", rateB)
	b.Kafka.StartOffset = 30
	b.Kafka.EndOffset = 40
	b.Kafka.HighWatermark = 40
	return a, b
}

func TestKafkaRateSelection(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name    string
		prepare func(a, b *logproto.StreamRatesResponse)
		want    int64
	}{
		{"overlapping preferred interval", func(_, b *logproto.StreamRatesResponse) { b.Kafka.EndOffset = 21; b.Kafka.HighWatermark = 21 }, 80},
		{"startup window excluded", func(_, b *logproto.StreamRatesResponse) { b.Kafka.Ready = false }, 80},
		{"all startup windows excluded", func(a, b *logproto.StreamRatesResponse) { a.Kafka.Ready = false; b.Kafka.Ready = false }, 0},
		{"legacy response preserves maximum", func(_, b *logproto.StreamRatesResponse) { b.Kafka = nil }, 200},
		{"legacy fallback keeps startup inputs", func(a, b *logproto.StreamRatesResponse) { a.Kafka = nil; b.Kafka.Ready = false }, 200},
		{"failed replica excluded", func(_, b *logproto.StreamRatesResponse) { b.Kafka.ProgressValid = false }, 80},
		{"no valid progress preserves maximum", func(a, b *logproto.StreamRatesResponse) { a.Kafka.ProgressValid = false; b.Kafka.ProgressValid = false }, 200},
		{"both replicas behind preserve maximum", func(a, b *logproto.StreamRatesResponse) { a.Kafka.HighWatermark = 100; b.Kafka.HighWatermark = 100 }, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newKafkaRateSelector(prometheus.NewRegistry())
			s.partitions[0] = &kafkaReplicaSelection{instanceID: "a", sessionID: "session-1", lastSeen: now}
			a, b := rateSnapshot(0, "a", 80), rateSnapshot(0, "b", 200)
			a.StreamRates[0].Pushes = 3
			b.StreamRates[0].Pushes = 3
			tc.prepare(a, b)
			got := s.selectRates([]*logproto.StreamRatesResponse{a, nil, b}, now)
			var rate int64
			if r := got["tenant"][9]; r != nil {
				rate = r.Rate
				require.EqualValues(t, 3, r.Pushes, "selection must preserve the reported push count")
			}
			require.Equal(t, tc.want, rate)
			require.EqualValues(t, 80, a.StreamRates[0].Rate, "selection must not modify a published response")
		})
	}
}

func TestKafkaRateSelectionPartitionAggregation(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name    string
		prepare func(a, b *logproto.StreamRatesResponse)
		want    int64
	}{
		{"selected", func(_, _ *logproto.StreamRatesResponse) {}, 100},
		{"grace fallback", func(_, b *logproto.StreamRatesResponse) {
			b.Kafka.StartOffset = 30
			b.Kafka.EndOffset = 40
			b.Kafka.HighWatermark = 40
		}, 180},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newKafkaRateSelector(prometheus.NewRegistry())
			responses := []*logproto.StreamRatesResponse{
				rateSnapshot(0, "a", 60), rateSnapshot(0, "b", 100),
				rateSnapshot(1, "a", 40), rateSnapshot(1, "b", 80),
			}
			for i := 0; i < len(responses); i += 2 {
				a, b := responses[i], responses[i+1]
				s.partitions[a.Kafka.PartitionID] = &kafkaReplicaSelection{instanceID: "a", sessionID: "session-1", lastSeen: now}
				tc.prepare(a, b)
			}

			before := make([][]logproto.StreamRate, len(responses))
			for i, r := range responses {
				// Two tenants and hashes exercise both levels of the aggregation maps.
				other := *r.StreamRates[0]
				other.Tenant = "other"
				other.StreamHash = 10
				r.StreamRates = append(r.StreamRates, &other)
				for _, v := range r.StreamRates {
					before[i] = append(before[i], *v)
				}
			}
			got := s.selectRates(responses, now)
			require.Equal(t, tc.want, got["tenant"][9].Rate)
			require.Equal(t, tc.want, got["other"][10].Rate)
			require.EqualValues(t, 2, got["tenant"][9].Pushes)
			require.EqualValues(t, 2, got["other"][10].Pushes)
			// Cross-partition sums own their storage, including fallback contributions.
			for _, rates := range got {
				for _, v := range rates {
					v.Rate = 0
					v.Pushes = 0
				}
			}
			for i, r := range responses {
				for j, v := range r.StreamRates {
					require.Equal(t, before[i][j], *v)
				}
			}
		})
	}
}

func TestKafkaRateSelectionPartitionSum(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rate       int64
		pushes     uint32
		wantRate   int64
		wantPushes uint32
	}{
		{name: "three partitions", rate: 10, pushes: 2, wantRate: 30, wantPushes: 6},
		{name: "saturated sums", rate: math.MaxInt64 / 2, pushes: math.MaxUint32 / 2, wantRate: math.MaxInt64, wantPushes: math.MaxUint32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			s := newKafkaRateSelector(prometheus.NewRegistry())
			var responses []*logproto.StreamRatesResponse
			for partitionID := int32(0); partitionID < 3; partitionID++ {
				r := rateSnapshot(partitionID, "a", tc.rate)
				r.StreamRates[0].Pushes = tc.pushes
				responses = append(responses, r)
			}
			got := s.selectRates(responses, now)["tenant"][9]
			require.Equal(t, tc.wantRate, got.Rate)
			require.Equal(t, tc.wantPushes, got.Pushes)
			for _, r := range responses {
				require.Equal(t, tc.rate, r.StreamRates[0].Rate)
				require.Equal(t, tc.pushes, r.StreamRates[0].Pushes)
			}
		})
	}
}

func TestKafkaRateSelectionGrace(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name         string
		prepare      func(a, b *logproto.StreamRatesResponse) []*logproto.StreamRatesResponse
		fallbackRate int64
	}{
		{"lagging", func(a, b *logproto.StreamRatesResponse) []*logproto.StreamRatesResponse {
			return []*logproto.StreamRatesResponse{a, b}
		}, 200},
		{"missing", func(_, b *logproto.StreamRatesResponse) []*logproto.StreamRatesResponse {
			return []*logproto.StreamRatesResponse{b}
		}, 80},
		{"startup", func(a, b *logproto.StreamRatesResponse) []*logproto.StreamRatesResponse {
			a.Kafka.Ready = false
			return []*logproto.StreamRatesResponse{a, b}
		}, 80},
		{"invalid progress", func(a, b *logproto.StreamRatesResponse) []*logproto.StreamRatesResponse {
			a.Kafka.ProgressValid = false
			return []*logproto.StreamRatesResponse{a, b}
		}, 200},
		{"restarted with overlapping offsets", func(a, b *logproto.StreamRatesResponse) []*logproto.StreamRatesResponse {
			a.Kafka.SessionID = "session-2"
			// Overlap must not preserve a selection from the previous process.
			b.Kafka.StartOffset = 10
			b.Kafka.EndOffset = 21
			b.Kafka.HighWatermark = 21
			return []*logproto.StreamRatesResponse{a, b}
		}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newKafkaRateSelector(prometheus.NewRegistry())
			s.partitions[0] = &kafkaReplicaSelection{instanceID: "a", sessionID: "session-1", lastSeen: now}
			for _, elapsed := range []time.Duration{0, kafkaRateSelectionGracePeriod - time.Nanosecond, kafkaRateSelectionGracePeriod, kafkaRateSelectionGracePeriod + time.Second} {
				tick := now.Add(elapsed)
				a, b := laggingRateSnapshots(0, 200, 80)
				got := s.selectRates(tc.prepare(a, b), tick)
				state := s.partitions[0]
				if elapsed < kafkaRateSelectionGracePeriod {
					require.Equal(t, tc.fallbackRate, got["tenant"][9].Rate)
					require.Equal(t, "a", state.instanceID)
					require.Equal(t, "session-1", state.sessionID)
					require.Equal(t, now, state.graceStartedAt, "refreshes must not extend the deadline")
				} else {
					require.EqualValues(t, 80, got["tenant"][9].Rate, "handoff must not start another fallback period")
					require.Equal(t, "b", state.instanceID)
					require.True(t, state.graceStartedAt.IsZero())
				}
				require.EqualValues(t, 200, a.StreamRates[0].Rate)
			}
			require.Equal(t, 2.0, testutil.ToFloat64(s.decisions.WithLabelValues("grace")))
			require.Equal(t, 2.0, testutil.ToFloat64(s.decisions.WithLabelValues("selected")))
		})
	}
}

func TestKafkaRateSelectionRecovery(t *testing.T) {
	now := time.Now()
	for _, delay := range []time.Duration{time.Second, kafkaRateSelectionGracePeriod, kafkaRateSelectionGracePeriod + time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			s := newKafkaRateSelector(prometheus.NewRegistry())
			s.partitions[0] = &kafkaReplicaSelection{instanceID: "a", sessionID: "session-1", lastSeen: now}
			responses := func(lagging bool) []*logproto.StreamRatesResponse {
				a, b := rateSnapshot(0, "a", 80), rateSnapshot(0, "b", 200)
				if lagging {
					a, b = laggingRateSnapshots(0, 80, 200)
				}
				return []*logproto.StreamRatesResponse{a, b}
			}
			require.EqualValues(t, 200, s.selectRates(responses(true), now)["tenant"][9].Rate)
			recovery := now.Add(delay)
			require.EqualValues(t, 80, s.selectRates(responses(false), recovery)["tenant"][9].Rate, "recovery cancels fallback even at or after expiry")
			require.Equal(t, "a", s.partitions[0].instanceID)
			require.True(t, s.partitions[0].graceStartedAt.IsZero())
			next := recovery.Add(time.Second)
			s.selectRates(responses(true), next)
			require.Equal(t, next, s.partitions[0].graceStartedAt, "a new episode needs a new grace period")
			beforeExpiry := next.Add(kafkaRateSelectionGracePeriod - time.Nanosecond)
			s.selectRates(responses(true), beforeExpiry)
			require.Equal(t, "a", s.partitions[0].instanceID)
			expiry := next.Add(kafkaRateSelectionGracePeriod)
			s.selectRates(responses(true), expiry)
			require.Equal(t, "b", s.partitions[0].instanceID)
		})
	}
}

func TestKafkaRateSelectionGracePreservesLagGuard(t *testing.T) {
	now := time.Now()
	for _, recovered := range []bool{false, true} {
		t.Run(fmt.Sprintf("recovered=%t", recovered), func(t *testing.T) {
			s := newKafkaRateSelector(prometheus.NewRegistry())
			s.partitions[0] = &kafkaReplicaSelection{instanceID: "a", sessionID: "session-1", lastSeen: now}
			a, b := laggingRateSnapshots(0, 80, 200)
			s.selectRates([]*logproto.StreamRatesResponse{a, b}, now)
			tick := now.Add(kafkaRateSelectionGracePeriod)
			a, b = rateSnapshot(0, "a", 80), rateSnapshot(0, "b", 200)
			if !recovered {
				b.Kafka.StartOffset = 30
				b.Kafka.EndOffset = 40
			}
			a.Kafka.HighWatermark = 100
			b.Kafka.HighWatermark = 100
			require.EqualValues(t, 200, s.selectRates([]*logproto.StreamRatesResponse{a, b}, tick)["tenant"][9].Rate)
			require.Equal(t, 1.0, testutil.ToFloat64(s.decisions.WithLabelValues("lag")))
			require.True(t, s.partitions[0].graceStartedAt.IsZero())
			if recovered {
				require.Equal(t, "a", s.partitions[0].instanceID)
			} else {
				require.Equal(t, "b", s.partitions[0].instanceID)
			}
		})
	}
}

func TestKafkaRateSelectionPartitionGraceAndExpiry(t *testing.T) {
	now := time.Now()
	s := newKafkaRateSelector(prometheus.NewRegistry())
	for _, partitionID := range []int32{0, 1} {
		a := rateSnapshot(partitionID, "a", 80)
		require.EqualValues(t, 80, s.selectRates([]*logproto.StreamRatesResponse{a}, now)["tenant"][9].Rate, "initial selection has no grace period")
	}
	responses := func(partitions ...int32) []*logproto.StreamRatesResponse {
		var out []*logproto.StreamRatesResponse
		for _, partitionID := range partitions {
			a, b := laggingRateSnapshots(partitionID, 200, 80)
			out = append(out, a, b)
		}
		return out
	}
	s.selectRates(responses(0), now)
	later := now.Add(3 * time.Second)
	s.selectRates(responses(0, 1), later)
	expiry := now.Add(kafkaRateSelectionGracePeriod)
	require.EqualValues(t, 280, s.selectRates(responses(0, 1), expiry)["tenant"][9].Rate, "selected and fallback partition contributions must both be included")
	require.Equal(t, "b", s.partitions[0].instanceID)
	require.Equal(t, "a", s.partitions[1].instanceID)
	require.Equal(t, later, s.partitions[1].graceStartedAt)
	s.selectRates(nil, now.Add(11*time.Minute))
	require.Empty(t, s.partitions)
}

func TestKafkaRateStoreIngestionMode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		kafkaEnabled bool
		wantRate     int64
	}{
		{name: "direct ingestion keeps maximum", wantRate: 200},
		{name: "Kafka ingestion selects a replica", kafkaEnabled: true, wantRate: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewRateStore(RateStoreConfig{}, tc.kafkaEnabled, newFakeRing(), newFakeClientPool(), &fakeOverrides{enabled: true}, prometheus.NewRegistry())
			a, b := rateSnapshot(0, "a", 80), rateSnapshot(0, "b", 200)
			// The leading replica has the lower rate, so maximum selection would differ.
			a.Kafka.EndOffset, a.Kafka.HighWatermark = 21, 21
			responses := make(chan *logproto.StreamRatesResponse, 2)
			responses <- a
			responses <- b
			rates, _ := store.ratesPerStream(responses, 2)
			require.Equal(t, tc.wantRate, rates["tenant"][9].Rate)
		})
	}
}

func TestKafkaRateStoreSelectionKeepsPhysicalShardMetrics(t *testing.T) {
	tc := setup(true)
	now := time.Now()
	tc.rateStore.kafkaSelector = newKafkaRateSelector(tc.reg)
	tc.rateStore.kafkaSelector.partitions[0] = &kafkaReplicaSelection{instanceID: "a", sessionID: "session-1", lastSeen: now}
	a, b := rateSnapshot(0, "a", 80), rateSnapshot(0, "b", 200)
	b.StreamRates = append(b.StreamRates, &logproto.StreamRate{Tenant: "tenant", StreamHash: 10, StreamHashNoShard: 7, Rate: 500, Pushes: 1})
	tc.ring.replicationSet = ring.ReplicationSet{Instances: []ring.InstanceDesc{{Addr: "a"}, {Addr: "b"}}}
	tc.clientPool.clients = map[string]client.PoolClient{
		"a": client2.ClosableHealthAndIngesterClient{StreamDataClient: &fakeStreamDataClient{resp: a}},
		"b": client2.ClosableHealthAndIngesterClient{StreamDataClient: &fakeStreamDataClient{resp: b}},
	}
	require.NoError(t, tc.rateStore.updateAllRates(context.Background()))
	rate, _ := tc.rateStore.RateFor("tenant", 7)
	require.EqualValues(t, 80, rate)
	require.EqualValues(t, 2, tc.rateStore.rates["tenant"][7].shards)
	// Selection retains the existing outer EWMA.
	a.StreamRates[0].Rate = 40
	require.NoError(t, tc.rateStore.updateAllRates(context.Background()))
	rate, _ = tc.rateStore.RateFor("tenant", 7)
	require.EqualValues(t, 64, rate)

	// An idle selected replica must not hide shard IDs seen on another replica.
	a.StreamRates = nil
	b.StreamRates = append(b.StreamRates, &logproto.StreamRate{Tenant: "tenant", StreamHash: 11, StreamHashNoShard: 8, Rate: 100, Pushes: 1})
	require.NoError(t, tc.rateStore.updateAllRates(context.Background()))
	rate, _ = tc.rateStore.RateFor("tenant", 7)
	require.EqualValues(t, 38, rate, "missing selected rates still decay through the existing EWMA")
	require.EqualValues(t, 2, tc.rateStore.rates["tenant"][7].shards)
	require.EqualValues(t, 1, tc.rateStore.rates["tenant"][8].shards)
	rate, _ = tc.rateStore.RateFor("tenant", 8)
	require.Zero(t, rate, "observing shard IDs must not import the unselected replica's rate")

	// Counts belong to this refresh, even after a previous refresh observed more shards.
	a.StreamRates = rateSnapshot(0, "a", 40).StreamRates
	b.StreamRates = nil
	require.NoError(t, tc.rateStore.updateAllRates(context.Background()))
	require.EqualValues(t, 1, tc.rateStore.rates["tenant"][7].shards)
	rate, _ = tc.rateStore.RateFor("tenant", 7)
	require.EqualValues(t, 38, rate)

}

func BenchmarkKafkaRateStore(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("enabled=%t", enabled), func(b *testing.B) {
			tc := setup(true)
			if enabled {
				tc.rateStore.kafkaSelector = newKafkaRateSelector(tc.reg)
			}
			responses := make([]*logproto.StreamRatesResponse, 128)
			for i := range responses {
				instanceID := fmt.Sprintf("replica-%d", i)
				r := rateSnapshot(int32(i/2), instanceID, 0)
				r.StreamRates = nil
				responses[i] = r
				tc.ring.replicationSet.Instances = append(tc.ring.replicationSet.Instances, ring.InstanceDesc{Addr: instanceID})
				tc.clientPool.clients[instanceID] = client2.ClosableHealthAndIngesterClient{StreamDataClient: &fakeStreamDataClient{resp: r}}
			}
			for h := uint64(0); h < 10000; h++ {
				for replica := 0; replica < 2; replica++ {
					r := responses[int(h%64)*2+replica]
					r.StreamRates = append(r.StreamRates, &logproto.StreamRate{Tenant: "tenant", StreamHash: h, StreamHashNoShard: h / 8, Rate: int64(32000 * (1 + (int(h)+replica)%3)), Pushes: 2})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := tc.rateStore.updateAllRates(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkKafkaRatePartitionMerge(b *testing.B) {
	for _, overlap := range []bool{false, true} {
		b.Run(fmt.Sprintf("overlap=%t", overlap), func(b *testing.B) {
			inputs := make([][]*logproto.StreamRate, 3)
			for partitionID := range inputs {
				for stream := 0; stream < 10000; stream++ {
					hash := stream
					if !overlap {
						hash += partitionID * 10000
					}
					inputs[partitionID] = append(inputs[partitionID], &logproto.StreamRate{Tenant: "tenant", StreamHash: uint64(hash), Rate: 100, Pushes: 1})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out := perTenantStreamRate{}
				for _, rates := range inputs {
					addKafkaSelectedRates(out, rates)
				}
			}
		})
	}
}
