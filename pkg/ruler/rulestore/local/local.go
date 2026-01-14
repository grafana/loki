package local

import (
	"context"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	promRules "github.com/prometheus/prometheus/rules"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
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
	root   *os.Root
}

func NewLocalRulesClient(cfg Config, loader promRules.GroupLoader) (*Client, error) {
	if cfg.Directory == "" {
		return nil, errors.New("directory required for local rules config")
	}

	// Open the root directory to ensure all file operations are confined within it.
	// This prevents path traversal attacks.
	root, err := os.OpenRoot(cfg.Directory)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open root directory %s", cfg.Directory)
	}

	return &Client{
		cfg:    cfg,
		loader: loader,
		root:   root,
	}, nil
}

// Close releases the root directory handle.
func (l *Client) Close() error {
	if l.root != nil {
		return l.root.Close()
	}
	return nil
}

func (l *Client) ListAllUsers(_ context.Context) ([]string, error) {
	// Use root.Open to safely open the directory within the root.
	// This prevents path traversal attacks.
	dir, err := l.root.Open(".")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open dir %s", l.cfg.Directory)
	}
	defer dir.Close()

	dirEntries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read dir %s", l.cfg.Directory)
	}

	// Sort entries by name to ensure consistent ordering (matching os.ReadDir behavior)
	slices.SortFunc(dirEntries, func(a, b fs.DirEntry) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})

	var result []string
	for _, entry := range dirEntries {
		// After resolving link, entry.Name() may be different than user, so keep original name.
		user := entry.Name()

		var isDir bool

		if entry.Type()&os.ModeSymlink != 0 {
			// ReadDir only returns result of LStat. Use root.Stat to resolve symlink safely.
			fi, err := l.root.Stat(entry.Name())
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
func (l *Client) GetRuleGroup(_ context.Context, _, _, _ string) (*rulespb.RuleGroupDesc, error) {
	return nil, errors.New("GetRuleGroup unsupported in rule local store")
}

// SetRuleGroup implements RuleStore
func (l *Client) SetRuleGroup(_ context.Context, _, _ string, _ *rulespb.RuleGroupDesc) error {
	return errors.New("SetRuleGroup unsupported in rule local store")
}

// DeleteRuleGroup implements RuleStore
func (l *Client) DeleteRuleGroup(_ context.Context, _, _ string, _ string) error {
	return errors.New("DeleteRuleGroup unsupported in rule local store")
}

// DeleteNamespace implements RulerStore
func (l *Client) DeleteNamespace(_ context.Context, _, _ string) error {
	return errors.New("DeleteNamespace unsupported in rule local store")
}

func (l *Client) loadAllRulesGroupsForUser(ctx context.Context, userID string) (rulespb.RuleGroupList, error) {
	var allLists rulespb.RuleGroupList

	// Use root.Open to safely open the user's directory within the root.
	// This prevents path traversal attacks via userID.
	dir, err := l.root.Open(userID)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open rule dir for user %s", userID)
	}
	defer dir.Close()

	dirEntries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read rule dir for user %s", userID)
	}

	// Sort entries by name to ensure consistent ordering (matching os.ReadDir behavior)
	slices.SortFunc(dirEntries, func(a, b fs.DirEntry) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})

	for _, entry := range dirEntries {
		// After resolving link, entry.Name() may be different than namespace, so keep original name.
		namespace := entry.Name()

		var isDir bool

		if entry.Type()&os.ModeSymlink != 0 {
			// ReadDir only returns result of LStat. Use root.Stat to resolve symlink safely.
			path := userID + "/" + entry.Name()
			fi, err := l.root.Stat(path)
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
	// Use os.OpenInRoot to safely open and read the file within the root directory.
	// This guarantees the path cannot escape the rules directory via path traversal.
	path := userID + "/" + namespace
	file, err := os.OpenInRoot(l.cfg.Directory, path)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open rule file %s", path)
	}
	defer file.Close()

	// now that we have checked the file, we can safely Load its rules.
	filename := filepath.Join(l.cfg.Directory, userID, namespace)
	rulegroups, allErrors := l.loader.Load(filename, true, model.UTF8Validation)
	if len(allErrors) > 0 {
		return nil, errors.Wrapf(allErrors[0], "error parsing %s", path)
	}

	var list rulespb.RuleGroupList

	for _, group := range rulegroups.Groups {
		desc := rulespb.ToProto(userID, namespace, group)
		list = append(list, desc)
	}

	return list, nil
}
