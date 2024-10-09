package base

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/promql"
	promRules "github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v2"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/series"
	"github.com/grafana/loki/v3/pkg/ruler/config"
	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
	"github.com/grafana/loki/v3/pkg/ruler/rulestore"
	"github.com/grafana/loki/v3/pkg/ruler/rulestore/objectclient"
	loki_storage "github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func defaultRulerConfig(t testing.TB, store rulestore.RuleStore) Config {
	t.Helper()

	// Create a new temporary directory for the rules, so that
	// each test will run in isolation.
	rulesDir := t.TempDir()

	codec := ring.GetCodec()
	consul, closer := consul.NewInMemoryClient(codec, log.NewNopLogger(), nil)
	t.Cleanup(func() { assert.NoError(t, closer.Close()) })

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.RulePath = rulesDir
	cfg.StoreConfig.mock = store
	cfg.Ring.KVStore.Mock = consul
	cfg.Ring.NumTokens = 1
	cfg.Ring.ListenPort = 0
	cfg.Ring.InstanceAddr = "localhost"
	cfg.Ring.InstanceID = "localhost"
	cfg.EnableQueryStats = false

	return cfg
}

type ruleLimits struct {
	tenantShard          int
	maxRulesPerRuleGroup int
	maxRuleGroups        int
	alertManagerConfig   map[string]*config.AlertManagerConfig
}

func (r ruleLimits) RulerTenantShardSize(_ string) int {
	return r.tenantShard
}

func (r ruleLimits) RulerMaxRuleGroupsPerTenant(_ string) int {
	return r.maxRuleGroups
}

func (r ruleLimits) RulerMaxRulesPerRuleGroup(_ string) int {
	return r.maxRulesPerRuleGroup
}

func (r ruleLimits) RulerAlertManagerConfig(tenantID string) *config.AlertManagerConfig {
	return r.alertManagerConfig[tenantID]
}

func testQueryableFunc(q storage.Querier) storage.QueryableFunc {
	if q != nil {
		return func(_, _ int64) (storage.Querier, error) {
			return q, nil
		}
	}

	return func(_, _ int64) (storage.Querier, error) {
		return storage.NoopQuerier(), nil
	}
}

func testSetup(t *testing.T, q storage.Querier) (*promql.Engine, storage.QueryableFunc, Pusher, log.Logger, RulesLimits, prometheus.Registerer) {
	dir := t.TempDir()

	tracker := promql.NewActiveQueryTracker(dir, 20, log.NewNopLogger())

	engine := promql.NewEngine(promql.EngineOpts{
		MaxSamples:         1e6,
		ActiveQueryTracker: tracker,
		Timeout:            2 * time.Minute,
	})

	// Mock the pusher
	pusher := newPusherMock()
	pusher.MockPush(&logproto.WriteResponse{}, nil)

	l := log.NewLogfmtLogger(os.Stdout)
	l = level.NewFilter(l, level.AllowInfo())

	reg := prometheus.NewRegistry()
	queryable := testQueryableFunc(q)

	return engine, queryable, pusher, l, ruleLimits{maxRuleGroups: 20, maxRulesPerRuleGroup: 15}, reg
}

func newManager(t *testing.T, cfg Config, q storage.Querier) *DefaultMultiTenantManager {
	engine, queryable, pusher, logger, overrides, reg := testSetup(t, q)
	manager, err := NewDefaultMultiTenantManager(cfg, DefaultTenantManagerFactory(cfg, pusher, queryable, engine, nil, constants.Loki), reg, logger, overrides, constants.Loki)
	require.NoError(t, err)

	return manager
}

func newMultiTenantManager(t *testing.T, cfg Config, q storage.Querier, amConf map[string]*config.AlertManagerConfig) *DefaultMultiTenantManager {
	engine, queryable, pusher, logger, _, reg := testSetup(t, q)

	overrides := ruleLimits{maxRuleGroups: 20, maxRulesPerRuleGroup: 15}
	overrides.alertManagerConfig = amConf

	manager, err := NewDefaultMultiTenantManager(cfg, DefaultTenantManagerFactory(cfg, pusher, queryable, engine, nil, constants.Loki), reg, logger, overrides, constants.Loki)
	require.NoError(t, err)

	return manager
}

type mockRulerClientsPool struct {
	ClientsPool
	cfg           Config
	rulerAddrMap  map[string]*Ruler
	numberOfCalls atomic.Int32
}

type mockRulerClient struct {
	ruler         *Ruler
	numberOfCalls *atomic.Int32
}

func (c *mockRulerClient) Rules(ctx context.Context, in *RulesRequest, _ ...grpc.CallOption) (*RulesResponse, error) {
	c.numberOfCalls.Inc()
	return c.ruler.Rules(ctx, in)
}

func (p *mockRulerClientsPool) GetClientFor(addr string) (RulerClient, error) {
	for _, r := range p.rulerAddrMap {
		if r.lifecycler.GetInstanceAddr() == addr {
			return &mockRulerClient{
				ruler:         r,
				numberOfCalls: &p.numberOfCalls,
			}, nil
		}
	}

	return nil, fmt.Errorf("unable to find ruler for add %s", addr)
}

func newMockClientsPool(cfg Config, logger log.Logger, reg prometheus.Registerer, metricsNamespace string, rulerAddrMap map[string]*Ruler) *mockRulerClientsPool {
	return &mockRulerClientsPool{
		ClientsPool:  newRulerClientPool(cfg.ClientTLSConfig, logger, reg, metricsNamespace),
		cfg:          cfg,
		rulerAddrMap: rulerAddrMap,
	}
}

func buildRuler(t *testing.T, rulerConfig Config, q storage.Querier, clientMetrics loki_storage.ClientMetrics, rulerAddrMap map[string]*Ruler) *Ruler {
	// validate here instead of newRuler because it is used elsewhere
	require.NoError(t, rulerConfig.Validate(log.NewNopLogger()))

	engine, queryable, pusher, logger, overrides, reg := testSetup(t, q)
	storage, err := NewLegacyRuleStore(rulerConfig.StoreConfig, hedging.Config{}, clientMetrics, promRules.FileLoader{}, log.NewNopLogger())
	require.NoError(t, err)

	managerFactory := DefaultTenantManagerFactory(rulerConfig, pusher, queryable, engine, reg, constants.Loki)
	manager, err := NewDefaultMultiTenantManager(rulerConfig, managerFactory, reg, log.NewNopLogger(), overrides, constants.Loki)
	require.NoError(t, err)

	ruler, err := newRuler(
		rulerConfig,
		manager,
		reg,
		logger,
		storage,
		overrides,
		newMockClientsPool(rulerConfig, logger, reg, constants.Loki, rulerAddrMap),
		constants.Loki,
	)
	require.NoError(t, err)
	return ruler
}

func newTestRuler(t *testing.T, rulerConfig Config) *Ruler {
	m := loki_storage.NewClientMetrics()
	defer m.Unregister()
	ruler := buildRuler(t, rulerConfig, nil, m, nil)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), ruler))

	// Ensure all rules are loaded before usage
	ruler.syncRules(context.Background(), rulerSyncReasonInitial)

	return ruler
}

var _ MultiTenantManager = &DefaultMultiTenantManager{}

func TestNotifierSendsUserIDHeader(t *testing.T) {
	var wg sync.WaitGroup

	// We do expect 1 API call for the user create with the getOrCreateNotifier()
	wg.Add(1)
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		userID, _, err := tenant.ExtractTenantIDFromHTTPRequest(r)
		assert.NoError(t, err)
		assert.Equal(t, userID, "1")
		wg.Done()
	}))
	defer ts.Close()

	// We create an empty rule store so that the ruler will not load any rule from it.
	cfg := defaultRulerConfig(t, newMockRuleStore(nil))
	cfg.AlertmanagerURL = ts.URL
	cfg.AlertmanagerDiscovery = false

	manager := newManager(t, cfg, nil)
	defer manager.Stop()

	n, err := manager.getOrCreateNotifier("1")
	require.NoError(t, err)

	// Loop until notifier discovery syncs up
	for len(n.Alertmanagers()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	n.Send(&notifier.Alert{
		Labels: labels.Labels{labels.Label{Name: "alertname", Value: "testalert"}},
	})

	wg.Wait()

	// Ensure we have metrics in the notifier.
	assert.NoError(t, prom_testutil.GatherAndCompare(manager.registry.(*prometheus.Registry), strings.NewReader(`
		# HELP loki_prometheus_notifications_dropped_total Total number of alerts dropped due to errors when sending to Alertmanager.
		# TYPE loki_prometheus_notifications_dropped_total counter
		loki_prometheus_notifications_dropped_total{user="1"} 0
	`), "loki_prometheus_notifications_dropped_total"))
}

func TestMultiTenantsNotifierSendsUserIDHeader(t *testing.T) {
	var wg sync.WaitGroup

	const tenant1 = "tenant1"
	const tenant2 = "tenant2"

	// We do expect 2 API calls for the users create with the getOrCreateNotifier()
	wg.Add(2)
	ts1 := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		userID, _, err := tenant.ExtractTenantIDFromHTTPRequest(r)
		assert.NoError(t, err)
		assert.Equal(t, userID, tenant1)
		wg.Done()
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		userID, _, err := tenant.ExtractTenantIDFromHTTPRequest(r)
		assert.NoError(t, err)
		assert.Equal(t, userID, tenant2)
		wg.Done()
	}))
	defer ts2.Close()

	// We create an empty rule store so that the ruler will not load any rule from it.
	cfg := defaultRulerConfig(t, newMockRuleStore(nil))

	amCfg := map[string]*config.AlertManagerConfig{
		tenant1: {
			AlertmanagerURL:       ts1.URL,
			AlertmanagerDiscovery: false,
		},
		tenant2: {
			AlertmanagerURL:       ts2.URL,
			AlertmanagerDiscovery: false,
		},
	}

	manager := newMultiTenantManager(t, cfg, nil, amCfg)
	defer manager.Stop()

	n1, err := manager.getOrCreateNotifier(tenant1)
	require.NoError(t, err)

	n2, err := manager.getOrCreateNotifier(tenant2)
	require.NoError(t, err)

	// Loop until notifier discovery syncs up
	for len(n1.Alertmanagers()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	n1.Send(&notifier.Alert{
		Labels: labels.Labels{labels.Label{Name: "alertname1", Value: "testalert1"}},
	})

	for len(n2.Alertmanagers()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	n2.Send(&notifier.Alert{
		Labels: labels.Labels{labels.Label{Name: "alertname2", Value: "testalert2"}},
	})

	wg.Wait()

	// Ensure we have metrics in the notifier.
	assert.NoError(t, prom_testutil.GatherAndCompare(manager.registry.(*prometheus.Registry), strings.NewReader(`
		# HELP loki_prometheus_notifications_dropped_total Total number of alerts dropped due to errors when sending to Alertmanager.
		# TYPE loki_prometheus_notifications_dropped_total counter
		loki_prometheus_notifications_dropped_total{user="tenant1"} 0
		loki_prometheus_notifications_dropped_total{user="tenant2"} 0
	`), "loki_prometheus_notifications_dropped_total"))
}

func TestRuler_Rules(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRules))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	// test user1
	ctx := user.InjectOrgID(context.Background(), "user1")
	rls, err := r.Rules(ctx, &RulesRequest{})
	require.NoError(t, err)
	require.Len(t, rls.Groups, 1)
	rg := rls.Groups[0]
	expectedRg := mockRules["user1"][0]
	compareRuleGroupDescToStateDesc(t, expectedRg, rg)

	// test user2
	ctx = user.InjectOrgID(context.Background(), "user2")
	rls, err = r.Rules(ctx, &RulesRequest{})
	require.NoError(t, err)
	require.Len(t, rls.Groups, 1)
	rg = rls.Groups[0]
	expectedRg = mockRules["user2"][0]
	compareRuleGroupDescToStateDesc(t, expectedRg, rg)
}

func compareRuleGroupDescToStateDesc(t *testing.T, expected *rulespb.RuleGroupDesc, got *GroupStateDesc) {
	require.Equal(t, got.Group.Name, expected.Name)
	require.Equal(t, got.Group.Namespace, expected.Namespace)
	require.Len(t, expected.Rules, len(got.ActiveRules))
	for i := range got.ActiveRules {
		require.Equal(t, expected.Rules[i].Record, got.ActiveRules[i].Rule.Record)
		require.Equal(t, expected.Rules[i].Alert, got.ActiveRules[i].Rule.Alert)
	}
}

func TestGetRules(t *testing.T) {
	// ruler ID -> (user ID -> list of groups).
	type expectedRulesMap map[string]map[string]rulespb.RuleGroupList

	type testCase struct {
		sharding         bool
		shardingStrategy string
		shardingAlgo     string
		shuffleShardSize int
	}

	allRules := expectedRulesMap{
		"ruler1": map[string]rulespb.RuleGroupList{
			"user1": {
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "third", Rules: []*rulespb.RuleDesc{createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) > 0`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user2": {
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
		"ruler2": map[string]rulespb.RuleGroupList{
			"user1": {
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "fourth", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user2": {
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "third", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user3": {
				&rulespb.RuleGroupDesc{User: "user3", Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) > 0`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
		"ruler3": map[string]rulespb.RuleGroupList{
			"user3": {
				&rulespb.RuleGroupDesc{User: "user3", Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user2": {
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "forth", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "fifth", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
	}

	expectedAlertRules := expectedRulesMap{
		"ruler1": map[string]rulespb.RuleGroupList{
			"user1": {
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "third", Rules: []*rulespb.RuleDesc{createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) > 0`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
		"ruler2": map[string]rulespb.RuleGroupList{
			"user3": {
				&rulespb.RuleGroupDesc{User: "user3", Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) > 0`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
		"ruler3": map[string]rulespb.RuleGroupList{},
	}

	expectedRecordingRules := expectedRulesMap{
		"ruler1": map[string]rulespb.RuleGroupList{
			"user1": {
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user2": {
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
		"ruler2": map[string]rulespb.RuleGroupList{
			"user1": {
				&rulespb.RuleGroupDesc{User: "user1", Namespace: "namespace", Name: "fourth", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user2": {
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "third", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
		"ruler3": map[string]rulespb.RuleGroupList{
			"user3": {
				&rulespb.RuleGroupDesc{User: "user3", Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
			"user2": {
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "forth", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
				&rulespb.RuleGroupDesc{User: "user2", Namespace: "namespace", Name: "fifth", Rules: []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`)}, Interval: 10 * time.Second, Limit: 10},
			},
		},
	}

	testCases := map[string]testCase{
		"No Sharding": {
			sharding: false,
		},
		"Default Sharding": {
			sharding: true,
		},
		"Shuffle Sharding and ShardSize = 2": {
			sharding:         true,
			shuffleShardSize: 2,
			shardingStrategy: util.ShardingStrategyShuffle,
		},
	}

	for name, tc := range testCases {
		for _, ruleType := range []RulesRequest_RuleType{AnyRule, AlertingRule, RecordingRule} {
			t.Run(name+" "+ruleType.String(), func(t *testing.T) {
				kvStore, cleanUp := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
				t.Cleanup(func() { assert.NoError(t, cleanUp.Close()) })
				allRulesByUser := map[string]rulespb.RuleGroupList{}
				filteredRulesByUser := map[string]rulespb.RuleGroupList{}
				allRulesByRuler := map[string]rulespb.RuleGroupList{}
				allTokensByRuler := map[string][]uint32{}
				rulerAddrMap := map[string]*Ruler{}

				createRuler := func(id string) *Ruler {
					cfg := defaultRulerConfig(t, newMockRuleStore(allRulesByUser))

					cfg.ShardingStrategy = tc.shardingStrategy
					cfg.EnableSharding = tc.sharding
					cfg.ShardingAlgo = tc.shardingAlgo

					cfg.Ring = RingConfig{
						InstanceID:   id,
						InstanceAddr: id,
						KVStore: kv.Config{
							Mock: kvStore,
						},
					}
					m := loki_storage.NewClientMetrics()
					defer m.Unregister()
					r := buildRuler(t, cfg, nil, m, rulerAddrMap)
					r.limits = ruleLimits{tenantShard: tc.shuffleShardSize}
					rulerAddrMap[id] = r
					if r.ring != nil {
						require.NoError(t, services.StartAndAwaitRunning(context.Background(), r.ring))
						t.Cleanup(r.ring.StopAsync)
					}
					return r
				}

				for rID, r := range allRules {
					createRuler(rID)
					for user, rules := range r {
						allRulesByUser[user] = append(allRulesByUser[user], rules...)
						allRulesByRuler[rID] = append(allRulesByRuler[rID], rules...)
						allTokensByRuler[rID] = generateTokenForGroups(rules, 1)
					}
				}

				var filteredRules expectedRulesMap
				switch ruleType {
				case AlertingRule:
					filteredRules = expectedAlertRules
				case RecordingRule:
					filteredRules = expectedRecordingRules
				default:
					filteredRules = allRules
				}

				for _, r := range filteredRules {
					for user, rules := range r {
						filteredRulesByUser[user] = append(filteredRulesByUser[user], rules...)
					}
				}

				if tc.sharding {
					err := kvStore.CAS(context.Background(), ringKey, func(in interface{}) (out interface{}, retry bool, err error) {
						d, _ := in.(*ring.Desc)
						if d == nil {
							d = ring.NewDesc()
						}
						for rID, tokens := range allTokensByRuler {
							d.AddIngester(rID, rulerAddrMap[rID].lifecycler.GetInstanceAddr(), "", tokens, ring.ACTIVE, time.Now(), false, time.Now())
						}
						return d, true, nil
					})
					require.NoError(t, err)
					// Wait a bit to make sure ruler's ring is updated.
					time.Sleep(100 * time.Millisecond)
				}

				forEachRuler := func(f func(rID string, r *Ruler)) {
					for rID, r := range rulerAddrMap {
						f(rID, r)
					}
				}

				// Sync Rules
				forEachRuler(func(_ string, r *Ruler) {
					r.syncRules(context.Background(), rulerSyncReasonInitial)
				})

				for u := range filteredRulesByUser {
					ctx := user.InjectOrgID(context.Background(), u)
					forEachRuler(func(_ string, r *Ruler) {
						rules, err := r.GetRules(ctx, &RulesRequest{Filter: ruleType})
						require.NoError(t, err)
						require.Equal(t, len(filteredRulesByUser[u]), len(rules))
						if tc.sharding {
							mockPoolClient := r.clientsPool.(*mockRulerClientsPool)

							if tc.shardingStrategy == util.ShardingStrategyShuffle {
								require.Equal(t, int32(tc.shuffleShardSize), mockPoolClient.numberOfCalls.Load())
							} else {
								require.Equal(t, int32(len(rulerAddrMap)), mockPoolClient.numberOfCalls.Load())
							}
							mockPoolClient.numberOfCalls.Store(0)
						}
					})
				}

				totalLoadedRules := 0
				totalConfiguredRules := 0

				forEachRuler(func(rID string, r *Ruler) {
					localRules, err := r.listRules(context.Background())
					require.NoError(t, err)
					for _, rules := range localRules {
						totalLoadedRules += len(rules)
					}
					totalConfiguredRules += len(allRulesByRuler[rID])
				})

				if tc.sharding {
					require.Equal(t, totalConfiguredRules, totalLoadedRules)
				} else {
					// Not sharding means that all rules will be loaded on all rulers
					numberOfRulers := len(rulerAddrMap)
					require.Equal(t, totalConfiguredRules*numberOfRulers, totalLoadedRules)
				}
			})
		}
	}
}

func TestSharding(t *testing.T) {
	const (
		user1 = "user1"
		user2 = "user2"
		user3 = "user3"
	)

	user1Group1Rule1 := &rulespb.RuleDesc{Record: "user1_group1_rule1", Expr: "1", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("rule", "1"))}
	user1Group1Rule2 := &rulespb.RuleDesc{Record: "user1_group1_rule2", Expr: "2", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("rule", "2"))}
	user1Group2Rule1 := &rulespb.RuleDesc{Record: "user1_group2_rule1", Expr: "1", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("rule", "1"))}

	user1Group1 := &rulespb.RuleGroupDesc{User: user1, Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{user1Group1Rule1, user1Group1Rule2}}
	user1Group2 := &rulespb.RuleGroupDesc{User: user1, Namespace: "namespace", Name: "second", Rules: []*rulespb.RuleDesc{user1Group2Rule1}}

	user2Group1Rule1 := &rulespb.RuleDesc{Alert: "User2Group1Alert1", Expr: "1", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("alert", "1"))}
	user2Group1Rule2 := &rulespb.RuleDesc{Record: "user2_group1_rule2", Expr: "2", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("rule", "2"))}

	user2Group1 := &rulespb.RuleGroupDesc{User: user2, Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{user2Group1Rule1, user2Group1Rule2}}

	user3Group1Rule1 := &rulespb.RuleDesc{Alert: "User3Group1Alert1", Expr: "1", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("alert", "1"))}
	user3Group1Rule2 := &rulespb.RuleDesc{Alert: "User3Group1Alert2", Expr: "2", Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("alert", "2"))}

	user3Group1 := &rulespb.RuleGroupDesc{User: user3, Namespace: "namespace", Name: "first", Rules: []*rulespb.RuleDesc{user3Group1Rule1, user3Group1Rule2}}

	// Must be distinct for test to work.
	user1Group1Token := tokenForGroup(user1Group1)
	user1Group2Token := tokenForGroup(user1Group2)
	user2Group1Token := tokenForGroup(user2Group1)
	user3Group1Token := tokenForGroup(user3Group1)

	user1Group1Rule1Token := tokenForRule(user1Group1, user1Group1Rule1)
	user1Group1Rule2Token := tokenForRule(user1Group1, user1Group1Rule2)
	user1Group2Rule1Token := tokenForRule(user1Group2, user1Group2Rule1)
	user2Group1Rule1Token := tokenForRule(user2Group1, user2Group1Rule1)
	user2Group1Rule2Token := tokenForRule(user2Group1, user2Group1Rule2)
	user3Group1Rule1Token := tokenForRule(user3Group1, user3Group1Rule1)
	user3Group1Rule2Token := tokenForRule(user3Group1, user3Group1Rule2)

	noRules := map[string]rulespb.RuleGroupList{}
	allRules := map[string]rulespb.RuleGroupList{
		user1: {user1Group1, user1Group2},
		user2: {user2Group1},
		user3: {user3Group1},
	}
	allRulesSharded := map[string]rulespb.RuleGroupList{
		user1: {
			cloneGroupWithRule(user1Group1, user1Group1Rule1),
			cloneGroupWithRule(user1Group1, user1Group1Rule2),
			cloneGroupWithRule(user1Group2, user1Group2Rule1),
		},
		user2: {
			cloneGroupWithRule(user2Group1, user2Group1Rule1),
			cloneGroupWithRule(user2Group1, user2Group1Rule2),
		},
		user3: {
			cloneGroupWithRule(user3Group1, user3Group1Rule1),
			cloneGroupWithRule(user3Group1, user3Group1Rule2),
		},
	}

	// ruler ID -> (user ID -> list of groups).
	type expectedRulesMap map[string]map[string]rulespb.RuleGroupList

	type testCase struct {
		sharding         bool
		shardingAlgo     string
		shardingStrategy string
		shuffleShardSize int
		setupRing        func(*ring.Desc)
		enabledUsers     []string
		disabledUsers    []string

		expectedRules expectedRulesMap
	}

	const (
		ruler1     = "ruler-1"
		ruler1Host = "1.1.1.1"
		ruler1Port = 9999
		ruler1Addr = "1.1.1.1:9999"

		ruler2     = "ruler-2"
		ruler2Host = "2.2.2.2"
		ruler2Port = 9999
		ruler2Addr = "2.2.2.2:9999"

		ruler3     = "ruler-3"
		ruler3Host = "3.3.3.3"
		ruler3Port = 9999
		ruler3Addr = "3.3.3.3:9999"
	)

	testCases := map[string]testCase{
		"no sharding": {
			sharding:      false,
			expectedRules: expectedRulesMap{ruler1: allRules},
		},

		"no sharding, single user allowed": {
			sharding:     false,
			enabledUsers: []string{user1},
			expectedRules: expectedRulesMap{ruler1: map[string]rulespb.RuleGroupList{
				user1: {user1Group1, user1Group2},
			}},
		},

		"no sharding, single user disabled": {
			sharding:      false,
			disabledUsers: []string{user1},
			expectedRules: expectedRulesMap{ruler1: map[string]rulespb.RuleGroupList{
				user2: {user2Group1},
				user3: {user3Group1},
			}},
		},

		"default sharding, single ruler": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", []uint32{0}, ring.ACTIVE, time.Now(), false, time.Now())
			},
			expectedRules: expectedRulesMap{ruler1: allRules},
		},

		"default sharding, single ruler, single enabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,
			enabledUsers:     []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", []uint32{0}, ring.ACTIVE, time.Now(), false, time.Now())
			},
			expectedRules: expectedRulesMap{ruler1: map[string]rulespb.RuleGroupList{
				user1: {user1Group1, user1Group2},
			}},
		},

		"default sharding, single ruler, single disabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,
			disabledUsers:    []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", []uint32{0}, ring.ACTIVE, time.Now(), false, time.Now())
			},
			expectedRules: expectedRulesMap{ruler1: map[string]rulespb.RuleGroupList{
				user2: {user2Group1},
				user3: {user3Group1},
			}},
		},

		"default sharding, multiple ACTIVE rulers": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Token + 1, user2Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1},
					user2: {user2Group1},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user1: {user1Group2},
					user3: {user3Group1},
				},
			},
		},

		"default sharding, multiple ACTIVE rulers, single enabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,
			enabledUsers:     []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Token + 1, user2Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user1: {user1Group2},
				},
			},
		},

		"default sharding, multiple ACTIVE rulers, single disabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,
			disabledUsers:    []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Token + 1, user2Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user2: {user2Group1},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user3: {user3Group1},
				},
			},
		},

		"default sharding, unhealthy ACTIVE ruler": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Token + 1, user2Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.Ingesters[ruler2] = ring.InstanceDesc{
					Addr:      ruler2Addr,
					Timestamp: time.Now().Add(-time.Hour).Unix(),
					State:     ring.ACTIVE,
					Tokens:    sortTokens([]uint32{user1Group2Token + 1, user3Group1Token + 1}),
				}
			},

			expectedRules: expectedRulesMap{
				// This ruler doesn't get rules from unhealthy ruler (RF=1).
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1},
					user2: {user2Group1},
				},
				ruler2: noRules,
			},
		},

		"default sharding, LEAVING ruler": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Token + 1, user2Group1Token + 1}), ring.LEAVING, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				// LEAVING ruler doesn't get any rules.
				ruler1: noRules,
				ruler2: allRules,
			},
		},

		"default sharding, JOINING ruler": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyDefault,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Token + 1, user2Group1Token + 1}), ring.JOINING, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				// JOINING ruler has no rules yet.
				ruler1: noRules,
				ruler2: allRules,
			},
		},

		"shuffle sharding, single ruler": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{0}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: allRules,
			},
		},

		"shuffle sharding, multiple rulers, shard size 1": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 1,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 0) + 1, userToken(user3, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group1Token + 1, user1Group2Token + 1, user2Group1Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: allRules,
				ruler2: noRules,
			},
		},

		// Same test as previous one, but with shard size=2. Second ruler gets all the rules.
		"shuffle sharding, two rulers, shard size 2": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 2,

			setupRing: func(desc *ring.Desc) {
				// Exact same tokens setup as previous test.
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 0) + 1, userToken(user3, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group1Token + 1, user1Group2Token + 1, user2Group1Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: noRules,
				ruler2: allRules,
			},
		},

		"shuffle sharding, two rulers, shard size 1, distributed users": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 1,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1, user1Group2},
				},
				ruler2: map[string]rulespb.RuleGroupList{
					user2: {user2Group1},
					user3: {user3Group1},
				},
			},
		},
		"shuffle sharding, three rulers, shard size 2": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 2,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, user1Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, user1Group2Token + 1, userToken(user2, 1) + 1, userToken(user3, 1) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user2Group1Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1},
				},
				ruler2: map[string]rulespb.RuleGroupList{
					user1: {user1Group2},
				},
				ruler3: map[string]rulespb.RuleGroupList{
					user2: {user2Group1},
					user3: {user3Group1},
				},
			},
		},
		"shuffle sharding, three rulers, shard size 2, ruler2 has no users": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 2,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 1) + 1, user1Group1Token + 1, user1Group2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, userToken(user3, 1) + 1, user2Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1, user1Group2},
				},
				ruler2: noRules, // Ruler2 owns token for user2group1, but user-2 will only be handled by ruler-1 and 3.
				ruler3: map[string]rulespb.RuleGroupList{
					user2: {user2Group1},
					user3: {user3Group1},
				},
			},
		},

		"shuffle sharding, three rulers, shard size 2, single enabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 2,
			enabledUsers:     []string{user1},

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, user1Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, user1Group2Token + 1, userToken(user2, 1) + 1, userToken(user3, 1) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user2Group1Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {user1Group1},
				},
				ruler2: map[string]rulespb.RuleGroupList{
					user1: {user1Group2},
				},
				ruler3: map[string]rulespb.RuleGroupList{},
			},
		},

		"shuffle sharding, three rulers, shard size 2, single disabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shuffleShardSize: 2,
			disabledUsers:    []string{user1},

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, user1Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, user1Group2Token + 1, userToken(user2, 1) + 1, userToken(user3, 1) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user2Group1Token + 1, user3Group1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{},
				ruler2: map[string]rulespb.RuleGroupList{},
				ruler3: map[string]rulespb.RuleGroupList{
					user2: {user2Group1},
					user3: {user3Group1},
				},
			},
		},

		"sharding by rule, single ruler": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", []uint32{0}, ring.ACTIVE, time.Now(), false, time.Now())
			},
			expectedRules: expectedRulesMap{ruler1: allRulesSharded},
		},

		"sharding by rule, single ruler, single enabled user": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,
			enabledUsers: []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", []uint32{0}, ring.ACTIVE, time.Now(), false, time.Now())
			},
			expectedRules: expectedRulesMap{ruler1: map[string]rulespb.RuleGroupList{
				user1: {
					cloneGroupWithRule(user1Group1, user1Group1Rule1),
					cloneGroupWithRule(user1Group1, user1Group1Rule2),
					cloneGroupWithRule(user1Group2, user1Group2Rule1),
				},
			}},
		},

		"sharding by rule, single ruler, single disabled user": {
			sharding:      true,
			shardingAlgo:  util.ShardingAlgoByRule,
			disabledUsers: []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", []uint32{0}, ring.ACTIVE, time.Now(), false, time.Now())
			},
			expectedRules: expectedRulesMap{ruler1: map[string]rulespb.RuleGroupList{
				user2: {
					cloneGroupWithRule(user2Group1, user2Group1Rule1),
					cloneGroupWithRule(user2Group1, user2Group1Rule2),
				},
				user3: {
					cloneGroupWithRule(user3Group1, user3Group1Rule1),
					cloneGroupWithRule(user3Group1, user3Group1Rule2),
				},
			}},
		},

		"sharding by rule, multiple ACTIVE rulers": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user2Group1Rule2Token + 1, user1Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
					},
					user2: {cloneGroupWithRule(user2Group1, user2Group1Rule2)},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user1: {cloneGroupWithRule(user1Group2, user1Group2Rule1)},
					user2: {cloneGroupWithRule(user2Group1, user2Group1Rule1)},
					user3: {
						cloneGroupWithRule(user3Group1, user3Group1Rule1),
						cloneGroupWithRule(user3Group1, user3Group1Rule2),
					},
				},
			},
		},

		"sharding by rule, multiple ACTIVE rulers, single enabled user": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,
			enabledUsers: []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user2Group1Rule2Token + 1, user1Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
					},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group2, user1Group2Rule1),
					},
				},
			},
		},

		"sharding by rule, multiple ACTIVE rulers, single disabled user": {
			sharding:      true,
			shardingAlgo:  util.ShardingAlgoByRule,
			disabledUsers: []string{user1},
			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user2Group1Rule2Token + 1, user1Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule2),
					},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule1),
					},
					user3: {
						cloneGroupWithRule(user3Group1, user3Group1Rule1),
						cloneGroupWithRule(user3Group1, user3Group1Rule2),
					},
				},
			},
		},

		"sharding by rule, unhealthy ACTIVE ruler": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user2Group1Rule2Token + 1, user1Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.Ingesters[ruler2] = ring.InstanceDesc{
					Addr:      ruler2Addr,
					Timestamp: time.Now().Add(-time.Hour).Unix(),
					State:     ring.ACTIVE,
					Tokens:    sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}),
				}
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
					},
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule2),
					},
				},
				// This ruler doesn't get rules from unhealthy ruler (RF=1).
				ruler2: noRules,
			},
		},

		"sharding by rule, LEAVING ruler": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user2Group1Rule2Token + 1, user1Group1Rule2Token + 1}), ring.LEAVING, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				// LEAVING ruler doesn't get any rules.
				ruler1: noRules,
				ruler2: allRulesSharded,
			},
		},

		"sharding by rule, JOINING ruler": {
			sharding:     true,
			shardingAlgo: util.ShardingAlgoByRule,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user2Group1Rule2Token + 1, user1Group1Rule2Token + 1}), ring.JOINING, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				// JOINING ruler has no rules yet.
				ruler1: noRules,
				ruler2: allRulesSharded,
			},
		},

		"shuffle sharding with 'by-rule' strategy, single ruler": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{0}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: allRulesSharded,
			},
		},

		"shuffle sharding with 'by-rule' strategy, multiple rulers, shard size 1": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 1,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 0) + 1, userToken(user3, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				// immaterial what tokens this ruler has, it won't be assigned any rules
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group2Rule1Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: allRulesSharded,
				ruler2: noRules,
			},
		},

		// Same test as previous one, but with shard size=2. Second ruler gets all the rules.
		"shuffle sharding with 'by-rule' strategy, two rulers, shard size 2": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 2,

			setupRing: func(desc *ring.Desc) {
				// Exact same tokens setup as previous test.
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 0) + 1, userToken(user3, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				// this ruler has all the rule tokens, so it gets all the rules
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{user1Group1Rule1Token + 1, user1Group1Rule2Token + 1, user1Group2Rule1Token + 1, user2Group1Rule1Token + 1, user2Group1Rule2Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: noRules,
				ruler2: allRulesSharded,
			},
		},

		"shuffle sharding with 'by-rule' strategy, two rulers, shard size 1, distributed users": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 1,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
						cloneGroupWithRule(user1Group2, user1Group2Rule1),
					},
				},

				ruler2: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule1),
						cloneGroupWithRule(user2Group1, user2Group1Rule2),
					},
					user3: {
						cloneGroupWithRule(user3Group1, user3Group1Rule1),
						cloneGroupWithRule(user3Group1, user3Group1Rule2),
					},
				},
			},
		},
		"shuffle sharding with 'by-rule' strategy, three rulers, shard size 2": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 2,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, user1Group1Rule1Token + 1, user1Group2Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, user1Group1Rule2Token + 1, userToken(user2, 1) + 1, userToken(user3, 1) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user2Group1Rule1Token + 1, user2Group1Rule2Token + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group2, user1Group2Rule1),
					},
				},
				ruler2: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
					},
				},
				ruler3: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule1),
						cloneGroupWithRule(user2Group1, user2Group1Rule2),
					},
					user3: {
						cloneGroupWithRule(user3Group1, user3Group1Rule1),
						cloneGroupWithRule(user3Group1, user3Group1Rule2),
					},
				},
			},
		},
		"shuffle sharding with 'by-rule' strategy, three rulers, shard size 2, ruler2 has no users": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 2,

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 1) + 1, user1Group1Rule1Token + 1, user1Group1Rule2Token + 1, user1Group2Rule1Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, userToken(user3, 1) + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
						cloneGroupWithRule(user1Group2, user1Group2Rule1),
					},
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule1),
					},
				},
				ruler2: noRules, // Ruler2 owns token for user2group1, but user-2 will only be handled by ruler-1 and 3.
				ruler3: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule2),
					},
					user3: {
						cloneGroupWithRule(user3Group1, user3Group1Rule1),
						cloneGroupWithRule(user3Group1, user3Group1Rule2),
					},
				},
			},
		},

		"shuffle sharding with 'by-rule' strategy, three rulers, shard size 2, single enabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 2,
			enabledUsers:     []string{user1},

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 1) + 1, user1Group1Rule1Token + 1, user1Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, userToken(user3, 1) + 1, user1Group2Rule1Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group1, user1Group1Rule1),
						cloneGroupWithRule(user1Group1, user1Group1Rule2),
					},
				},
				ruler2: map[string]rulespb.RuleGroupList{
					user1: {
						cloneGroupWithRule(user1Group2, user1Group2Rule1),
					},
				},
				ruler3: map[string]rulespb.RuleGroupList{},
			},
		},

		"shuffle sharding with 'by-rule' strategy, three rulers, shard size 2, single disabled user": {
			sharding:         true,
			shardingStrategy: util.ShardingStrategyShuffle,
			shardingAlgo:     util.ShardingAlgoByRule,
			shuffleShardSize: 2,
			disabledUsers:    []string{user1},

			setupRing: func(desc *ring.Desc) {
				desc.AddIngester(ruler1, ruler1Addr, "", sortTokens([]uint32{userToken(user1, 0) + 1, userToken(user2, 1) + 1, user1Group1Rule1Token + 1, user1Group1Rule2Token + 1, user2Group1Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler2, ruler2Addr, "", sortTokens([]uint32{userToken(user1, 1) + 1, userToken(user3, 1) + 1, user1Group2Rule1Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
				desc.AddIngester(ruler3, ruler3Addr, "", sortTokens([]uint32{userToken(user2, 0) + 1, userToken(user3, 0) + 1, user3Group1Rule1Token + 1, user3Group1Rule2Token + 1, user2Group1Rule2Token + 1}), ring.ACTIVE, time.Now(), false, time.Now())
			},

			expectedRules: expectedRulesMap{
				ruler1: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule1),
					},
				},
				ruler2: map[string]rulespb.RuleGroupList{},
				ruler3: map[string]rulespb.RuleGroupList{
					user2: {
						cloneGroupWithRule(user2Group1, user2Group1Rule2),
					},
					user3: {
						cloneGroupWithRule(user3Group1, user3Group1Rule1),
						cloneGroupWithRule(user3Group1, user3Group1Rule2),
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
			t.Cleanup(func() { assert.NoError(t, closer.Close()) })

			setupRuler := func(id string, host string, port int, forceRing *ring.Ring) *Ruler {
				cfg := Config{
					StoreConfig:      RuleStoreConfig{mock: newMockRuleStore(allRules)},
					EnableSharding:   tc.sharding,
					ShardingStrategy: tc.shardingStrategy,
					ShardingAlgo:     tc.shardingAlgo,
					Ring: RingConfig{
						InstanceID:   id,
						InstanceAddr: host,
						InstancePort: port,
						KVStore: kv.Config{
							Mock: kvStore,
						},
						HeartbeatTimeout: 1 * time.Minute,
					},
					FlushCheckPeriod: 0,
					EnabledTenants:   tc.enabledUsers,
					DisabledTenants:  tc.disabledUsers,
				}

				m := loki_storage.NewClientMetrics()
				defer m.Unregister()
				r := buildRuler(t, cfg, nil, m, nil)
				r.limits = ruleLimits{tenantShard: tc.shuffleShardSize}

				if forceRing != nil {
					r.ring = forceRing
				}
				return r
			}

			r1 := setupRuler(ruler1, ruler1Host, ruler1Port, nil)

			rulerRing := r1.ring

			// We start ruler's ring, but nothing else (not even lifecycler).
			if rulerRing != nil {
				require.NoError(t, services.StartAndAwaitRunning(context.Background(), rulerRing))
				t.Cleanup(rulerRing.StopAsync)
			}

			var r2, r3 *Ruler
			if rulerRing != nil {
				// Reuse ring from r1.
				r2 = setupRuler(ruler2, ruler2Host, ruler2Port, rulerRing)
				r3 = setupRuler(ruler3, ruler3Host, ruler3Port, rulerRing)
			}

			if tc.setupRing != nil {
				err := kvStore.CAS(context.Background(), ringKey, func(in interface{}) (out interface{}, retry bool, err error) {
					d, _ := in.(*ring.Desc)
					if d == nil {
						d = ring.NewDesc()
					}

					tc.setupRing(d)

					return d, true, nil
				})
				require.NoError(t, err)
				// Wait a bit to make sure ruler's ring is updated.
				time.Sleep(100 * time.Millisecond)
			}

			// Always add ruler1 to expected rulers, even if there is no ring (no sharding).
			loadedRules1, err := r1.listRules(context.Background())
			require.NoError(t, err)

			actual := expectedRulesMap{
				ruler1: loadedRules1,
			}

			addToActual := func(id string, r *Ruler) {
				// Only expect rules from other rulers when using ring, and they are present in the ring.
				if r != nil && rulerRing != nil && rulerRing.HasInstance(id) {
					loaded, err := r.listRules(context.Background())
					require.NoError(t, err)
					// Normalize nil map to empty one.
					if loaded == nil {
						loaded = map[string]rulespb.RuleGroupList{}
					}
					actual[id] = loaded
				}
			}

			addToActual(ruler2, r2)
			addToActual(ruler3, r3)

			require.Equal(t, tc.expectedRules, actual)
		})
	}
}

// User shuffle shard token.
func userToken(user string, skip int) uint32 {
	r := rand.New(rand.NewSource(util.ShuffleShardSeed(user, "")))

	for ; skip > 0; skip-- {
		_ = r.Uint32()
	}
	return r.Uint32()
}

func sortTokens(tokens []uint32) []uint32 {
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i] < tokens[j]
	})
	return tokens
}

func TestDeleteTenantRuleGroups(t *testing.T) {
	ruleGroups := []ruleGroupKey{
		{user: "userA", namespace: "namespace", group: "group"},
		{user: "userB", namespace: "namespace1", group: "group"},
		{user: "userB", namespace: "namespace2", group: "group"},
	}

	obj, rs := setupRuleGroupsStore(t, ruleGroups)
	require.Equal(t, 3, obj.GetObjectCount())

	api, err := NewRuler(Config{}, nil, nil, log.NewNopLogger(), rs, nil, constants.Loki)
	require.NoError(t, err)

	{
		req := &http.Request{}
		resp := httptest.NewRecorder()
		api.DeleteTenantConfiguration(resp, req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	}

	{
		callDeleteTenantAPI(t, api, "user-with-no-rule-groups")
		require.Equal(t, 3, obj.GetObjectCount())

		verifyExpectedDeletedRuleGroupsForUser(t, api, "user-with-no-rule-groups", true) // Has no rule groups
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userA", false)
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userB", false)
	}

	{
		callDeleteTenantAPI(t, api, "userA")
		require.Equal(t, 2, obj.GetObjectCount())

		verifyExpectedDeletedRuleGroupsForUser(t, api, "user-with-no-rule-groups", true) // Has no rule groups
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userA", true)                    // Just deleted.
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userB", false)
	}

	// Deleting same user again works fine and reports no problems.
	{
		callDeleteTenantAPI(t, api, "userA")
		require.Equal(t, 2, obj.GetObjectCount())

		verifyExpectedDeletedRuleGroupsForUser(t, api, "user-with-no-rule-groups", true) // Has no rule groups
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userA", true)                    // Already deleted before.
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userB", false)
	}

	{
		callDeleteTenantAPI(t, api, "userB")
		require.Equal(t, 0, obj.GetObjectCount())

		verifyExpectedDeletedRuleGroupsForUser(t, api, "user-with-no-rule-groups", true) // Has no rule groups
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userA", true)                    // Deleted previously
		verifyExpectedDeletedRuleGroupsForUser(t, api, "userB", true)                    // Just deleted
	}
}

func generateTokenForGroups(groups []*rulespb.RuleGroupDesc, offset uint32) []uint32 {
	var tokens []uint32

	for _, g := range groups {
		tokens = append(tokens, tokenForGroup(g)+offset)
	}

	return tokens
}

func callDeleteTenantAPI(t *testing.T, api *Ruler, userID string) {
	ctx := user.InjectOrgID(context.Background(), userID)

	req := &http.Request{}
	resp := httptest.NewRecorder()
	api.DeleteTenantConfiguration(resp, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, resp.Code)
}

func verifyExpectedDeletedRuleGroupsForUser(t *testing.T, r *Ruler, userID string, expectedDeleted bool) {
	list, err := r.store.ListRuleGroupsForUserAndNamespace(context.Background(), userID, "")
	require.NoError(t, err)

	if expectedDeleted {
		require.Equal(t, 0, len(list))
	} else {
		require.NotEqual(t, 0, len(list))
	}
}

func setupRuleGroupsStore(t *testing.T, ruleGroups []ruleGroupKey) (*testutils.MockStorage, rulestore.RuleStore) {
	obj := testutils.NewMockStorage()
	rs := objectclient.NewRuleStore(obj, 5, log.NewNopLogger())
	testutils.ResetMockStorage()
	// "upload" rule groups
	for _, key := range ruleGroups {
		desc := rulespb.ToProto(key.user, key.namespace, rulefmt.RuleGroup{Name: key.group})
		require.NoError(t, rs.SetRuleGroup(context.Background(), key.user, key.namespace, desc))
	}

	return obj, rs
}

type ruleGroupKey struct {
	user, namespace, group string
}

func TestRuler_ListAllRules(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRules))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	router := mux.NewRouter()
	router.Path("/ruler/rule_groups").Methods(http.MethodGet).HandlerFunc(r.ListAllRules)

	req := requestFor(t, http.MethodGet, "https://localhost:8080/ruler/rule_groups", nil, "")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// Check status code and header
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/yaml", resp.Header.Get("Content-Type"))

	gs := make(map[string]map[string][]rulefmt.RuleGroup) // user:namespace:[]rulefmt.RuleGroup
	for userID := range mockRules {
		gs[userID] = mockRules[userID].Formatted()
	}
	expectedResponse, err := yaml.Marshal(gs)
	require.NoError(t, err)
	require.YAMLEq(t, string(expectedResponse), string(body))
}

type senderFunc func(alerts ...*notifier.Alert)

func (s senderFunc) Send(alerts ...*notifier.Alert) {
	s(alerts...)
}

func TestSendAlerts(t *testing.T) {
	escapedExpression := url.QueryEscape("{\"expr\":\"up\",\"queryType\":\"range\",\"datasource\":{\"type\":\"loki\",\"uid\":\"uid\"}}")
	testCases := []struct {
		in  []*promRules.Alert
		exp []*notifier.Alert
	}{
		{
			in: []*promRules.Alert{
				{
					Labels:      []labels.Label{{Name: "l1", Value: "v1"}},
					Annotations: []labels.Label{{Name: "a2", Value: "v2"}},
					ActiveAt:    time.Unix(1, 0),
					FiredAt:     time.Unix(2, 0),
					ValidUntil:  time.Unix(3, 0),
				},
			},
			exp: []*notifier.Alert{
				{
					Labels:       []labels.Label{{Name: "l1", Value: "v1"}},
					Annotations:  []labels.Label{{Name: "a2", Value: "v2"}},
					StartsAt:     time.Unix(2, 0),
					EndsAt:       time.Unix(3, 0),
					GeneratorURL: fmt.Sprintf("http://localhost:8080/explore?left={\"queries\":[%s]}", escapedExpression),
				},
			},
		},
		{
			in: []*promRules.Alert{
				{
					Labels:      []labels.Label{{Name: "l1", Value: "v1"}},
					Annotations: []labels.Label{{Name: "a2", Value: "v2"}},
					ActiveAt:    time.Unix(1, 0),
					FiredAt:     time.Unix(2, 0),
					ResolvedAt:  time.Unix(4, 0),
				},
			},
			exp: []*notifier.Alert{
				{
					Labels:       []labels.Label{{Name: "l1", Value: "v1"}},
					Annotations:  []labels.Label{{Name: "a2", Value: "v2"}},
					StartsAt:     time.Unix(2, 0),
					EndsAt:       time.Unix(4, 0),
					GeneratorURL: fmt.Sprintf("http://localhost:8080/explore?left={\"queries\":[%s]}", escapedExpression),
				},
			},
		},
		{
			in: []*promRules.Alert{},
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			senderFunc := senderFunc(func(alerts ...*notifier.Alert) {
				if len(tc.in) == 0 {
					t.Fatalf("sender called with 0 alert")
				}
				require.Equal(t, tc.exp, alerts)
			})
			SendAlerts(senderFunc, "http://localhost:8080", "uid")(context.TODO(), "up", tc.in...)
		})
	}
}

type fakeQuerier struct {
	fn func(sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet
}

func (f *fakeQuerier) Select(_ context.Context, sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	return f.fn(sortSeries, hints, matchers...)
}

func (f *fakeQuerier) LabelValues(_ context.Context, _ string, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}

func (f *fakeQuerier) LabelNames(_ context.Context, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (f *fakeQuerier) Close() error { return nil }

// Tests for whether the Ruler is able to recover ALERTS_FOR_STATE state
func TestRecoverAlertsPostOutage(t *testing.T) {
	// Test Setup
	// alert FOR 30m, already ran for 10m, outage down at 15m prior to now(), outage tolerance set to 1hr
	// EXPECTATION: for state for alert restores to 10m+(now-15m)

	// FIRST set up 1 Alert rule with 30m FOR duration
	alertForDuration, _ := time.ParseDuration("30m")
	mockRules := map[string]rulespb.RuleGroupList{
		"user1": {
			&rulespb.RuleGroupDesc{
				Name:      "group1",
				Namespace: "namespace1",
				User:      "user1",
				Rules: []*rulespb.RuleDesc{
					{
						Alert: "UP_ALERT",
						Expr:  "1", // always fire for this test
						For:   alertForDuration,
					},
				},
				Interval: interval,
				Limit:    limit,
			},
		},
	}

	// NEXT, set up ruler config with outage tolerance = 1hr
	rulerCfg := defaultRulerConfig(t, newMockRuleStore(mockRules))
	rulerCfg.OutageTolerance, _ = time.ParseDuration("1h")

	// NEXT, set up mock distributor containing sample,
	// metric: ALERTS_FOR_STATE{alertname="UP_ALERT"}, ts: time.now()-15m, value: time.now()-25m
	currentTime := time.Now().UTC()
	downAtTime := currentTime.Add(time.Minute * -15)
	downAtTimeMs := downAtTime.UnixNano() / int64(time.Millisecond)
	downAtActiveAtTime := currentTime.Add(time.Minute * -25)
	downAtActiveSec := downAtActiveAtTime.Unix()

	m := loki_storage.NewClientMetrics()
	defer m.Unregister()
	// create a ruler but don't start it. instead, we'll evaluate the rule groups manually.
	r := buildRuler(t, rulerCfg, &fakeQuerier{
		fn: func(_ bool, _ *storage.SelectHints, _ ...*labels.Matcher) storage.SeriesSet {
			return series.NewConcreteSeriesSet([]storage.Series{
				series.NewConcreteSeries(
					labels.Labels{
						{Name: labels.MetricName, Value: "ALERTS_FOR_STATE"},
						{Name: labels.AlertName, Value: mockRules["user1"][0].GetRules()[0].Alert},
					},
					[]model.SamplePair{{Timestamp: model.Time(downAtTimeMs), Value: model.SampleValue(downAtActiveSec)}},
				),
			})
		},
	}, m, nil)
	r.syncRules(context.Background(), rulerSyncReasonInitial)

	// assert initial state of rule group
	ruleGroup := r.manager.GetRules("user1")[0]
	require.Equal(t, time.Time{}, ruleGroup.GetLastEvaluation())
	require.Equal(t, "group1", ruleGroup.Name())
	require.Equal(t, 1, len(ruleGroup.Rules()))

	// assert initial state of rule within rule group
	alertRule := ruleGroup.Rules()[0]
	require.Equal(t, time.Time{}, alertRule.GetEvaluationTimestamp())
	require.Equal(t, "UP_ALERT", alertRule.Name())
	require.Equal(t, promRules.HealthUnknown, alertRule.Health())

	// NEXT, evaluate the rule group the first time and assert
	ctx := user.InjectOrgID(context.Background(), "user1")
	ruleGroup.Eval(ctx, currentTime)

	// since the eval is done at the current timestamp, the activeAt timestamp of alert should equal current timestamp
	require.Equal(t, "UP_ALERT", alertRule.Name())
	require.Equal(t, promRules.HealthGood, alertRule.Health())

	activeMapRaw := reflect.ValueOf(alertRule).Elem().FieldByName("active")
	activeMapKeys := activeMapRaw.MapKeys()
	require.True(t, len(activeMapKeys) == 1)

	activeAlertRuleRaw := activeMapRaw.MapIndex(activeMapKeys[0]).Elem()
	activeAtTimeRaw := activeAlertRuleRaw.FieldByName("ActiveAt")

	require.Equal(t, promRules.StatePending, promRules.AlertState(activeAlertRuleRaw.FieldByName("State").Int()))
	require.Equal(t, reflect.NewAt(activeAtTimeRaw.Type(), unsafe.Pointer(activeAtTimeRaw.UnsafeAddr())).Elem().Interface().(time.Time), currentTime)

	// NEXT, restore the FOR state and assert
	ruleGroup.RestoreForState(currentTime)

	require.Equal(t, "UP_ALERT", alertRule.Name())
	require.Equal(t, promRules.HealthGood, alertRule.Health())
	require.Equal(t, promRules.StatePending, promRules.AlertState(activeAlertRuleRaw.FieldByName("State").Int()))
	require.Equal(t, reflect.NewAt(activeAtTimeRaw.Type(), unsafe.Pointer(activeAtTimeRaw.UnsafeAddr())).Elem().Interface().(time.Time), downAtActiveAtTime.Add(currentTime.Sub(downAtTime)))

	// NEXT, 20 minutes is expected to be left, eval timestamp at currentTimestamp +20m
	currentTime = currentTime.Add(time.Minute * 20)
	ruleGroup.Eval(ctx, currentTime)

	// assert alert state after alert is firing
	firedAtRaw := activeAlertRuleRaw.FieldByName("FiredAt")
	firedAtTime := reflect.NewAt(firedAtRaw.Type(), unsafe.Pointer(firedAtRaw.UnsafeAddr())).Elem().Interface().(time.Time)
	require.Equal(t, firedAtTime, currentTime)

	require.Equal(t, promRules.StateFiring, promRules.AlertState(activeAlertRuleRaw.FieldByName("State").Int()))
}

func TestRuleGroupAlertsAndSeriesLimit(t *testing.T) {

	currentTime := time.Now().UTC()
	seriesStartTime := currentTime.Add(time.Minute * -10)
	sampleTimeDiff := 5 * time.Minute

	testCases := []struct {
		name               string
		rule               *rulespb.RuleDesc
		limit              int64
		expectedRuleHealth promRules.RuleHealth
		expectedError      error
	}{
		{
			name:               "AlertingRule alerts within limit",
			rule:               getMockRule("HIGH_HTTP_REQUESTS", "http_requests > 50", "", 2*time.Minute),
			limit:              2,
			expectedRuleHealth: promRules.HealthGood,
			expectedError:      nil,
		},
		{
			name:               "AlertingRule alerts with limit 0",
			rule:               getMockRule("HIGH_HTTP_REQUESTS", "http_requests > 50", "", 2*time.Minute),
			limit:              0,
			expectedRuleHealth: promRules.HealthGood,
			expectedError:      nil,
		},
		{
			name:               "AlertingRule alerts exceeding limit",
			rule:               getMockRule("HIGH_HTTP_REQUESTS", "http_requests > 50", "", 2*time.Minute),
			limit:              1,
			expectedRuleHealth: promRules.HealthBad,
			expectedError:      errors.New("exceeded limit of 1 with 2 alerts"),
		},
		{
			name:               "RecordingRule series within limit",
			rule:               getMockRule("", "sum by (instance) (http_requests)", "total_http_requests_per_instance", 0),
			limit:              2,
			expectedRuleHealth: promRules.HealthGood,
			expectedError:      nil,
		},
		{
			name:               "RecordingRule series with limit 0",
			rule:               getMockRule("", "sum by (instance) (http_requests)", "total_http_requests_per_instance", 0),
			limit:              0,
			expectedRuleHealth: promRules.HealthGood,
			expectedError:      nil,
		},
		{
			name:               "RecordingRule series exceeding limit",
			rule:               getMockRule("", "sum by (instance) (http_requests)", "total_http_requests_per_instance", 0),
			limit:              1,
			expectedRuleHealth: promRules.HealthBad,
			expectedError:      errors.New("exceeded limit of 1 with 2 series"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(tt *testing.T) {

			mockRuleGroupList := map[string]rulespb.RuleGroupList{
				"user1": {
					&rulespb.RuleGroupDesc{
						Name:      "group1",
						Namespace: "namespace1",
						User:      "user1",
						Interval:  interval,
						Limit:     tc.limit,
						Rules:     []*rulespb.RuleDesc{tc.rule},
					},
				},
			}

			rulerCfg := defaultRulerConfig(t, newMockRuleStore(mockRuleGroupList))
			m := loki_storage.NewClientMetrics()
			defer m.Unregister()

			r := buildRuler(tt, rulerCfg, &fakeQuerier{
				fn: func(_ bool, _ *storage.SelectHints, _ ...*labels.Matcher) storage.SeriesSet {
					return series.NewConcreteSeriesSet([]storage.Series{
						series.NewConcreteSeries(
							labels.Labels{
								{Name: labels.MetricName, Value: "http_requests"},
								{Name: labels.InstanceName, Value: "server1"},
							},
							[]model.SamplePair{
								{Timestamp: model.Time(seriesStartTime.Add(sampleTimeDiff).UnixMilli()), Value: 100},
								{Timestamp: model.Time(currentTime.UnixMilli()), Value: 100},
							},
						),
						series.NewConcreteSeries(
							labels.Labels{
								{Name: labels.MetricName, Value: "http_requests"},
								{Name: labels.InstanceName, Value: "server2"},
							},
							[]model.SamplePair{
								{Timestamp: model.Time(seriesStartTime.Add(sampleTimeDiff).UnixMilli()), Value: 100},
								{Timestamp: model.Time(currentTime.UnixMilli()), Value: 100},
							},
						),
					})
				},
			}, m, nil)

			r.syncRules(context.Background(), rulerSyncReasonInitial)

			// assert initial state of rule group
			ruleGroup := r.manager.GetRules("user1")[0]
			require.Equal(tt, time.Time{}, ruleGroup.GetLastEvaluation())
			require.Equal(tt, "group1", ruleGroup.Name())
			require.Equal(tt, 1, len(ruleGroup.Rules()))

			// assert initial state of rule within rule group
			rule := ruleGroup.Rules()[0]
			require.Equal(tt, time.Time{}, rule.GetEvaluationTimestamp())
			require.Equal(tt, promRules.HealthUnknown, rule.Health())

			// evaluate the rule group the first time and assert
			ctx := user.InjectOrgID(context.Background(), "user1")
			ruleGroup.Eval(ctx, currentTime)

			require.Equal(tt, tc.expectedRuleHealth, rule.Health())
			require.Equal(tt, tc.expectedError, rule.LastError())
		})

	}
}

func getMockRule(alert, expr, record string, forDuration time.Duration) *rulespb.RuleDesc {
	return &rulespb.RuleDesc{
		Alert:  alert,
		Expr:   expr,
		For:    forDuration,
		Record: record,
	}
}
