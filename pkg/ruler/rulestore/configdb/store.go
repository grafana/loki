package configdb

import (
	"context"
	"errors"

	"github.com/grafana/loki/v3/pkg/configs/client"
	"github.com/grafana/loki/v3/pkg/configs/userconfig"
	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
)

const (
	Name = "configdb"
)

// ConfigRuleStore is a concrete implementation of RuleStore that sources rules from the config service
type ConfigRuleStore struct {
	configClient  client.Client
	since         userconfig.ID
	ruleGroupList map[string]rulespb.RuleGroupList
}

func (c *ConfigRuleStore) SupportsModifications() bool {
	return false
}

// NewConfigRuleStore constructs a ConfigRuleStore
func NewConfigRuleStore(c client.Client) *ConfigRuleStore {
	return &ConfigRuleStore{
		configClient:  c,
		since:         0,
		ruleGroupList: make(map[string]rulespb.RuleGroupList),
	}
}

func (c *ConfigRuleStore) ListAllUsers(ctx context.Context) ([]string, error) {
	m, err := c.ListAllRuleGroups(ctx)

	result := make([]string, 0, len(m))
	for u := range m {
		result = append(result, u)
	}

	return result, err
}

// ListAllRuleGroups implements RuleStore
func (c *ConfigRuleStore) ListAllRuleGroups(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	configs, err := c.configClient.GetRules(ctx, c.since)

	if err != nil {
		return nil, err
	}

	for user, cfg := range configs {
		userRules := rulespb.RuleGroupList{}
		if cfg.IsDeleted() {
			delete(c.ruleGroupList, user)
			continue
		}
		rMap, err := cfg.Config.ParseFormatted()
		if err != nil {
			return nil, err
		}
		for file, rgs := range rMap {
			for _, rg := range rgs.Groups {
				userRules = append(userRules, rulespb.ToProto(user, file, rg))
			}
		}
		c.ruleGroupList[user] = userRules
	}

	c.since = getLatestConfigID(configs, c.since)

	return c.ruleGroupList, nil
}

// getLatestConfigID gets the latest configs ID.
// max [latest, max (map getID cfgs)]
func getLatestConfigID(cfgs map[string]userconfig.VersionedRulesConfig, latest userconfig.ID) userconfig.ID {
	ret := latest
	for _, config := range cfgs {
		if config.ID > ret {
			ret = config.ID
		}
	}
	return ret
}

func (c *ConfigRuleStore) ListRuleGroupsForUserAndNamespace(ctx context.Context, userID string, namespace string) (rulespb.RuleGroupList, error) {
	r, err := c.ListAllRuleGroups(ctx)
	if err != nil {
		return nil, err
	}

	if namespace == "" {
		return r[userID], nil
	}

	list := r[userID]
	for ix := 0; ix < len(list); {
		if list[ix].GetNamespace() != namespace {
			list = append(list[:ix], list[ix+1:]...)
		} else {
			ix++
		}
	}

	return list, nil
}

func (c *ConfigRuleStore) LoadRuleGroups(_ context.Context, _ map[string]rulespb.RuleGroupList) error {
	// Since ConfigRuleStore already Loads the rules in the List methods, there is nothing left to do here.
	return nil
}

// GetRuleGroup is not implemented
func (c *ConfigRuleStore) GetRuleGroup(_ context.Context, _, _, _ string) (*rulespb.RuleGroupDesc, error) {
	return nil, errors.New("not implemented by the config service rule store")
}

// SetRuleGroup is not implemented
func (c *ConfigRuleStore) SetRuleGroup(_ context.Context, _, _ string, _ *rulespb.RuleGroupDesc) error {
	return errors.New("not implemented by the config service rule store")
}

// DeleteRuleGroup is not implemented
func (c *ConfigRuleStore) DeleteRuleGroup(_ context.Context, _, _ string, _ string) error {
	return errors.New("not implemented by the config service rule store")
}

// DeleteNamespace is not implemented
func (c *ConfigRuleStore) DeleteNamespace(_ context.Context, _, _ string) error {
	return errors.New("not implemented by the config service rule store")
}
