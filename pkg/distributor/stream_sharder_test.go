package distributor

import (
	"bytes"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
	"github.com/grafana/loki/v3/pkg/validation"
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
			require.Equal(t, wantLastShard, shardCounter(s.d.shardTracker, streamSharderTestTenant, stream.Hash))
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

func TestShardStreams_WarnsOnMixedShardStreams(t *testing.T) {
	now := time.Now()
	lbls := labels.FromStrings("app", "a")
	shardingOff := false

	lim := &validation.Limits{}
	flagext.DefaultValues(lim)
	lim.ShardStreams.Enabled = true
	lim.PolicyOverrideLimits = map[string]validation.PolicyOverridableLimits{
		"off": {ShardStreams: &validation.PerPolicyConfigOverride{Enabled: &shardingOff}},
	}
	overrides, err := validation.NewOverrides(*lim, nil)
	require.NoError(t, err)
	validator, err := NewValidator(overrides, nil)
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		policies []string
		wantWarn bool
	}{
		{name: "policies that agree", policies: []string{"on", "on-too"}},
		{name: "policies that disagree", policies: []string{"on", "off"}, wantWarn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			d := &Distributor{
				logger:       log.NewLogfmtLogger(&logs),
				validator:    validator,
				rateStore:    &fakeRateStore{},
				m:            newMetrics(prometheus.NewPedanticRegistry()),
				shardTracker: NewShardTracker(),
			}
			d.cfg.IngestLimitsEnabled = true

			accepted := make([]acceptedStream, 0, len(tc.policies))
			for _, policy := range tc.policies {
				accepted = append(accepted, acceptedStream{stream: newTestShardedStream(lbls, now), lbs: lbls, policy: policy})
			}
			sharded := d.shardStreams(streamSharderTestTenant, now, accepted)

			require.Len(t, sharded.streams, len(tc.policies))
			if !tc.wantWarn {
				require.Empty(t, logs.String())
				return
			}
			require.Contains(t, logs.String(), "level=warn")
			require.Contains(t, logs.String(), "enabled_policy=on disabled_policy=off")
		})
	}
}

func TestStreamSharder_Packing(t *testing.T) {
	grouped := buildGroupedStream(2, 2, 5)
	require.Equal(t, 736, expandedShardSize(grouped))
	entries := make([]logproto.Entry, 200)
	for i := range entries {
		entries[i] = buildEntry(strings.Repeat("x", 100))
	}
	infrequent := *logproto.FromStream(logproto.Stream{Labels: shardLabels.String(), Entries: entries})

	cases := []struct {
		name                                          string
		enabled                                       bool
		input                                         logproto.InternalStreamAdapter
		target                                        int
		pushRate                                      float64
		streamRate                                    int64
		recommendedShards, wantShards, wantShardBytes int
	}{
		{
			name:  "packing disabled",
			input: grouped, target: 300, pushRate: 1, streamRate: 1200,
			recommendedShards: 4, wantShards: 4,
		},
		{
			name: "pack into fewer shards", enabled: true,
			input: grouped, target: 300, pushRate: 1, streamRate: 1200,
			recommendedShards: 4, wantShards: 3,
		},
		{
			name: "pack into one shard", enabled: true,
			input: grouped, target: 1000, pushRate: 1, streamRate: 4000,
			recommendedShards: 4, wantShards: 1,
		},
		{
			// The rate estimate permits two shards; each must take 10KB of the 20KB push.
			name: "infrequent push respects recommended shard count", enabled: true,
			input: infrequent, target: 1000, pushRate: 0.1,
			recommendedShards: 2, wantShards: 2, wantShardBytes: 10000,
		},
	}
	for _, mode := range []struct {
		name, value string
	}{
		{"rate store", ""},
		{"shadow", shardstreams.LimitsServiceStreamShardingModeShadow},
		{"live", shardstreams.LimitsServiceStreamShardingModeLive},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					s := newTestStreamSharder(true, tc.pushRate, time.Now())
					s.d.cfg.PackStreams = tc.enabled
					s.d.rateStore = &fakeRateStore{rate: tc.streamRate, pushRate: tc.pushRate}
					cfg := shardstreams.Config{Enabled: true, DesiredRate: loki_flagext.ByteSize(tc.target), MaxShardCount: 4, LimitsServiceStreamShardingMode: mode.value}
					s.shard(tc.input, shardLabels, expandedShardSize(tc.input), "policy", cfg)
					if mode.value == shardstreams.LimitsServiceStreamShardingModeLive {
						require.Empty(t, s.streams)
						require.Len(t, s.liveCandidates, 1)
						c := s.liveCandidates[0]
						require.Equal(t, tc.recommendedShards, c.rateStoreShards, "packing must not change the recommended count")
						s.streams = s.d.shardStreamToCount(c.stream, c.labels, streamSharderTestTenant, c.policy, c.shardStreamsCfg, tc.recommendedShards, int(c.totalSize))
					}
					if mode.value == shardstreams.LimitsServiceStreamShardingModeShadow {
						require.Len(t, s.shadowCandidates, 1)
						require.Equal(t, tc.recommendedShards, s.shadowCandidates[0].rateStoreShards)
					}
					require.Len(t, s.streams, tc.wantShards)
					require.Equal(t, tc.wantShards, shardCounter(s.d.shardTracker, streamSharderTestTenant, tc.input.Hash))
					var streams []logproto.InternalStreamAdapter
					for _, ks := range s.streams {
						require.Equal(t, "policy", ks.Policy)
						require.Equal(t, tc.input.Hash, ks.HashKeyNoShard)
						require.Equal(t, lokiring.TokenFor(streamSharderTestTenant, ks.Stream.Labels), ks.HashKey)
						lbls, err := syntax.ParseLabels(ks.Stream.Labels)
						require.NoError(t, err)
						require.Equal(t, labels.StableHash(lbls), ks.Stream.Hash)
						number, err := strconv.Atoi(lbls.Get(constants.StreamShardLabel))
						require.NoError(t, err)
						require.GreaterOrEqual(t, number, 0)
						require.Less(t, number, tc.recommendedShards)
						if tc.wantShardBytes > 0 {
							require.Equal(t, tc.wantShardBytes, expandedShardSize(ks.Stream))
						}
						streams = append(streams, ks.Stream)
					}
					requireShardsCarryTheStream(t, tc.input, streams)
				})
			}
		})
	}
}

func TestStreamSharder_PackingAfterTimeSharding(t *testing.T) {
	now := time.Now()
	s := newTestStreamSharder(false, 1, now)
	s.d.cfg.PackStreams = true
	input := newTestShardedStream(shardLabels)
	for _, ts := range []time.Time{now.Add(-3 * time.Hour), now} {
		for range 6 {
			input.ResourceLogs[0].ScopeLogs[0].Entries = append(input.ResourceLogs[0].ScopeLogs[0].Entries, logproto.Entry{Timestamp: ts, Line: strings.Repeat("x", 1000)})
		}
	}
	cfg := shardstreams.Config{Enabled: true, TimeShardingEnabled: true, TimeShardingIgnoreRecent: 40 * time.Minute, DesiredRate: 4000}
	s.shard(input, shardLabels, 12000, "policy", cfg)
	// Each time bucket has 6000 bytes and uses two rate shards, independent of the full push size.
	require.Len(t, s.streams, 4)
	byTime := map[string]int{}
	for _, ks := range s.streams {
		lbls, err := syntax.ParseLabels(ks.Stream.Labels)
		require.NoError(t, err)
		byTime[lbls.Get(constants.TimeShardLabel)] += ks.Stream.EntryCount()
		require.Contains(t, []int{2, 4}, ks.Stream.EntryCount())
		require.Equal(t, ks.Stream.EntryCount()*1000, expandedShardSize(ks.Stream))
	}
	require.Len(t, byTime, 2)
	for _, count := range byTime {
		require.Equal(t, 6, count)
	}
}

func TestConfig_PackStreams(t *testing.T) {
	var cfg Config
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(fs)
	require.False(t, cfg.PackStreams)
	require.NoError(t, fs.Parse([]string{"-distributor.pack-streams"}))
	require.True(t, cfg.PackStreams)
	require.NoError(t, cfg.Validate())

	cfg.PackStreams = false
	decoder := yaml.NewDecoder(strings.NewReader("pack_streams: true"))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&cfg))
	require.True(t, cfg.PackStreams)

	var shardCfg shardstreams.Config
	decoder = yaml.NewDecoder(strings.NewReader("pack_streams: true"))
	decoder.KnownFields(true)
	require.ErrorContains(t, decoder.Decode(&shardCfg), "field pack_streams not found")
}

func TestStreamSharder_PackingRotation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		entries     int
		target      loki_flagext.ByteSize
		wantEntries []int
	}{
		{name: "empty packed stream", target: 100},
		{name: "empty zero-target stream"},
		{name: "zero target with fewer entries than shards", entries: 2, wantEntries: []int{1, 1}},
		{name: "zero target uses all recommended shards", entries: 20, wantEntries: []int{5, 5, 5, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStreamSharder(false, 1, time.Now())
			s.d.cfg.PackStreams = true
			input := buildGroupedStream(1, 1, tc.entries)
			// Packed pushes sharing this stripe have already advanced the counter.
			s.d.shardTracker.Reserve(streamSharderTestTenant, input.Hash+defaultStripeSize, 10, 7, true)

			// The limits service can recommend multiple shards while the local target is zero.
			cfg := shardstreams.Config{DesiredRate: tc.target}
			streams := s.d.shardStreamToCount(input, shardLabels, streamSharderTestTenant, "policy", cfg, 4, expandedShardSize(input))
			require.Len(t, streams, len(tc.wantEntries))
			var outputs []logproto.InternalStreamAdapter
			for i, stream := range streams {
				require.Equal(t, tc.wantEntries[i], stream.Stream.EntryCount())
				lbls, err := syntax.ParseLabels(stream.Stream.Labels)
				require.NoError(t, err)
				require.Equal(t, strconv.Itoa((7+i)%4), lbls.Get(constants.StreamShardLabel))
				outputs = append(outputs, stream.Stream)
			}
			requireShardsCarryTheStream(t, input, outputs)
			require.Equal(t, 7+len(tc.wantEntries), shardCounter(s.d.shardTracker, streamSharderTestTenant, input.Hash))
		})
	}
}

func TestStreamSharder_ConcurrentReservations(t *testing.T) {
	const pushes, entries = 100, 3
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprintf("packed=%t", packed), func(t *testing.T) {
			s := newTestStreamSharder(false, 1, time.Now())
			s.d.cfg.PackStreams = packed
			s.d.shardTracker.seed = 1
			stream := *logproto.FromStream(logproto.Stream{
				Labels: shardLabels.String(), Hash: labels.StableHash(shardLabels),
				Entries: []logproto.Entry{buildEntry("a"), buildEntry("b"), buildEntry("c")},
			})
			cfg := shardstreams.Config{DesiredRate: 1}
			outputs := make(chan []KeyedStream, pushes)
			var wg sync.WaitGroup
			for range pushes {
				wg.Go(func() {
					outputs <- s.d.shardStreamToCount(stream, shardLabels, streamSharderTestTenant, "", cfg, pushes*entries, entries)
				})
			}
			wg.Wait()
			close(outputs)
			seen := make(map[string]bool)
			for out := range outputs {
				require.Len(t, out, entries)
				for _, shard := range out {
					require.Equal(t, 1, shard.Stream.EntryCount())
					require.False(t, seen[shard.Stream.Labels], "concurrent reservations reused %s", shard.Stream.Labels)
					seen[shard.Stream.Labels] = true
				}
			}
			require.Len(t, seen, pushes*entries)
			require.Equal(t, pushes*entries, shardCounter(s.d.shardTracker, streamSharderTestTenant, stream.Hash))
		})
	}
}
