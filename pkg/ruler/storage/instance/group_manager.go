// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/prometheus/prometheus/config"
)

// A GroupManager wraps around another Manager and groups all incoming Configs
// into a smaller set of configs, causing less managed instances to be spawned.
//
// Configs are grouped by all settings for a Config *except* scrape configs.
// Any difference found in any flag will cause a Config to be placed in another
// group. One exception to this rule is that remote_writes are compared
// unordered, but the sets of remote_writes should otherwise be identical.
//
// GroupManagers drastically improve the performance of the Agent when a
// significant number of instances are spawned, as the overhead of each
// instance having its own service discovery, WAL, and remote_write can be
// significant.
//
// The config names of instances within the group will be represented by
// that group's hash of settings.
type GroupManager struct {
	inner Manager

	mtx sync.Mutex

	// groups is a map of group name to the grouped configs.
	groups map[string]groupedConfigs

	// groupLookup is a map of config name to group name.
	groupLookup map[string]string
}

// groupedConfigs holds a set of grouped configs, keyed by the config name.
// They are stored in a map rather than a slice to make overriding an existing
// config within the group less error prone.
type groupedConfigs map[string]Config

// Copy returns a shallow copy of the groupedConfigs.
func (g groupedConfigs) Copy() groupedConfigs {
	res := make(groupedConfigs, len(g))
	for k, v := range g {
		res[k] = v
	}
	return res
}

// NewGroupManager creates a new GroupManager for combining instances of the
// same "group."
func NewGroupManager(inner Manager) *GroupManager {
	return &GroupManager{
		inner:       inner,
		groups:      make(map[string]groupedConfigs),
		groupLookup: make(map[string]string),
	}
}

// GetInstance gets the underlying grouped instance for a given name.
func (m *GroupManager) GetInstance(name string) (ManagedInstance, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	group, ok := m.groupLookup[name]
	if !ok {
		return nil, fmt.Errorf("instance %s does not exist", name)
	}

	inst, err := m.inner.GetInstance(group)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance for %s: %w", name, err)
	}
	return inst, nil
}

// ListInstances returns all currently grouped managed instances. The key
// will be the group's hash of shared settings.
func (m *GroupManager) ListInstances() map[string]ManagedInstance {
	return m.inner.ListInstances()
}

// ListConfigs returns the UNGROUPED instance configs with their original
// settings. To see the grouped instances, call ListInstances instead.
func (m *GroupManager) ListConfigs() map[string]Config {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	cfgs := make(map[string]Config)
	for _, groupedConfigs := range m.groups {
		for _, cfg := range groupedConfigs {
			cfgs[cfg.Name] = cfg
		}
	}
	return cfgs
}

// ApplyConfig will determine the group of the Config before applying it to
// the group. If no group exists, one will be created. If a group already
// exists, the group will have its settings merged with the Config and
// will be updated.
func (m *GroupManager) ApplyConfig(c Config) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.applyConfig(c)
}

func (m *GroupManager) applyConfig(c Config) (err error) {
	groupName, err := hashConfig(c)
	if err != nil {
		return fmt.Errorf("failed to get group name for config %s: %w", c.Name, err)
	}

	grouped := m.groups[groupName]
	if grouped == nil {
		grouped = make(groupedConfigs)
	} else {
		grouped = grouped.Copy()
	}

	// Add the config to the group. If the config already exists within this
	// group, it'll be overwritten.
	grouped[c.Name] = c
	mergedConfig, err := groupConfigs(groupName, grouped)
	if err != nil {
		err = fmt.Errorf("failed to group configs for %s: %w", c.Name, err)
		return
	}

	// If this config already exists in another group, we have to delete it.
	// If we can't delete it from the old group, we also can't apply it.
	if oldGroup, ok := m.groupLookup[c.Name]; ok && oldGroup != groupName {
		// There's a few cases here where if something fails, it's safer to crash
		// out and restart the Agent from scratch than it would be to continue as
		// normal. The panics here are for truly exceptional cases, otherwise if
		// something is recoverable, we'll return an error like normal.

		// If we can't find the old config, something got messed up when applying
		// the config. But it also means that we're not going to be able to restore
		// the config if something fails. Preemptively we should panic, since the
		// internal state has gotten messed up and can't be fixed.
		oldConfig, ok := m.groups[oldGroup][c.Name]
		if !ok {
			panic("failed to properly move config to new group. THIS IS A BUG!")
		}

		err = m.deleteConfig(c.Name)
		if err != nil {
			err = fmt.Errorf("cannot apply config %s because deleting it from the old group failed: %w", c.Name, err)
			return
		}

		// Now that the config is deleted, we need to restore it in case applying
		// the new one happens to fail.
		defer func() {
			if err == nil {
				return
			}

			// If restoring a config fails, we've left the Agent in a really bad
			// state: the new config can't be applied and the old config can't be
			// brought back. Just crash and let the Agent start fresh.
			//
			// Restoring the config _shouldn't_ fail here since applies only fail
			// if the config is invalid. Since the config was running before, it
			// should already be valid. If it does happen to fail, though, the
			// internal state is left corrupted since we've completely lost a
			// config.
			restoreError := m.applyConfig(oldConfig)
			if restoreError != nil {
				panic(fmt.Sprintf("failed to properly restore config. THIS IS A BUG! error: %s", restoreError))
			}
		}()
	}

	err = m.inner.ApplyConfig(mergedConfig)
	if err != nil {
		err = fmt.Errorf("failed to apply grouped configs for config %s: %w", c.Name, err)
		return
	}

	// If the inner apply succeeded, we can update our group and the lookup.
	m.groups[groupName] = grouped
	m.groupLookup[c.Name] = groupName
	return
}

// DeleteConfig will remove a Config from its associated group. If there are
// no more Configs within that group after this Config is deleted, the managed
// instance will be stopped. Otherwise, the managed instance will be updated
// with the new grouped Config that doesn't include the removed one.
func (m *GroupManager) DeleteConfig(name string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.deleteConfig(name)
}

func (m *GroupManager) deleteConfig(name string) error {
	groupName, ok := m.groupLookup[name]
	if !ok {
		return fmt.Errorf("config does not exist")
	}

	// Grab a copy of the stored group and delete our entry. We can
	// persist it after we successfully remove the config.
	group := m.groups[groupName].Copy()
	delete(group, name)

	if len(group) == 0 {
		// We deleted the last remaining config in that group; we can delete it in
		// its entirety now.
		if err := m.inner.DeleteConfig(groupName); err != nil {
			return fmt.Errorf("failed to delete empty group %s after removing config %s: %w", groupName, name, err)
		}
	} else {
		// We deleted the config but there's still more in the group; apply the new
		// group that holds the remainder of the configs (minus the one we just
		// deleted).
		mergedConfig, err := groupConfigs(groupName, group)
		if err != nil {
			return fmt.Errorf("failed to regroup configs without %s: %w", name, err)
		}

		err = m.inner.ApplyConfig(mergedConfig)
		if err != nil {
			return fmt.Errorf("failed to apply new group without %s: %w", name, err)
		}
	}

	// Update the stored group and remove the entry from the lookup table.
	if len(group) == 0 {
		delete(m.groups, groupName)
	} else {
		m.groups[groupName] = group
	}

	delete(m.groupLookup, name)
	return nil
}

// Stop stops the Manager and all of its managed instances.
func (m *GroupManager) Stop() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.inner.Stop()
	m.groupLookup = make(map[string]string)
	m.groups = make(map[string]groupedConfigs)
}

// hashConfig determines the hash of a Config used for grouping. It ignores
// the name and scrape_configs and also orders remote_writes by name prior to
// hashing.
func hashConfig(c Config) (string, error) {
	// We need a deep copy since we're going to mutate the remote_write
	// pointers.
	groupable, err := c.Clone()
	if err != nil {
		return "", err
	}

	// Ignore name and scrape configs when hashing
	groupable.Name = ""
	groupable.ScrapeConfigs = nil

	// Assign names to remote_write configs if they're not present already.
	// This is also done in AssignDefaults but is duplicated here for the sake
	// of simplifying responsibility of GroupManager.
	for _, cfg := range groupable.RemoteWrite {
		if cfg != nil {
			// We don't care if the names are different, just that the other settings
			// are the same. Blank out the name here before hashing the remote
			// write config.
			cfg.Name = ""

			hash, err := getHash(cfg)
			if err != nil {
				return "", err
			}
			cfg.Name = hash[:6]
		}
	}

	// Now sort remote_writes by name and nil-ness.
	sort.Slice(groupable.RemoteWrite, func(i, j int) bool {
		switch {
		case groupable.RemoteWrite[i] == nil:
			return true
		case groupable.RemoteWrite[j] == nil:
			return false
		default:
			return groupable.RemoteWrite[i].Name < groupable.RemoteWrite[j].Name
		}
	})

	bb, err := MarshalConfig(&groupable, false)
	if err != nil {
		return "", err
	}
	hash := md5.Sum(bb)
	return hex.EncodeToString(hash[:]), nil
}

// groupConfig creates a grouped Config where all fields are copied from
// the first config except for scrape_configs, which are appended together.
func groupConfigs(groupName string, grouped groupedConfigs) (Config, error) {
	if len(grouped) == 0 {
		return Config{}, fmt.Errorf("no configs")
	}

	// Move the map into a slice and sort it by name so this function
	// consistently does the same thing.
	cfgs := make([]Config, 0, len(grouped))
	for _, cfg := range grouped {
		cfgs = append(cfgs, cfg)
	}
	sort.Slice(cfgs, func(i, j int) bool { return cfgs[i].Name < cfgs[j].Name })

	combined, err := cfgs[0].Clone()
	if err != nil {
		return Config{}, err
	}
	combined.Name = groupName
	combined.ScrapeConfigs = []*config.ScrapeConfig{}

	// Assign all remote_write configs in the group a consistent set of remote_names.
	// If the grouped configs are coming from the scraping service, defaults will have
	// been applied and the remote names will be prefixed with the old instance config name.
	for _, rwc := range combined.RemoteWrite {
		// Blank out the existing name before getting the hash so it is doesn't take into
		// account any existing name.
		rwc.Name = ""

		hash, err := getHash(rwc)
		if err != nil {
			return Config{}, err
		}

		rwc.Name = groupName[:6] + "-" + hash[:6]
	}

	// Combine all the scrape configs. It's possible that two different ungrouped
	// configs had a matching job name, but this will be detected and rejected
	// (as it should be) when the underlying Manager eventually validates the
	// combined config.
	//
	// TODO(rfratto): should we prepend job names with the name of the original
	// config? (e.g., job_name = "config_name/job_name").
	for _, cfg := range cfgs {
		combined.ScrapeConfigs = append(combined.ScrapeConfigs, cfg.ScrapeConfigs...)
	}

	return combined, nil
}
