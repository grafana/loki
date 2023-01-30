package local

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	promRules "github.com/prometheus/prometheus/rules"

	"github.com/grafana/loki/pkg/ruler/rulespb"
)

const (
	Name = "local"
)

type Config struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, prefix+"local.directory", "", "Directory to scan for rules")
}

// Client expects to load already existing rules located at:
//
//	cfg.Directory / userID / namespace
type Client struct {
	cfg    Config
	loader promRules.GroupLoader
}

func NewLocalRulesClient(cfg Config, loader promRules.GroupLoader) (*Client, error) {
	if cfg.Directory == "" {
		return nil, errors.New("directory required for local rules config")
	}

	return &Client{
		cfg:    cfg,
		loader: loader,
	}, nil
}

func (l *Client) ListAllUsers(ctx context.Context) ([]string, error) {
	root := l.cfg.Directory
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read dir %s", root)
	}

	var result []string
	for _, entry := range dirEntries {
		// After resolving link, entry.Name() may be different than user, so keep original name.
		user := entry.Name()

		var isDir bool

		if entry.Type()&os.ModeSymlink != 0 {
			// os.ReadDir only returns result of LStat. Calling Stat resolves symlink.
			fi, err := os.Stat(filepath.Join(root, entry.Name()))
			if err != nil {
				return nil, err
			}

			isDir = fi.IsDir()
		} else {
			isDir = entry.IsDir()
		}

		if isDir {
			result = append(result, user)
		}
	}

	return result, nil
}

// ListAllRuleGroups implements rules.RuleStore. This method also loads the rules.
func (l *Client) ListAllRuleGroups(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	users, err := l.ListAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	lists := make(map[string]rulespb.RuleGroupList)
	for _, user := range users {
		list, err := l.loadAllRulesGroupsForUser(ctx, user)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list rule groups for user %s", user)
		}

		lists[user] = list
	}

	return lists, nil
}

// ListRuleGroupsForUserAndNamespace implements rules.RuleStore. This method also loads the rules.
func (l *Client) ListRuleGroupsForUserAndNamespace(ctx context.Context, userID string, namespace string) (rulespb.RuleGroupList, error) {
	if namespace != "" {
		return l.loadAllRulesGroupsForUserAndNamespace(ctx, userID, namespace)
	}

	return l.loadAllRulesGroupsForUser(ctx, userID)
}

func (l *Client) LoadRuleGroups(_ context.Context, _ map[string]rulespb.RuleGroupList) error {
	// This Client already loads the rules in its List methods, there is nothing left to do here.
	return nil
}

// GetRuleGroup implements RuleStore
func (l *Client) GetRuleGroup(ctx context.Context, userID, namespace, group string) (*rulespb.RuleGroupDesc, error) {
	return nil, errors.New("GetRuleGroup unsupported in rule local store")
}

// SetRuleGroup implements RuleStore
func (l *Client) SetRuleGroup(ctx context.Context, userID, namespace string, group *rulespb.RuleGroupDesc) error {
	return errors.New("SetRuleGroup unsupported in rule local store")
}

// DeleteRuleGroup implements RuleStore
func (l *Client) DeleteRuleGroup(ctx context.Context, userID, namespace string, group string) error {
	return errors.New("DeleteRuleGroup unsupported in rule local store")
}

// DeleteNamespace implements RulerStore
func (l *Client) DeleteNamespace(ctx context.Context, userID, namespace string) error {
	return errors.New("DeleteNamespace unsupported in rule local store")
}

func (l *Client) loadAllRulesGroupsForUser(ctx context.Context, userID string) (rulespb.RuleGroupList, error) {
	var allLists rulespb.RuleGroupList

	root := filepath.Join(l.cfg.Directory, userID)
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read rule dir %s", root)
	}

	for _, entry := range dirEntries {
		// After resolving link, entry.Name() may be different than namespace, so keep original name.
		namespace := entry.Name()

		var isDir bool

		if entry.Type()&os.ModeSymlink != 0 {
			// os.ReadDir only returns result of LStat. Calling Stat resolves symlink.
			path := filepath.Join(root, entry.Name())
			fi, err := os.Stat(path)
			if err != nil {
				return nil, errors.Wrapf(err, "unable to stat rule file %s", path)
			}
			isDir = fi.IsDir()
		} else {
			isDir = entry.IsDir()
		}

		if isDir {
			continue
		}

		list, err := l.loadAllRulesGroupsForUserAndNamespace(ctx, userID, namespace)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list rule group for user %s and namespace %s", userID, namespace)
		}

		allLists = append(allLists, list...)
	}

	return allLists, nil
}

func (l *Client) loadAllRulesGroupsForUserAndNamespace(_ context.Context, userID string, namespace string) (rulespb.RuleGroupList, error) {
	filename := filepath.Join(l.cfg.Directory, userID, namespace)

	rulegroups, allErrors := l.loader.Load(filename)
	if len(allErrors) > 0 {
		return nil, errors.Wrapf(allErrors[0], "error parsing %s", filename)
	}

	var list rulespb.RuleGroupList

	for _, group := range rulegroups.Groups {
		desc := rulespb.ToProto(userID, namespace, group)
		list = append(list, desc)
	}

	return list, nil
}
