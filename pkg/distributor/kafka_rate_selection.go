package distributor

import (
	"crypto/rand"
	"iter"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const kafkaRateSelectionGracePeriod = 5 * time.Second

type kafkaReplicaSelection struct {
	instanceID, sessionID string
	graceStartedAt        time.Time
	lastSeen              time.Time
}

type kafkaRateSelector struct {
	partitions        map[int32]*kafkaReplicaSelection
	seed              uint64
	decisions         *prometheus.CounterVec
	rejectedSnapshots *prometheus.CounterVec
	legacyFallbacks   prometheus.Counter
}

func newKafkaRateSelector(reg prometheus.Registerer) *kafkaRateSelector {
	return &kafkaRateSelector{
		partitions: map[int32]*kafkaReplicaSelection{},
		seed:       xxhash.Sum64String(rand.Text()),
		decisions: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_rate_store_kafka_selections_total",
			Help: "Rate selection decisions for Kafka partitions with eligible snapshots.",
		}, []string{"result"}),
		rejectedSnapshots: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_rate_store_kafka_snapshot_drops_total",
			Help: "Kafka rate snapshots excluded before replica selection.",
		}, []string{"reason"}),
		legacyFallbacks: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_rate_store_kafka_legacy_refreshes_total",
			Help: "Rate refreshes using the legacy maximum because a response lacks Kafka metadata or replica identity.",
		}),
	}
}

// selectRates returns read-only rates, borrowing snapshots unless aggregation needs a copy.
func (s *kafkaRateSelector) selectRates(responses []*logproto.StreamRatesResponse, now time.Time) perTenantStreamRate {
	for id, state := range s.partitions {
		if now.Sub(state.lastSeen) > 10*time.Minute {
			delete(s.partitions, id)
		}
	}
	// Without partition metadata, summing responses could double-count replicas.
	for _, r := range responses {
		if r != nil && (r.Kafka == nil || r.Kafka.InstanceID == "" || r.Kafka.SessionID == "") {
			s.legacyFallbacks.Inc()
			return maximumKafkaRates(responses)
		}
	}
	result := perTenantStreamRate{}
	for partitionID, candidates := range s.groupReadySnapshots(responses) {
		selected, decision := s.selectReplica(partitionID, candidates, now)
		s.decisions.WithLabelValues(decision).Inc()
		if selected == nil {
			addKafkaFallbackRates(result, maximumKafkaRates(candidates))
			continue
		}

		addKafkaSelectedRates(result, selected.StreamRates)
	}
	return result
}

func (s *kafkaRateSelector) groupReadySnapshots(responses []*logproto.StreamRatesResponse) map[int32][]*logproto.StreamRatesResponse {
	groups := map[int32][]*logproto.StreamRatesResponse{}
	for _, r := range responses {
		if r == nil {
			continue
		}
		m := r.Kafka
		if !m.Ready {
			s.rejectedSnapshots.WithLabelValues("startup").Inc()
			continue
		}
		groups[m.PartitionID] = append(groups[m.PartitionID], r)
	}
	return groups
}

// selectReplica avoids frequent switches by keeping the current replica while it is
// close to the leader and allowing a grace period when it falls behind or becomes unavailable.
// It returns nil for maximum-rate fallback during grace, when no progress is valid,
// or when all usable replicas lag.
func (s *kafkaRateSelector) selectReplica(partitionID int32, candidates []*logproto.StreamRatesResponse, now time.Time) (*logproto.StreamRatesResponse, string) {
	state := s.partitions[partitionID]
	if state == nil {
		state = &kafkaReplicaSelection{}
		s.partitions[partitionID] = state
	}
	state.lastSeen = now
	var leading, previous *logproto.StreamRatesResponse
	allBehind := true
	for _, r := range candidates {
		m := r.Kafka
		if !m.ProgressValid || m.StartOffset > m.EndOffset || m.EndOffset > m.HighWatermark {
			continue
		}
		// A replica is close enough if its backlog is at most twice its progress in the last sample.
		if m.HighWatermark-m.EndOffset <= 2*(m.EndOffset-m.StartOffset) {
			allBehind = false
		}
		if leading == nil || m.EndOffset > leading.Kafka.EndOffset ||
			(m.EndOffset == leading.Kafka.EndOffset && s.preference(m.InstanceID) > s.preference(leading.Kafka.InstanceID)) {
			leading = r
		}
		// Consider the previous selection only if the process hasn't restarted.
		if m.InstanceID == state.instanceID && m.SessionID == state.sessionID {
			previous = r
		}
	}
	if leading == nil {
		return nil, "invalid_progress"
	}

	selected := leading
	inGracePeriod := false
	// Avoid frequent replica switches: keep the current replica if it has reached ahead of the leader's sample start offset.
	if previous != nil && previous.Kafka.EndOffset >= leading.Kafka.StartOffset {
		selected = previous
	} else if state.instanceID != "" {
		// Allow brief lag or missing responses to recover before switching; do not restart the grace timer.
		if state.graceStartedAt.IsZero() {
			state.graceStartedAt = now
		}
		inGracePeriod = now.Sub(state.graceStartedAt) < kafkaRateSelectionGracePeriod
	}
	if !inGracePeriod {
		state.instanceID = selected.Kafka.InstanceID
		state.sessionID = selected.Kafka.SessionID
		state.graceStartedAt = time.Time{}
	}
	if allBehind {
		return nil, "lag"
	}
	if inGracePeriod {
		return nil, "grace"
	}
	return selected, "selected"
}

// preference breaks equal-offset ties consistently within a distributor, while the
// random seed lets different distributors prefer different replicas.
func (s *kafkaRateSelector) preference(instanceID string) uint64 {
	return xxhash.Sum64String(instanceID) ^ s.seed
}

func maximumKafkaRates(responses []*logproto.StreamRatesResponse) perTenantStreamRate {
	out := perTenantStreamRate{}
	for _, response := range responses {
		if response == nil {
			continue
		}
		for _, r := range response.StreamRates {
			if out[r.Tenant] == nil {
				out[r.Tenant] = map[uint64]*logproto.StreamRate{}
			}
			if old := out[r.Tenant][r.StreamHash]; old == nil || r.Rate > old.Rate {
				out[r.Tenant][r.StreamHash] = r
			}
		}
	}
	return out
}

// addKafkaFallbackRates adds fallback rates without modifying their snapshots.
func addKafkaFallbackRates(out, partition perTenantStreamRate) {
	for _, rates := range partition {
		addKafkaStreamRates(out, maps.Values(rates), len(rates))
	}
}

// addKafkaSelectedRates adds a selected replica's rates without modifying its snapshot.
func addKafkaSelectedRates(out perTenantStreamRate, rates []*logproto.StreamRate) {
	addKafkaStreamRates(out, slices.Values(rates), len(rates))
}

// addKafkaStreamRates borrows immutable rates, copying only when summing across partitions.
func addKafkaStreamRates(out perTenantStreamRate, rates iter.Seq[*logproto.StreamRate], count int) {
	var copies []logproto.StreamRate
	for current := range rates {
		tenantRates := out[current.Tenant]
		if tenantRates == nil {
			tenantRates = map[uint64]*logproto.StreamRate{}
			out[current.Tenant] = tenantRates
		}
		if old := tenantRates[current.StreamHash]; old != nil {
			// Allocate once per batch with collisions, rather than once per stream.
			if copies == nil {
				copies = make([]logproto.StreamRate, 0, count)
			}
			copies = append(copies, *current)
			current = &copies[len(copies)-1]
			if current.Rate > 0 && old.Rate > math.MaxInt64-current.Rate {
				current.Rate = math.MaxInt64
			} else {
				current.Rate += old.Rate
			}
			current.Pushes = uint32(min(uint64(old.Pushes)+uint64(current.Pushes), uint64(math.MaxUint32)))
		}
		tenantRates[current.StreamHash] = current
	}
}
