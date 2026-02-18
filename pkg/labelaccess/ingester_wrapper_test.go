package labelaccess

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester"
	ingester_client "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func CreateLabelMatchers(input *types.LabelPolicy) error {
	policyMatchers := make([]*labels.Matcher, len(input.Selector))
	for i, lm := range input.Selector {
		matcher, err := types.LabelMatcherToPromLabel(lm)
		policyMatchers[i] = matcher
		if err != nil {
			return err
		}
	}

	return nil
}

func BenchmarkTwoLabelMatchersNoRegex(b *testing.B) {
	b.ReportAllocs()

	m1, _ := labels.NewMatcher(labels.MatchNotEqual, "risk_profile", "CRITICAL")
	m2, _ := labels.NewMatcher(labels.MatchNotEqual, "risk_profile", "critical")
	policy := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, 2),
	}
	policy.Selector[0], _ = types.LabelMatcherFromPromLabel(m1)
	policy.Selector[1], _ = types.LabelMatcherFromPromLabel(m2)

	for i := 0; i < b.N; i++ {
		err := CreateLabelMatchers(policy)
		if err != nil {
			b.Errorf("Unexpected error in benchmark: %v", err)
		}
	}
}

func BenchmarkOneLabelMatcherWithRegex(b *testing.B) {
	b.ReportAllocs()

	m1, _ := labels.NewMatcher(labels.MatchRegexp, "risk_profile", "CRITICAL|critical")
	policy := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, 1),
	}
	policy.Selector[0], _ = types.LabelMatcherFromPromLabel(m1)

	for i := 0; i < b.N; i++ {
		err := CreateLabelMatchers(policy)
		if err != nil {
			b.Errorf("Unexpected error in benchmark: %v", err)
		}
	}
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}

// nolint
func defaultIngesterTestConfig(t testing.TB) ingester.Config {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, ring.GetCodec(), nil, log.NewNopLogger())
	require.NoError(t, err)

	cfg := ingester.Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushCheckPeriod = 99999 * time.Hour
	cfg.MaxChunkIdle = 99999 * time.Hour
	cfg.ConcurrentFlushes = 1
	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = kvClient
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = 0
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
	cfg.LifecyclerConfig.FinalSleep = 0
	cfg.LifecyclerConfig.MinReadyDuration = 0
	cfg.BlockSize = 256 * 1024
	cfg.TargetChunkSize = 1500 * 1024
	cfg.WAL.Enabled = false
	return cfg
}

func TestLabel(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	i, err := ingester.New(ingesterConfig, ingester_client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, nil, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	tw := IngesterWrapper{}
	wrapped := tw.Wrap(i)

	instancePolicyMap := LabelPolicySet{}
	// A policy with multiple label matchers: must match env="dev" or level="info"
	m1 := labels.MustNewMatcher(labels.MatchEqual, "env", "dev")
	m2 := labels.MustNewMatcher(labels.MatchNotEqual, "level", "info")
	policy := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, 2),
	}
	policy.Selector[0], err = types.LabelMatcherFromPromLabel(m1)
	require.Nil(t, err)
	policy.Selector[1], err = types.LabelMatcherFromPromLabel(m2)
	require.Nil(t, err)
	instancePolicyMap["tenant1"] = []*types.LabelPolicy{policy}

	ctx := context.Background()
	ctx = InjectLabelMatchersContext(ctx, instancePolicyMap)
	ctx = user.InjectOrgID(ctx, "tenant1")

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{env="dev",foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{env="dev",foo="bar",bar="baz2"}`,
			},
			{
				Labels: `{env="prod",foo="bar",bar="baz3"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[2].Entries = append(req.Streams[2].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	// Call Label() without a query : labels matching LBAC header are returned
	start := time.Unix(0, 0)
	res, err := wrapped.Label(ctx, &logproto.LabelRequest{
		Start:  &start,
		Name:   "bar",
		Values: true,
		Query:  ``,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"baz1", "baz2"}, res.Values)

	// Call Label() with a query : labels matching LBAC header and query are returned
	res, err = wrapped.Label(ctx, &logproto.LabelRequest{
		Start:  &start,
		Name:   "bar",
		Values: true,
		Query:  `{bar="baz2"}`,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"baz2"}, res.Values)

	// Call Label() without an LBAC header and a query: all label values are returned
	ctx = context.Background()
	ctx = user.InjectOrgID(ctx, "tenant1")
	res, err = wrapped.Label(ctx, &logproto.LabelRequest{
		Start:  &start,
		Name:   "bar",
		Values: true,
		Query:  ``,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"baz1", "baz2", "baz3"}, res.Values)
}
