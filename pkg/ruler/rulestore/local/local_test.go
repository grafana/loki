package local

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
)

func TestClient_LoadAllRuleGroups(t *testing.T) {
	user1 := "user"
	user2 := "second-user"

	namespace1 := "ns"
	namespace2 := "z-another" // This test relies on the fact that ioutil.ReadDir() returns files sorted by name.

	dir := t.TempDir()

	ruleGroups := rulefmt.RuleGroups{
		Groups: []rulefmt.RuleGroup{
			{
				Name:     "rule",
				Interval: model.Duration(100 * time.Second),
				Rules: []rulefmt.Rule{
					{
						Record: "test_rule",
						Expr:   "up",
					},
				},
			},
		},
	}

	b, err := yaml.Marshal(ruleGroups)
	require.NoError(t, err)

	err = os.MkdirAll(path.Join(dir, user1), 0777)
	require.NoError(t, err)

	// Link second user to first.
	err = os.Symlink(user1, path.Join(dir, user2))
	require.NoError(t, err)

	err = os.WriteFile(path.Join(dir, user1, namespace1), b, 0777)
	require.NoError(t, err)

	const ignoredDir = "ignored-dir"
	err = os.Mkdir(path.Join(dir, user1, ignoredDir), os.ModeDir|0644)
	require.NoError(t, err)

	err = os.Symlink(ignoredDir, path.Join(dir, user1, "link-to-dir"))
	require.NoError(t, err)

	// Link second namespace to first.
	err = os.Symlink(namespace1, path.Join(dir, user1, namespace2))
	require.NoError(t, err)

	// Dotfile with content the rulefmt loader rejects (valid YAML, wrong
	// shape for a rule group). Before the fix, this file would be handed
	// to the loader and fail parsing, aborting the entire listing. After
	// the fix, its name is enough to skip it.
	err = os.WriteFile(path.Join(dir, user1, ".alerts.yml.swp"), []byte("groups: not-a-list"), 0777)
	require.NoError(t, err)

	// Broken dot-symlink pointing at a non-existent target. If the
	// dotfile check runs AFTER os.Stat, the stat call will fail on the
	// dangling target and the whole listing errors out. If the check
	// runs BEFORE stat (as intended), this entry is silently skipped.
	err = os.Symlink("/does/not/exist", path.Join(dir, user1, ".linktonowhere"))
	require.NoError(t, err)

	// Kubernetes ConfigMap atomic-writer style internal directory at the
	// rules root. Before the fix, ListAllUsers would treat this as a
	// tenant ID. After the fix, ListAllUsers skips it.
	err = os.Mkdir(path.Join(dir, "..2022_03_15_14_00_00.000000000"), 0755)
	require.NoError(t, err)

	client, err := NewLocalRulesClient(Config{
		Directory: dir,
	}, testFileLoader{})
	require.NoError(t, err)

	ctx := context.Background()
	userMap, err := client.ListAllRuleGroups(ctx) // Client loads rules in its List method.
	require.NoError(t, err)

	for _, u := range []string{user1, user2} {
		actual, found := userMap[u]
		require.True(t, found)

		require.Equal(t, 2, len(actual))
		// We rely on the fact that files are parsed in alphabetical order, and our namespace1 < namespace2.
		require.Equal(t, rulespb.ToProto(u, namespace1, ruleGroups.Groups[0]), actual[0])
		require.Equal(t, rulespb.ToProto(u, namespace2, ruleGroups.Groups[0]), actual[1])
	}

	// The K8s-style dotdir at the rules root must not appear as a tenant.
	// The map must contain exactly user1 and user2.
	require.Len(t, userMap, 2)
	_, dotdirLeaked := userMap["..2022_03_15_14_00_00.000000000"]
	require.False(t, dotdirLeaked, "dot-directory at rules root was not filtered by ListAllUsers")
}

type testFileLoader struct{}

func (testFileLoader) Load(identifier string, ignoreUnknownFields bool, nameValidationScheme model.ValidationScheme) (*rulefmt.RuleGroups, []error) {
	parseLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	return rulefmt.ParseFile(identifier, ignoreUnknownFields, nameValidationScheme, parser.NewParser(parser.Options{}), parseLog)
}

func (testFileLoader) Parse(query string) (parser.Expr, error) {
	return parser.NewParser(parser.Options{}).ParseExpr(query)
}
