package configdb

import (
	"context"
	fmt "fmt"
	"testing"
	time "time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/configs/client"
	"github.com/grafana/loki/v3/pkg/configs/userconfig"
)

var zeroTime time.Time

type MockClient struct {
	cfgs map[string]userconfig.VersionedRulesConfig
	err  error
}

func (c *MockClient) GetRules(_ context.Context, _ userconfig.ID) (map[string]userconfig.VersionedRulesConfig, error) {
	return c.cfgs, c.err
}

func (c *MockClient) GetAlerts(_ context.Context, _ userconfig.ID) (*client.ConfigsResponse, error) {
	return nil, nil
}

func Test_ConfigRuleStoreError(t *testing.T) {
	mock := &MockClient{
		cfgs: nil,
		err:  fmt.Errorf("Error"),
	}

	store := NewConfigRuleStore(mock)
	_, err := store.ListAllRuleGroups(context.Background())

	assert.Equal(t, mock.err, err, "Unexpected error returned")
}

func Test_ConfigRuleStoreReturn(t *testing.T) {
	id := userconfig.ID(10)
	mock := &MockClient{
		cfgs: map[string]userconfig.VersionedRulesConfig{
			"user": {
				ID:        id,
				Config:    fakeRuleConfig(),
				DeletedAt: zeroTime,
			},
		},
		err: nil,
	}

	store := NewConfigRuleStore(mock)
	rules, _ := store.ListAllRuleGroups(context.Background())

	assert.Equal(t, 1, len(rules["user"]))
	assert.Equal(t, id, store.since)
}

func Test_ConfigRuleStoreDelete(t *testing.T) {
	mock := &MockClient{
		cfgs: map[string]userconfig.VersionedRulesConfig{
			"user": {
				ID:        1,
				Config:    fakeRuleConfig(),
				DeletedAt: zeroTime,
			},
		},
		err: nil,
	}

	store := NewConfigRuleStore(mock)
	_, _ = store.ListAllRuleGroups(context.Background())

	mock.cfgs["user"] = userconfig.VersionedRulesConfig{
		ID:        1,
		Config:    userconfig.RulesConfig{},
		DeletedAt: time.Unix(0, 1),
	}

	rules, _ := store.ListAllRuleGroups(context.Background())

	assert.Equal(t, 0, len(rules["user"]))
}

func Test_ConfigRuleStoreAppend(t *testing.T) {
	mock := &MockClient{
		cfgs: map[string]userconfig.VersionedRulesConfig{
			"user": {
				ID:        1,
				Config:    fakeRuleConfig(),
				DeletedAt: zeroTime,
			},
		},
		err: nil,
	}

	store := NewConfigRuleStore(mock)
	_, _ = store.ListAllRuleGroups(context.Background())

	delete(mock.cfgs, "user")
	mock.cfgs["user2"] = userconfig.VersionedRulesConfig{
		ID:        1,
		Config:    fakeRuleConfig(),
		DeletedAt: zeroTime,
	}

	rules, _ := store.ListAllRuleGroups(context.Background())

	assert.Equal(t, 2, len(rules))
}

func Test_ConfigRuleStoreSinceSet(t *testing.T) {
	mock := &MockClient{
		cfgs: map[string]userconfig.VersionedRulesConfig{
			"user": {
				ID:        1,
				Config:    fakeRuleConfig(),
				DeletedAt: zeroTime,
			},
			"user1": {
				ID:        10,
				Config:    fakeRuleConfig(),
				DeletedAt: zeroTime,
			},
			"user2": {
				ID:        100,
				Config:    fakeRuleConfig(),
				DeletedAt: zeroTime,
			},
		},
		err: nil,
	}

	store := NewConfigRuleStore(mock)
	_, _ = store.ListAllRuleGroups(context.Background())
	assert.Equal(t, userconfig.ID(100), store.since)

	delete(mock.cfgs, "user")
	delete(mock.cfgs, "user1")
	mock.cfgs["user2"] = userconfig.VersionedRulesConfig{
		ID:        50,
		Config:    fakeRuleConfig(),
		DeletedAt: zeroTime,
	}

	_, _ = store.ListAllRuleGroups(context.Background())
	assert.Equal(t, userconfig.ID(100), store.since)

	mock.cfgs["user2"] = userconfig.VersionedRulesConfig{
		ID:        101,
		Config:    fakeRuleConfig(),
		DeletedAt: zeroTime,
	}

	_, _ = store.ListAllRuleGroups(context.Background())
	assert.Equal(t, userconfig.ID(101), store.since)
}

func fakeRuleConfig() userconfig.RulesConfig {
	return userconfig.RulesConfig{
		FormatVersion: userconfig.RuleFormatV2,
		Files: map[string]string{
			"test": `
# Config no. 1.
groups:
- name: example
  rules:
  - alert: ScrapeFailed
    expr: 'up != 1'
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "Scrape of {{$labels.job}} (pod: {{$labels.instance}}) failed."
      description: "Prometheus cannot reach the /metrics page on the {{$labels.instance}} pod."
      impact: "We have no monitoring data for {{$labels.job}} - {{$labels.instance}}. At worst, it's completely down. At best, we cannot reliably respond to operational issues."
      dashboardURL: "$${base_url}/admin/prometheus/targets"
`,
		},
	}
}
