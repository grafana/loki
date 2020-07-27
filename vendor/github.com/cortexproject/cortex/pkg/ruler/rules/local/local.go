package local

import (
	"context"
	"flag"
	"io/ioutil"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/rulefmt"

	"github.com/cortexproject/cortex/pkg/ruler/rules"
)

type Config struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, prefix+"local.directory", "", "Directory to scan for rules")
}

// Client expects to load already existing rules located at:
//  cfg.Directory / userID / namespace
type Client struct {
	cfg Config
}

func NewLocalRulesClient(cfg Config) (*Client, error) {
	if cfg.Directory == "" {
		return nil, errors.New("directory required for local rules config")
	}

	return &Client{
		cfg: cfg,
	}, nil
}

// ListAllRuleGroups implements RuleStore
func (l *Client) ListAllRuleGroups(ctx context.Context) (map[string]rules.RuleGroupList, error) {
	lists := make(map[string]rules.RuleGroupList)

	root := l.cfg.Directory
	infos, err := ioutil.ReadDir(root)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read dir %s", root)
	}

	for _, info := range infos {
		if !info.IsDir() {
			continue
		}

		list, err := l.listAllRulesGroupsForUser(ctx, info.Name())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list rule groups for user %s", info.Name())
		}

		lists[info.Name()] = list
	}

	return lists, nil
}

// ListRuleGroups implements RuleStore
func (l *Client) ListRuleGroups(ctx context.Context, userID string, namespace string) (rules.RuleGroupList, error) {
	if namespace != "" {
		return l.listAllRulesGroupsForUserAndNamespace(ctx, userID, namespace)
	}

	return l.listAllRulesGroupsForUser(ctx, userID)
}

// GetRuleGroup implements RuleStore
func (l *Client) GetRuleGroup(ctx context.Context, userID, namespace, group string) (*rules.RuleGroupDesc, error) {
	return nil, errors.New("GetRuleGroup unsupported in rule local store")
}

// SetRuleGroup implements RuleStore
func (l *Client) SetRuleGroup(ctx context.Context, userID, namespace string, group *rules.RuleGroupDesc) error {
	return errors.New("SetRuleGroup unsupported in rule local store")
}

// DeleteRuleGroup implements RuleStore
func (l *Client) DeleteRuleGroup(ctx context.Context, userID, namespace string, group string) error {
	return errors.New("DeleteRuleGroup unsupported in rule local store")
}

func (l *Client) listAllRulesGroupsForUser(ctx context.Context, userID string) (rules.RuleGroupList, error) {
	var allLists rules.RuleGroupList

	root := filepath.Join(l.cfg.Directory, userID)
	infos, err := ioutil.ReadDir(root)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read dir %s", root)
	}

	for _, info := range infos {
		if info.IsDir() {
			continue
		}

		list, err := l.listAllRulesGroupsForUserAndNamespace(ctx, userID, info.Name())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list rule group for user %s and namespace %s", userID, info.Name())
		}

		allLists = append(allLists, list...)
	}

	return allLists, nil
}

func (l *Client) listAllRulesGroupsForUserAndNamespace(ctx context.Context, userID string, namespace string) (rules.RuleGroupList, error) {
	filename := filepath.Join(l.cfg.Directory, userID, namespace)

	rulegroups, allErrors := rulefmt.ParseFile(filename)
	if len(allErrors) > 0 {
		return nil, errors.Wrapf(allErrors[0], "error parsing %s", filename)
	}

	var list rules.RuleGroupList

	for _, group := range rulegroups.Groups {
		desc := rules.ToProto(userID, namespace, group)
		list = append(list, desc)
	}

	return list, nil
}
