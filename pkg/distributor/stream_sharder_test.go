package distributor

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
)

const streamSharderTestTenant = "tenant"

func newTestStreamSharder(ingestLimitsEnabled bool, pushRate float64, now time.Time) streamSharder {
	d := &Distributor{
		logger:       log.NewNopLogger(),
		rateStore:    &fakeRateStore{pushRate: pushRate},
		m:            newMetrics(prometheus.NewPedanticRegistry()),
		shardTracker: NewShardTracker(),
	}
	d.cfg.IngestLimitsEnabled = ingestLimitsEnabled
	d.ingesterCfg.MaxChunkAge = 2 * time.Hour
	return newStreamSharder(d, streamSharderTestTenant, now, 1)
}

func newTestShardedStream(lbls labels.Labels, timestamps ...time.Time) logproto.InternalStreamAdapter {
	entries := make([]logproto.Entry, 0, len(timestamps))
	for _, ts := range timestamps {
		entries = append(entries, logproto.Entry{Timestamp: ts, Line: "line"})
	}
	return *logproto.FromStream(logproto.Stream{Labels: lbls.String(), Hash: labels.StableHash(lbls), Entries: entries})
}

func streamLabels(t *testing.T, streams []KeyedStream) []string {
	t.Helper()
	got := make([]string, 0, len(streams))
	for _, s := range streams {
		got = append(got, s.Stream.Labels)
	}
	return got
}

func candidateLabels(candidates []limitsServiceShardCandidate) []string {
	got := make([]string, 0, len(candidates))
	for _, c := range candidates {
		got = append(got, c.stream.Labels)
	}
	return got
}

func TestStreamSharder_ShardByRate(t *testing.T) {
	now := time.Now()
	lbls := labels.FromStrings("app", "a")
	rateShards := make([]string, 0, 4)
	for _, shard := range []string{"0", "1", "2", "3"} {
		rateShards = append(rateShards, labels.FromStrings("app", "a", constants.StreamShardLabel, shard).String())
	}
	sharding := func(mode string) shardstreams.Config {
		return shardstreams.Config{Enabled: true, DesiredRate: loki_flagext.ByteSize(100), LimitsServiceStreamShardingMode: mode}
	}

	// With a push rate of 1 and no recorded rate, a 400 byte push at a desired rate of 100 bytes
	// asks for 4 shards, one per entry.
	tests := []struct {
		name                string
		ingestLimitsEnabled bool
		pushRate            float64
		cfg                 shardstreams.Config
		wantStreams         []string
		wantLive            []string
		wantShadow          []string
	}{
		{
			name:        "sharding disabled leaves the stream whole",
			pushRate:    1,
			cfg:         shardstreams.Config{DesiredRate: loki_flagext.ByteSize(100)},
			wantStreams: []string{lbls.String()},
		},
		{
			name:        "a first push is not sharded",
			pushRate:    0,
			cfg:         sharding(""),
			wantStreams: []string{lbls.String()},
		},
		{
			name:        "a stream over its desired rate is sharded",
			pushRate:    1,
			cfg:         sharding(""),
			wantStreams: rateShards,
		},
		{
			name:                "live mode leaves the shard count to the limits service",
			ingestLimitsEnabled: true,
			pushRate:            1,
			cfg:                 sharding(shardstreams.LimitsServiceStreamShardingModeLive),
			wantLive:            []string{lbls.String()},
		},
		{
			name:        "live mode without the ingest limits shards with the rate store",
			pushRate:    1,
			cfg:         sharding(shardstreams.LimitsServiceStreamShardingModeLive),
			wantStreams: rateShards,
		},
		{
			name:                "shadow mode shards with the rate store and asks the limits service too",
			ingestLimitsEnabled: true,
			pushRate:            1,
			cfg:                 sharding(shardstreams.LimitsServiceStreamShardingModeShadow),
			wantStreams:         rateShards,
			wantShadow:          []string{lbls.String()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStreamSharder(tc.ingestLimitsEnabled, tc.pushRate, now)
			stream := newTestShardedStream(lbls, now, now, now, now)

			s.shard(stream, lbls, 400, "policy", tc.cfg)

			require.ElementsMatch(t, tc.wantStreams, streamLabels(t, s.streams))
			require.ElementsMatch(t, tc.wantLive, candidateLabels(s.liveCandidates))
			require.ElementsMatch(t, tc.wantShadow, candidateLabels(s.shadowCandidates))
			for _, ks := range s.streams {
				require.Equal(t, "policy", ks.Policy)
				require.Equal(t, stream.Hash, ks.HashKeyNoShard)
			}
			for _, c := range append(s.liveCandidates, s.shadowCandidates...) {
				require.Equal(t, 4, c.rateStoreShards)
			}

			// Sharding into more than one stream advances the stream's shard rotation.
			wantLastShard := 0
			if len(s.streams) > 1 {
				wantLastShard = len(s.streams)
			}
			require.Equal(t, wantLastShard, s.d.shardTracker.LastShardNum(streamSharderTestTenant, stream.Hash))
		})
	}
}

func TestStreamSharder_TimeSharding(t *testing.T) {
	now := time.Now()
	lbls := labels.FromStrings("app", "a")
	// MaxChunkAge/2 is one hour, so these entries fall into two time shards.
	old := now.Add(-5 * time.Hour).Truncate(time.Hour)
	timeShardingCfg := func(mode string) shardstreams.Config {
		return shardstreams.Config{
			Enabled:                         true,
			TimeShardingEnabled:             true,
			TimeShardingIgnoreRecent:        40 * time.Minute,
			DesiredRate:                     loki_flagext.ByteSize(100),
			LimitsServiceStreamShardingMode: mode,
		}
	}

	t.Run("old entries are time-sharded", func(t *testing.T) {
		s := newTestStreamSharder(false, 0, now)
		s.shard(newTestShardedStream(lbls, old, old.Add(90*time.Minute)), lbls, 8, "policy", timeShardingCfg(""))

		require.Len(t, s.streams, 2)
		for _, ks := range s.streams {
			shardLbls, err := syntax.ParseLabels(ks.Stream.Labels)
			require.NoError(t, err)
			require.True(t, shardLbls.Has(constants.TimeShardLabel), ks.Stream.Labels)
		}
	})

	t.Run("live-mode candidates are time shards", func(t *testing.T) {
		s := newTestStreamSharder(true, 0, now)
		s.shard(newTestShardedStream(lbls, old, old.Add(90*time.Minute)), lbls, 8, "policy", timeShardingCfg(shardstreams.LimitsServiceStreamShardingModeLive))

		require.Empty(t, s.streams)
		require.Len(t, s.liveCandidates, 2)
		for _, c := range s.liveCandidates {
			require.True(t, c.labels.Has(constants.TimeShardLabel), c.stream.Labels)
		}
	})

	t.Run("backfill streams are not time-sharded", func(t *testing.T) {
		backfill := labels.FromStrings("app", "a", constants.BackfillLabel, "true")
		s := newTestStreamSharder(false, 0, now)
		s.shard(newTestShardedStream(backfill, old, old.Add(90*time.Minute)), backfill, 8, "policy", timeShardingCfg(""))

		require.Equal(t, []string{backfill.String()}, streamLabels(t, s.streams))
	})

	t.Run("recent entries are sorted in place but not time-sharded", func(t *testing.T) {
		s := newTestStreamSharder(false, 0, now)
		stream := newTestShardedStream(lbls, now, now.Add(-time.Second))
		s.shard(stream, lbls, 8, "policy", timeShardingCfg(""))

		require.Equal(t, []string{lbls.String()}, streamLabels(t, s.streams))
		var got []time.Time
		stream.EachGroup(func(_, _ []logproto.LabelAdapter, entries []logproto.Entry) {
			for _, e := range entries {
				got = append(got, e.Timestamp)
			}
		})
		require.Equal(t, []time.Time{now.Add(-time.Second), now}, got)
	})
}

func TestStreamSharder_MixedShardStreams(t *testing.T) {
	now := time.Now()
	lbls := labels.FromStrings("app", "a")
	enabled := shardstreams.Config{Enabled: true, DesiredRate: loki_flagext.ByteSize(100)}
	disabled := shardstreams.Config{DesiredRate: loki_flagext.ByteSize(100)}

	for _, ingestLimitsEnabled := range []bool{true, false} {
		s := newTestStreamSharder(ingestLimitsEnabled, 0, now)
		s.shard(newTestShardedStream(lbls, now), lbls, 4, "on", enabled)
		s.shard(newTestShardedStream(lbls, now), lbls, 4, "off", disabled)
		s.shard(newTestShardedStream(lbls, now), lbls, 4, "off-too", disabled)

		// Only tracked while the ingest limits are enabled, and only the first policy of each.
		require.Equal(t, ingestLimitsEnabled, s.shardStreamsEnabledSeen)
		require.Equal(t, ingestLimitsEnabled, s.shardStreamsDisabledSeen)
		if ingestLimitsEnabled {
			require.Equal(t, "on", s.shardStreamsEnabledPolicy)
			require.Equal(t, "off", s.shardStreamsDisabledPolicy)
		}
	}
}
