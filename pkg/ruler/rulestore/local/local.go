package local

import (
	"context"
	"flag"
	"io/fs"
	"os"
	"path"
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
//
// When the Loki Operator stores rules in
//
//	multiple Config Map shards, they are located at:
//
//	cfg.Directory / configMapName / userID / namespace
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

func contains(strArr []string, str string) bool {
	for _, v := range strArr {
		if v == str {
			return true
		}
	}
	return false
}

func (l *Client) ListAllUsers(ctx context.Context) ([]string, error) {

	var result []string
	var isDir bool

	err := filepath.WalkDir(l.cfg.Directory, func(p string, d fs.DirEntry, err error) error {
		if d.Type()&fs.FileMode(os.ModeSymlink) != 0 {
			fi, err := os.Stat(p)
			if err != nil {
				return errors.Wrapf(err, "unable to stat file %s", p)
			}
			isDir = fi.IsDir()
		} else {
			isDir = d.IsDir()
		}

		if !isDir {
			userDir := path.Base(path.Dir(p))
			if !contains(result, userDir) {
				result = append(result, userDir)
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to walk the file tree located at %s", l.cfg.Directory)
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
	var (
		allLists        rulespb.RuleGroupList
		rootDirectories []string
	)

	err := filepath.WalkDir(l.cfg.Directory, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() && d.Name() == userID {
			rootDirectories = append(rootDirectories, path)
		}
		return nil
	})
	if err != nil {
		println(err)
	}

	for _, rulesDir := range rootDirectories {
		dirEntries, err := os.ReadDir(rulesDir)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read rule dir %s", rulesDir)
		}

		for _, entry := range dirEntries {
			// After resolving link, entry.Name() may be different than namespace, so keep original name.
			namespace := entry.Name()

			var isDir bool

			if entry.Type()&os.ModeSymlink != 0 {
				// os.ReadDir only returns result of LStat. Calling Stat resolves symlink.
				path := filepath.Join(rulesDir, entry.Name())
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

			list, err := l.loadAllRulesGroupsForUserAndNamespace(ctx, rulesDir, namespace)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to list rule group for user %s and namespace %s", userID, namespace)
			}
			allLists = append(allLists, list...)
		}
	}
	return allLists, nil
}

func (l *Client) loadAllRulesGroupsForUserAndNamespace(_ context.Context, rootDir string, namespace string) (rulespb.RuleGroupList, error) {
	filename := filepath.Join(rootDir, namespace)

	rulegroups, allErrors := l.loader.Load(filename)
	if len(allErrors) > 0 {
		return nil, errors.Wrapf(allErrors[0], "error parsing %s", filename)
	}

	var list rulespb.RuleGroupList

	for _, group := range rulegroups.Groups {
		desc := rulespb.ToProto(path.Base(rootDir), namespace, group)
		list = append(list, desc)
	}

	return list, nil
}
