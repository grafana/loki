package distributor

import (
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

// streamSharder shards the streams that passed validation, each with the shard-streams config of
// its policy (see PolicyShardStreams), so a policy can override the tenant sharding behavior (e.g.
// toggle time sharding or use a different desired_rate). streams holds the streams ready to be
// written.
//
// Tenants can ask the limits service for a shard count, see
// shardstreams.Config.LimitsServiceStreamShardingMode. Their streams are collected as candidates
// and handled in one call after validation. Shadow-mode streams are already sharded by the local
// rate store and are only compared against the service's answer. Live-mode streams are left
// unsharded here: enforceStreamLimits shards them with the count the service returns, so they are
// not in streams. The mode is per tenant, so at most one of the two is used per request.
type streamSharder struct {
	d        *Distributor
	tenantID string
	now      time.Time

	streams                          []KeyedStream
	shadowCandidates, liveCandidates []limitsServiceShardCandidate

	// shardStreamsEnabledSeen/shardStreamsDisabledSeen track whether this
	// push mixes streams whose resolved shardStreamsCfg.Enabled differs for
	// this tenant. A tenant's policies are expected to agree on whether
	// stream sharding is enabled; a stream with it disabled skips the limits
	// service and is enforced by EnforceLimits instead, splitting
	// max_global_streams_per_user enforcement between two independent
	// trackers. This is only a concern while the limits service is enabled.
	shardStreamsEnabledSeen, shardStreamsDisabledSeen     bool
	shardStreamsEnabledPolicy, shardStreamsDisabledPolicy string
}

func newStreamSharder(d *Distributor, tenantID string, now time.Time, numStreams int) streamSharder {
	// Sized for one KeyedStream per pushed stream; sharding can add more.
	return streamSharder{d: d, tenantID: tenantID, now: now, streams: make([]KeyedStream, 0, numStreams)}
}

// shard time-shards stream if time sharding applies to it, then shards it by rate.
func (s *streamSharder) shard(stream logproto.InternalStreamAdapter, lbls labels.Labels, pushSize int, policy string, shardStreamsCfg shardstreams.Config) {
	// Backfill streams implement time sharding on the client side (via the
	// constants.BackfillShardLabel), so Loki's own time sharding is disabled for them to avoid
	// exploding stream cardinality. Rate-based sharding still applies.
	if !shardStreamsCfg.TimeShardingEnabled || lbls.Has(constants.BackfillLabel) {
		s.shardByRate(stream, lbls, pushSize, policy, shardStreamsCfg)
		return
	}

	ignoreRecentFrom := s.now.Add(-shardStreamsCfg.TimeShardingIgnoreRecent)
	timeShards, sharded := timeShardNested(stream, lbls, s.d.ingesterCfg.MaxChunkAge/2, ignoreRecentFrom)
	if !sharded {
		s.shardByRate(stream, lbls, pushSize, policy, shardStreamsCfg)
		return
	}

	for _, ts := range timeShards {
		s.shardByRate(ts.stream, ts.lbls, ts.expandedSize, policy, shardStreamsCfg)
	}
}

// shardByRate shards stream by rate and adds the result to streams, unless the limits service
// decides the shard count. It then becomes a candidate, see streamSharder.
func (s *streamSharder) shardByRate(stream logproto.InternalStreamAdapter, lbls labels.Labels, pushSize int, policy string, shardStreamsCfg shardstreams.Config) {
	d, tenantID := s.d, s.tenantID

	if d.cfg.IngestLimitsEnabled {
		if shardStreamsCfg.Enabled {
			if !s.shardStreamsEnabledSeen {
				s.shardStreamsEnabledSeen = true
				s.shardStreamsEnabledPolicy = policy
			}
		} else if !s.shardStreamsDisabledSeen {
			s.shardStreamsDisabledSeen = true
			s.shardStreamsDisabledPolicy = policy
		}
	}

	// Shortcut when stream sharding is not enabled
	if !shardStreamsCfg.Enabled {
		s.streams = append(s.streams, KeyedStream{
			HashKey:        lokiring.TokenFor(tenantID, stream.Labels),
			HashKeyNoShard: stream.Hash,
			Stream:         stream,
			Policy:         policy,
		})
		return
	}

	// Limits-service stream sharding requires the ingest limits to be enabled.
	// Shortcut when limits-service sharding is disabled. Treat unset mode also as "disabled".
	mode := shardStreamsCfg.LimitsServiceStreamShardingMode
	if !d.cfg.IngestLimitsEnabled || mode == shardstreams.LimitsServiceStreamShardingModeDisabled || mode == "" {
		sharded, _ := d.shardStream(stream, lbls, pushSize, tenantID, policy, shardStreamsCfg)
		s.streams = append(s.streams, sharded...)
		return
	}

	// rateStoreShards is shardCountFor's recommendation rather than the
	// number of streams sharding produces, which shardNested limits to the
	// number of entries and which would look like a difference of opinion
	// for a small push on a hot stream. pushSize is passed on as it is, so
	// that the limits service sees the same size the rate store used.
	logger := log.With(util_log.WithUserID(tenantID, d.logger), "stream", stream.Labels)
	rateStoreRate, _ := d.rateStore.RateFor(tenantID, stream.Hash)
	candidate := limitsServiceShardCandidate{
		stream:          stream,
		labels:          lbls,
		policy:          policy,
		shardStreamsCfg: shardStreamsCfg,
		rateStoreShards: d.shardCountFor(logger, stream, pushSize, tenantID, shardStreamsCfg),
		rateStoreRate:   rateStoreRate,
		totalSize:       uint64(pushSize),
	}

	switch mode {
	case shardstreams.LimitsServiceStreamShardingModeLive:
		s.liveCandidates = append(s.liveCandidates, candidate)
	case shardstreams.LimitsServiceStreamShardingModeShadow:
		s.streams = append(s.streams, d.shardStreamToCount(stream, lbls, tenantID, policy, shardStreamsCfg, candidate.rateStoreShards)...)
		s.shadowCandidates = append(s.shadowCandidates, candidate)
	}
}

// warnMixedShardStreams logs a warning when the push mixed policies with stream sharding enabled
// and disabled, see streamSharder.shardStreamsEnabledSeen.
func (s *streamSharder) warnMixedShardStreams() {
	if !s.shardStreamsEnabledSeen || !s.shardStreamsDisabledSeen {
		return
	}
	level.Warn(s.d.logger).Log(
		"msg", "tenant has shard_streams.enabled true for one policy and false for another in the same push; this is not expected and splits max_global_streams_per_user enforcement between the limits service and EnforceLimits",
		"tenant", s.tenantID,
		"enabled_policy", s.shardStreamsEnabledPolicy,
		"disabled_policy", s.shardStreamsDisabledPolicy,
	)
}
