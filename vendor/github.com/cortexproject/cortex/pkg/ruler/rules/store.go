package rules

import (
	"context"
	"errors"

	"github.com/prometheus/prometheus/pkg/rulefmt"

	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/configs/userconfig"
)

var (
	// ErrGroupNotFound is returned if a rule group does not exist
	ErrGroupNotFound = errors.New("group does not exist")
	// ErrGroupNamespaceNotFound is returned if a namespace does not exist
	ErrGroupNamespaceNotFound = errors.New("group namespace does not exist")
	// ErrUserNotFound is returned if the user does not currently exist
	ErrUserNotFound = errors.New("no rule groups found for user")
)

// RuleStore is used to store and retrieve rules.
// Methods starting with "List" prefix may return partially loaded groups: with only group Name, Namespace and User fields set.
// To make sure that rules within each group are loaded, client must use LoadRuleGroups method.
type RuleStore interface {
	ListAllUsers(ctx context.Context) ([]string, error)
	ListAllRuleGroups(ctx context.Context) (map[string]RuleGroupList, error)
	// ListRuleGroupsForUserAndNamespace returns all the active rule groups for a user from given namespace.
	// If namespace is empty, groups from all namespaces are returned.
	ListRuleGroupsForUserAndNamespace(ctx context.Context, userID string, namespace string) (RuleGroupList, error)

	// LoadRuleGroups loads rules for each rule group in the map.
	// Parameter with groups to load *MUST* be coming from one of the List methods.
	// Reason is that some implementations don't do anything, since their List method already loads the rules.
	LoadRuleGroups(ctx context.Context, groupsToLoad map[string]RuleGroupList) error

	GetRuleGroup(ctx context.Context, userID, namespace, group string) (*RuleGroupDesc, error)
	SetRuleGroup(ctx context.Context, userID, namespace string, group *RuleGroupDesc) error
	DeleteRuleGroup(ctx context.Context, userID, namespace string, group string) error
	DeleteNamespace(ctx context.Context, userID, namespace string) error
}

// RuleGroupList contains a set of rule groups
type RuleGroupList []*RuleGroupDesc

// Formatted returns the rule group list as a set of formatted rule groups mapped
// by namespace
func (l RuleGroupList) Formatted() map[string][]rulefmt.RuleGroup {
	ruleMap := map[string][]rulefmt.RuleGroup{}
	for _, g := range l {
		if _, exists := ruleMap[g.Namespace]; !exists {
			ruleMap[g.Namespace] = []rulefmt.RuleGroup{FromProto(g)}
			continue
		}
		ruleMap[g.Namespace] = append(ruleMap[g.Namespace], FromProto(g))

	}
	return ruleMap
}

// ConfigRuleStore is a concrete implementation of RuleStore that sources rules from the config service
type ConfigRuleStore struct {
	configClient  client.Client
	since         userconfig.ID
	ruleGroupList map[string]RuleGroupList
}

// NewConfigRuleStore constructs a ConfigRuleStore
func NewConfigRuleStore(c client.Client) *ConfigRuleStore {
	return &ConfigRuleStore{
		configClient:  c,
		since:         0,
		ruleGroupList: make(map[string]RuleGroupList),
	}
}

func (c *ConfigRuleStore) ListAllUsers(ctx context.Context) ([]string, error) {
	m, err := c.ListAllRuleGroups(ctx)

	// TODO: this should be optimized, if possible.
	result := []string(nil)
	for u := range m {
		result = append(result, u)
	}

	return result, err
}

// ListAllRuleGroups implements RuleStore
func (c *ConfigRuleStore) ListAllRuleGroups(ctx context.Context) (map[string]RuleGroupList, error) {
	configs, err := c.configClient.GetRules(ctx, c.since)

	if err != nil {
		return nil, err
	}

	for user, cfg := range configs {
		userRules := RuleGroupList{}
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
				userRules = append(userRules, ToProto(user, file, rg))
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

func (c *ConfigRuleStore) ListRuleGroupsForUserAndNamespace(ctx context.Context, userID string, namespace string) (RuleGroupList, error) {
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

func (c *ConfigRuleStore) LoadRuleGroups(ctx context.Context, groupsToLoad map[string]RuleGroupList) error {
	// Since ConfigRuleStore already Loads the rules in the List methods, there is nothing left to do here.
	return nil
}

// GetRuleGroup is not implemented
func (c *ConfigRuleStore) GetRuleGroup(ctx context.Context, userID, namespace, group string) (*RuleGroupDesc, error) {
	return nil, errors.New("not implemented by the config service rule store")
}

// SetRuleGroup is not implemented
func (c *ConfigRuleStore) SetRuleGroup(ctx context.Context, userID, namespace string, group *RuleGroupDesc) error {
	return errors.New("not implemented by the config service rule store")
}

// DeleteRuleGroup is not implemented
func (c *ConfigRuleStore) DeleteRuleGroup(ctx context.Context, userID, namespace string, group string) error {
	return errors.New("not implemented by the config service rule store")
}

// DeleteNamespace is not implemented
func (c *ConfigRuleStore) DeleteNamespace(ctx context.Context, userID, namespace string) error {
	return errors.New("not implemented by the config service rule store")
}
