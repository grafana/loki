package base

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	rulerconfig "github.com/grafana/loki/v3/pkg/ruler/config"
	lokiutil "github.com/grafana/loki/v3/pkg/util"
	"github.com/prometheus/prometheus/notifier"
	promRules "github.com/prometheus/prometheus/rules"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/test"
)

func TestSyncRuleGroups(t *testing.T) {
	dir := t.TempDir()

	m, err := NewDefaultMultiTenantManager(Config{RulePath: dir}, factory, nil, log.NewNopLogger(), ruleLimits{}, constants.Loki)
	require.NoError(t, err)

	const user = "testUser"

	userRules := map[string]rulespb.RuleGroupList{
		user: {
			&rulespb.RuleGroupDesc{
				Name:      "group1",
				Namespace: "ns",
				Interval:  1 * time.Minute,
				User:      user,
				Limit:     10,
			},
		},
	}
	m.SyncRuleGroups(context.Background(), userRules)

	mgr := getManager(m, user)
	require.NotNil(t, mgr)

	test.Poll(t, 1*time.Second, true, func() interface{} {
		return mgr.(*mockRulesManager).running.Load()
	})

	// Verify that user rule groups are now cached locally.
	{
		users, err := m.mapper.users()
		require.NoError(t, err)
		require.Equal(t, []string{user}, users)
	}

	// Passing empty map / nil stops all managers.
	m.SyncRuleGroups(context.Background(), nil)
	require.Nil(t, getManager(m, user))

	// Make sure old manager was stopped.
	test.Poll(t, 1*time.Second, false, func() interface{} {
		return mgr.(*mockRulesManager).running.Load()
	})

	// Verify that local rule groups were removed.
	{
		users, err := m.mapper.users()
		require.NoError(t, err)
		require.Equal(t, []string(nil), users)
	}

	// Resync same rules as before. Previously this didn't restart the manager.
	m.SyncRuleGroups(context.Background(), userRules)

	newMgr := getManager(m, user)
	require.NotNil(t, newMgr)
	require.True(t, mgr != newMgr)

	test.Poll(t, 1*time.Second, true, func() interface{} {
		return newMgr.(*mockRulesManager).running.Load()
	})

	// Verify that user rule groups are cached locally again.
	{
		users, err := m.mapper.users()
		require.NoError(t, err)
		require.Equal(t, []string{user}, users)
	}

	m.Stop()

	test.Poll(t, 1*time.Second, false, func() interface{} {
		return newMgr.(*mockRulesManager).running.Load()
	})
}

func getManager(m *DefaultMultiTenantManager, user string) RulesManager {
	m.userManagerMtx.Lock()
	defer m.userManagerMtx.Unlock()

	return m.userManagers[user]
}

func factory(_ context.Context, _ string, _ *notifier.Manager, _ log.Logger, _ prometheus.Registerer) RulesManager {
	return &mockRulesManager{done: make(chan struct{})}
}

type mockRulesManager struct {
	running atomic.Bool
	done    chan struct{}
}

func (m *mockRulesManager) Run() {
	m.running.Store(true)
	<-m.done
}

func (m *mockRulesManager) Stop() {
	m.running.Store(false)
	close(m.done)
}

func (m *mockRulesManager) Update(_ time.Duration, _ []string, _ labels.Labels, _ string, _ promRules.GroupEvalIterationFunc) error {
	return nil
}

func (m *mockRulesManager) RuleGroups() []*promRules.Group {
	return nil
}

func TestNotifierRebuiltOnAMConfigChange(t *testing.T) {
	dir := t.TempDir()
	const user = "testUser"

	limits := ruleLimits{
		alertManagerConfig: map[string]*rulerconfig.AlertManagerConfig{
			user: {AlertmanagerURL: "http://alertmanager-initial:9093"},
		},
	}

	m, err := NewDefaultMultiTenantManager(Config{RulePath: dir}, factory, prometheus.NewRegistry(), log.NewNopLogger(), limits, "test")
	require.NoError(t, err)

	userRules := map[string]rulespb.RuleGroupList{
		user: {{Name: "group1", Namespace: "ns", Interval: 1 * time.Minute, User: user}},
	}
	m.SyncRuleGroups(context.Background(), userRules)

	require.False(t, m.amConfigChanged(user))

	// Change the alertmanager URL — amConfigChanged should detect it.
	limits.alertManagerConfig[user] = &rulerconfig.AlertManagerConfig{AlertmanagerURL: "http://alertmanager-updated:9093"}
	m.limits = limits
	require.True(t, m.amConfigChanged(user))

	// After sync the hash is updated and the change is no longer detected.
	m.SyncRuleGroups(context.Background(), userRules)
	require.False(t, m.amConfigChanged(user))

	m.Stop()
}

func TestNotifierHashUnchangedWhenConfigUnchanged(t *testing.T) {
	dir := t.TempDir()
	const user = "testUser"

	limits := ruleLimits{
		alertManagerConfig: map[string]*rulerconfig.AlertManagerConfig{
			user: {AlertmanagerURL: "http://alertmanager:9093"},
		},
	}

	m, err := NewDefaultMultiTenantManager(Config{RulePath: dir}, factory, prometheus.NewRegistry(), log.NewNopLogger(), limits, "test")
	require.NoError(t, err)

	userRules := map[string]rulespb.RuleGroupList{
		user: {{Name: "group1", Namespace: "ns", Interval: 1 * time.Minute, User: user}},
	}
	m.SyncRuleGroups(context.Background(), userRules)
	require.False(t, m.amConfigChanged(user))

	m.SyncRuleGroups(context.Background(), userRules)
	require.False(t, m.amConfigChanged(user))

	m.Stop()
}

func TestNoNotifierForUnknownTenant(t *testing.T) {
	dir := t.TempDir()
	m, err := NewDefaultMultiTenantManager(Config{RulePath: dir}, factory, nil, log.NewNopLogger(), ruleLimits{}, "test")
	require.NoError(t, err)
	require.False(t, m.amConfigChanged("unknown-tenant"))
}

// TestNotifierAppliesURLChange verifies that after a sync following an alertmanager URL change,
// the notifier's active targets reflect the new URL.
func TestNotifierAppliesURLChange(t *testing.T) {
	dir := t.TempDir()
	const user = "testUser"

	limits := ruleLimits{
		alertManagerConfig: map[string]*rulerconfig.AlertManagerConfig{
			user: {AlertmanagerURL: "http://alertmanager-initial:9093"},
		},
	}

	m, err := NewDefaultMultiTenantManager(Config{RulePath: dir}, factory, prometheus.NewRegistry(), log.NewNopLogger(), limits, "test")
	require.NoError(t, err)

	userRules := map[string]rulespb.RuleGroupList{
		user: {{Name: "group1", Namespace: "ns", Interval: 1 * time.Minute, User: user}},
	}
	m.SyncRuleGroups(context.Background(), userRules)

	// Verify the initial URL is active. The notifier appends /api/v1/alerts to the path.
	test.Poll(t, 2*time.Second, "http://alertmanager-initial:9093/api/v1/alerts", func() interface{} {
		m.notifiersMtx.Lock()
		defer m.notifiersMtx.Unlock()
		n, ok := m.notifiers[user]
		if !ok || len(n.notifier.Alertmanagers()) == 0 {
			return ""
		}
		return n.notifier.Alertmanagers()[0].String()
	})

	// Update the alertmanager URL and re-sync.
	limits.alertManagerConfig[user] = &rulerconfig.AlertManagerConfig{AlertmanagerURL: "http://alertmanager-updated:9093"}
	m.limits = limits
	m.SyncRuleGroups(context.Background(), userRules)

	// Verify the notifier now points at the new URL.
	test.Poll(t, 2*time.Second, "http://alertmanager-updated:9093/api/v1/alerts", func() interface{} {
		m.notifiersMtx.Lock()
		defer m.notifiersMtx.Unlock()
		n, ok := m.notifiers[user]
		if !ok || len(n.notifier.Alertmanagers()) == 0 {
			return ""
		}
		return n.notifier.Alertmanagers()[0].String()
	})

	m.Stop()
}

// TestNotifierDetectsCredentialRotation verifies that rotating a password or bearer token
// in the alertmanager config is detected as a change. Previously this was broken because
// flagext.Secret.MarshalYAML redacts values to "********", making the hash identical.
func TestNotifierDetectsCredentialRotation(t *testing.T) {
	dir := t.TempDir()
	const user = "testUser"

	limits := ruleLimits{
		alertManagerConfig: map[string]*rulerconfig.AlertManagerConfig{
			user: {
				AlertmanagerURL: "http://alertmanager:9093",
				Notifier: rulerconfig.NotifierConfig{
					BasicAuth: lokiutil.BasicAuth{
						Username: "user",
						Password: flagext.SecretWithValue("initial-password"),
					},
				},
			},
		},
	}

	m, err := NewDefaultMultiTenantManager(Config{RulePath: dir}, factory, prometheus.NewRegistry(), log.NewNopLogger(), limits, "test")
	require.NoError(t, err)

	userRules := map[string]rulespb.RuleGroupList{
		user: {{Name: "group1", Namespace: "ns", Interval: 1 * time.Minute, User: user}},
	}
	m.SyncRuleGroups(context.Background(), userRules)
	require.False(t, m.amConfigChanged(user))

	// Rotate the password — amConfigChanged must detect this.
	limits.alertManagerConfig[user] = &rulerconfig.AlertManagerConfig{
		AlertmanagerURL: "http://alertmanager:9093",
		Notifier: rulerconfig.NotifierConfig{
			BasicAuth: lokiutil.BasicAuth{
				Username: "user",
				Password: flagext.SecretWithValue("rotated-password"),
			},
		},
	}
	m.limits = limits
	require.True(t, m.amConfigChanged(user), "credential rotation must be detected")

	// After sync the hash is updated and the change is no longer detected.
	m.SyncRuleGroups(context.Background(), userRules)
	require.False(t, m.amConfigChanged(user))

	m.Stop()
}
