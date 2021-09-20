package instance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupManager_ListInstances_Configs(t *testing.T) {
	gm := NewGroupManager(newFakeManager())

	// Create two configs in the same group and one in another
	// group.
	configs := []string{
		`
name: configA
scrape_configs: []
remote_write: []`,
		`
name: configB
scrape_configs: []
remote_write: []`,
		`
name: configC
scrape_configs: []
remote_write:
- url: http://localhost:9090`,
	}

	for _, cfg := range configs {
		c := testUnmarshalConfig(t, cfg)
		err := gm.ApplyConfig(c)
		require.NoError(t, err)
	}

	// ListInstances should return our grouped instances
	insts := gm.ListInstances()
	require.Equal(t, 2, len(insts))

	// ...but ListConfigs should return the ungrouped configs.
	confs := gm.ListConfigs()
	require.Equal(t, 3, len(confs))
	require.Containsf(t, confs, "configA", "configA not in confs")
	require.Containsf(t, confs, "configB", "configB not in confs")
	require.Containsf(t, confs, "configC", "configC not in confs")
}

func testUnmarshalConfig(t *testing.T, cfg string) Config {
	c, err := UnmarshalConfig(strings.NewReader(cfg))
	require.NoError(t, err)
	return *c
}

func TestGroupManager_ApplyConfig(t *testing.T) {
	t.Run("combining configs", func(t *testing.T) {
		inner := newFakeManager()
		gm := NewGroupManager(inner)
		err := gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs: []
remote_write: []
`))
		require.NoError(t, err)

		err = gm.ApplyConfig(testUnmarshalConfig(t, `
name: configB
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`))
		require.NoError(t, err)

		require.Equal(t, 1, len(gm.groups))
		require.Equal(t, 2, len(gm.groupLookup))

		// Check the underlying grouped config and make sure it was updated.
		expect := testUnmarshalConfig(t, fmt.Sprintf(`
name: %s
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`, gm.groupLookup["configA"]))

		innerConfigs := inner.ListConfigs()
		require.Equal(t, 1, len(innerConfigs))
		require.Equal(t, expect, innerConfigs[gm.groupLookup["configA"]])
	})

	t.Run("updating existing config within group", func(t *testing.T) {
		inner := newFakeManager()
		gm := NewGroupManager(inner)
		err := gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs: []
remote_write: []
`))
		require.NoError(t, err)
		require.Equal(t, 1, len(gm.groups))
		require.Equal(t, 1, len(gm.groupLookup))

		err = gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`))
		require.NoError(t, err)
		require.Equal(t, 1, len(gm.groups))
		require.Equal(t, 1, len(gm.groupLookup))

		// Check the underlying grouped config and make sure it was updated.
		expect := testUnmarshalConfig(t, fmt.Sprintf(`
name: %s
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`, gm.groupLookup["configA"]))
		actual := inner.ListConfigs()[gm.groupLookup["configA"]]
		require.Equal(t, expect, actual)
	})

	t.Run("updating existing config to new group", func(t *testing.T) {
		inner := newFakeManager()
		gm := NewGroupManager(inner)
		err := gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs: []
remote_write: []
`))
		require.NoError(t, err)
		require.Equal(t, 1, len(gm.groups))
		require.Equal(t, 1, len(gm.groupLookup))
		oldGroup := gm.groupLookup["configA"]

		// Reapply the config but give it a setting change that would
		// force it into a new group. We should still have only one
		// group and only one entry in the group lookup table.
		err = gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
host_filter: true
scrape_configs: []
remote_write: []
`))
		require.NoError(t, err)
		require.Equal(t, 1, len(gm.groups))
		require.Equal(t, 1, len(gm.groupLookup))
		newGroup := gm.groupLookup["configA"]

		// Check the underlying grouped config and make sure it was updated.
		expect := testUnmarshalConfig(t, fmt.Sprintf(`
name: %s
host_filter: true
scrape_configs: []
remote_write: []
`, gm.groupLookup["configA"]))
		actual := inner.ListConfigs()[newGroup]
		require.Equal(t, expect, actual)

		// The old underlying ngroup should be gone.
		require.NotContains(t, inner.ListConfigs(), oldGroup)
		require.Equal(t, 1, len(inner.ListConfigs()))
	})
}

func TestGroupManager_ApplyConfig_RemoteWriteName(t *testing.T) {
	inner := newFakeManager()
	gm := NewGroupManager(inner)
	err := gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs: []
remote_write:
- name: rw-cfg-a
  url: http://localhost:9009/api/prom/push
`))
	require.NoError(t, err)

	require.Equal(t, 1, len(gm.groups))
	require.Equal(t, 1, len(gm.groupLookup))

	// Check the underlying grouped config and make sure the group_name
	// didn't get copied from the remote_name of A.
	innerConfigs := inner.ListConfigs()
	require.Equal(t, 1, len(innerConfigs))

	cfg := innerConfigs[gm.groupLookup["configA"]]
	require.NotEqual(t, "rw-cfg-a", cfg.RemoteWrite[0].Name)
}

func TestGroupManager_DeleteConfig(t *testing.T) {
	t.Run("partial delete", func(t *testing.T) {
		inner := newFakeManager()
		gm := NewGroupManager(inner)

		// Apply two configs in the same group and then delete one. The group
		// should still be active with the one config inside of it.
		err := gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`))
		require.NoError(t, err)

		err = gm.ApplyConfig(testUnmarshalConfig(t, `
name: configB
scrape_configs:
- job_name: test_job2
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`))
		require.NoError(t, err)

		err = gm.DeleteConfig("configA")
		require.NoError(t, err)

		expect := testUnmarshalConfig(t, fmt.Sprintf(`
name: %s
scrape_configs:
- job_name: test_job2
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []`, gm.groupLookup["configB"]))
		actual := inner.ListConfigs()[gm.groupLookup["configB"]]
		require.Equal(t, expect, actual)
		require.Equal(t, 1, len(gm.groups))
		require.Equal(t, 1, len(gm.groupLookup))
	})

	t.Run("full delete", func(t *testing.T) {
		inner := newFakeManager()
		gm := NewGroupManager(inner)

		// Apply a single config but delete the entire group.
		err := gm.ApplyConfig(testUnmarshalConfig(t, `
name: configA
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []
`))
		require.NoError(t, err)

		err = gm.DeleteConfig("configA")
		require.NoError(t, err)
		require.Equal(t, 0, len(inner.ListConfigs()))
		require.Equal(t, 0, len(inner.ListInstances()))
		require.Equal(t, 0, len(gm.groups))
		require.Equal(t, 0, len(gm.groupLookup))
	})
}

func newFakeManager() Manager {
	instances := make(map[string]ManagedInstance)
	configs := make(map[string]Config)

	return &MockManager{
		ListInstancesFunc: func() map[string]ManagedInstance {
			return instances
		},
		ListConfigsFunc: func() map[string]Config {
			return configs
		},
		ApplyConfigFunc: func(c Config) error {
			instances[c.Name] = &mockInstance{}
			configs[c.Name] = c
			return nil
		},
		DeleteConfigFunc: func(name string) error {
			delete(instances, name)
			delete(configs, name)
			return nil
		},
		StopFunc: func() {},
	}
}

func Test_hashConfig(t *testing.T) {
	t.Run("name and scrape configs are ignored", func(t *testing.T) {
		configAText := `
name: configA
scrape_configs: []
remote_write: []`

		configBText := `
name: configB
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write: []`

		hashA, hashB := getHashesFromConfigs(t, configAText, configBText)
		require.Equal(t, hashA, hashB)
	})

	t.Run("remote_writes are unordered", func(t *testing.T) {
		configAText := `
name: configA
scrape_configs: []
remote_write:
- url: http://localhost:9009/api/prom/push1
- url: http://localhost:9009/api/prom/push2`

		configBText := `
name: configB
scrape_configs: []
remote_write:
- url: http://localhost:9009/api/prom/push2
- url: http://localhost:9009/api/prom/push1`

		hashA, hashB := getHashesFromConfigs(t, configAText, configBText)
		require.Equal(t, hashA, hashB)
	})

	t.Run("remote_writes must match", func(t *testing.T) {
		configAText := `
name: configA
scrape_configs: []
remote_write:
- url: http://localhost:9009/api/prom/push1
- url: http://localhost:9009/api/prom/push2`

		configBText := `
name: configB
scrape_configs: []
remote_write:
- url: http://localhost:9009/api/prom/push1
- url: http://localhost:9009/api/prom/push1`

		hashA, hashB := getHashesFromConfigs(t, configAText, configBText)
		require.NotEqual(t, hashA, hashB)
	})

	t.Run("other fields must match", func(t *testing.T) {
		configAText := `
name: configA
host_filter: true
scrape_configs: []
remote_write: []`

		configBText := `
name: configB
host_filter: false
scrape_configs: []
remote_write: []`

		hashA, hashB := getHashesFromConfigs(t, configAText, configBText)
		require.NotEqual(t, hashA, hashB)
	})
}

func getHashesFromConfigs(t *testing.T, configAText, configBText string) (string, string) {
	configA := testUnmarshalConfig(t, configAText)
	configB := testUnmarshalConfig(t, configBText)

	hashA, err := hashConfig(configA)
	require.NoError(t, err)

	hashB, err := hashConfig(configB)
	require.NoError(t, err)

	return hashA, hashB
}

func Test_groupConfigs(t *testing.T) {
	configAText := `
name: configA
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write:
- url: http://localhost:9009/api/prom/push1
- url: http://localhost:9009/api/prom/push2`

	configBText := `
name: configB
scrape_configs:
- job_name: test_job2
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write:
- url: http://localhost:9009/api/prom/push2
- url: http://localhost:9009/api/prom/push1`

	configA := testUnmarshalConfig(t, configAText)
	configB := testUnmarshalConfig(t, configBText)

	groupName, err := hashConfig(configA)
	require.NoError(t, err)

	expectText := fmt.Sprintf(`
name: %s
scrape_configs:
- job_name: test_job
  static_configs:
    - targets: [127.0.0.1:12345]
- job_name: test_job2
  static_configs:
    - targets: [127.0.0.1:12345]
remote_write:
- url: http://localhost:9009/api/prom/push1
- url: http://localhost:9009/api/prom/push2`, groupName)

	expect, err := UnmarshalConfig(strings.NewReader(expectText))
	require.NoError(t, err)

	// Generate expected remote_write names
	for _, rwConfig := range expect.RemoteWrite {
		hash, err := getHash(rwConfig)
		require.NoError(t, err)
		rwConfig.Name = groupName[:6] + "-" + hash[:6]
	}

	group := groupedConfigs{
		"configA": configA,
		"configB": configB,
	}
	actual, err := groupConfigs(groupName, group)
	require.NoError(t, err)
	require.Equal(t, *expect, actual)

	// Consistency check: groupedConfigs is a map and we want to always have
	// groupConfigs return the same thing regardless of how the map
	// is iterated over. Run through groupConfigs a bunch of times and
	// make sure it always returns the same thing.
	for i := 0; i < 100; i++ {
		actual, err = groupConfigs(groupName, group)
		require.NoError(t, err)
		require.Equal(t, *expect, actual)
	}
}
