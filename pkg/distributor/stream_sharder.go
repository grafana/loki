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

// shardedStreams is the result of shardStreams. streams are written once enforceStreamLimits has
// checked them.
//
// When a tenant asks the limits service for shard counts (see
// shardstreams.Config.LimitsServiceStreamShardingMode), its streams are also collected as
// candidates for one call to the service, after the rate limit. Shadow-mode streams are in streams
// too, sharded by the local rate store, and the service's answer is only compared. Live-mode
// streams are only time-sharded and are not in streams: the service decides their shard count. The
// mode is per tenant, so a push has candidates of one mode at most.
type shardedStreams struct {
	streams                          []KeyedStream
	shadowCandidates, liveCandidates []limitsServiceShardCandidate
}

// shardStreams shards the accepted streams, each with its policy's shard-streams config. Side
// effects: rate sharding a stream into more than one shard advances its shard rotation, a warning is
// logged when the push's policies disagree on whether sharding is enabled, and time sharding sorts
// each scope's entries by timestamp in place, even when it then does not time-shard.
func (d *Distributor) shardStreams(tenantID string, now time.Time, accepted []acceptedStream) shardedStreams {
	sharder := newStreamSharder(d, tenantID, now, len(accepted))
	for _, a := range accepted {
		shardCfg, _ := d.validator.PolicyShardStreams(tenantID, a.policy)
		sharder.shard(a.stream, a.lbs, a.stats.expandedSize, a.policy, shardCfg)
	}
	if sharder.shardStreamsEnabledSeen && sharder.shardStreamsDisabledSeen {
		level.Warn(d.logger).Log(
			"msg", "tenant has shard_streams.enabled true for one policy and false for another in the same push; this is not expected and splits max_global_streams_per_user enforcement between the limits service and EnforceLimits",
			"tenant", tenantID,
			"enabled_policy", sharder.shardStreamsEnabledPolicy,
			"disabled_policy", sharder.shardStreamsDisabledPolicy,
		)
	}
	return shardedStreams{
		streams:          sharder.streams,
		shadowCandidates: sharder.shadowCandidates,
		liveCandidates:   sharder.liveCandidates,
	}
}

type streamSharder struct {
	d        *Distributor
	tenantID string
	now      time.Time

	streams                          []KeyedStream
	shadowCandidates, liveCandidates []limitsServiceShardCandidate

	// The policies seen with sharding enabled and disabled, the first of each, for the warning in
	// shardStreams. A tenant's policies are expected to agree: in live mode, a stream with
	// sharding disabled is limited by EnforceLimits instead of CheckLimitsAndShard, splitting
	// max_global_streams_per_user between two trackers. The warning is logged whenever the ingest
	// limits are enabled, in every mode.
	shardStreamsEnabledSeen, shardStreamsDisabledSeen     bool
	shardStreamsEnabledPolicy, shardStreamsDisabledPolicy string
}

func newStreamSharder(d *Distributor, tenantID string, now time.Time, numStreams int) streamSharder {
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

// shardByRate shards stream with the local rate store's count, or collects it as a candidate for
// the limits service, or both in shadow mode. See shardedStreams.
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

	if !shardStreamsCfg.Enabled {
		s.streams = append(s.streams, KeyedStream{
			HashKey:        lokiring.TokenFor(tenantID, stream.Labels),
			HashKeyNoShard: stream.Hash,
			Stream:         stream,
			Policy:         policy,
		})
		return
	}

	// The limits service only decides shard counts while the ingest limits
	// are enabled. An unset mode means disabled.
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

	// No default: shardstreams.Config.Validate only allows the modes handled
	// here and above.
	switch mode {
	case shardstreams.LimitsServiceStreamShardingModeLive:
		s.liveCandidates = append(s.liveCandidates, candidate)
	case shardstreams.LimitsServiceStreamShardingModeShadow:
		s.streams = append(s.streams, d.shardStreamToCount(stream, lbls, tenantID, policy, shardStreamsCfg, candidate.rateStoreShards)...)
		s.shadowCandidates = append(s.shadowCandidates, candidate)
	}
}
