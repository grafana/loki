package cluster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (c *Component) WithRulerRemoteWrite(name, url string) {

	// ensure remote-write is enabled
	c.WithExtraConfig(`
ruler:
  remote_write:
    enabled: true
`)

	c.WithExtraConfig(fmt.Sprintf(`
ruler:
  remote_write:
    clients:
      %s:
        url: %s/api/v1/write
        queue_config:
          # send immediately as soon as a sample is generated
          capacity: 1
          batch_send_deadline: 0s
`, name, url))
}

func (c *Component) WithTenantRules(tenantFilesMap map[string]map[string]string) error {
	sharedPath := c.ClusterSharedPath()
	rulesPath := filepath.Join(sharedPath, "rules")

	if err := os.Mkdir(rulesPath, 0750); err != nil {
		return fmt.Errorf("error creating rules path: %w", err)
	}

	for tenant, files := range tenantFilesMap {
		for filename, file := range files {
			path := filepath.Join(rulesPath, tenant)
			if err := os.Mkdir(path, 0750); err != nil {
				return fmt.Errorf("error creating tenant %s rules path: %w", tenant, err)
			}
			if err := os.WriteFile(filepath.Join(path, filename), []byte(strings.TrimSpace(file)), 0640); err != nil { // #nosec G306 -- this is fencing off the "other" permissions
				return fmt.Errorf("error creating rule file at path %s: %w", path, err)
			}
		}
	}

	return nil
}
