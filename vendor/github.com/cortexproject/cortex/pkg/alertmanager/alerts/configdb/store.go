package configdb

import (
	"context"

	"github.com/cortexproject/cortex/pkg/configs/userconfig"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
	"github.com/cortexproject/cortex/pkg/configs/client"
)

// Store is a concrete implementation of RuleStore that sources rules from the config service
type Store struct {
	configClient client.Client
	since        userconfig.ID
	alertConfigs map[string]alerts.AlertConfigDesc
}

// NewStore constructs a Store
func NewStore(c client.Client) *Store {
	return &Store{
		configClient: c,
		since:        0,
		alertConfigs: make(map[string]alerts.AlertConfigDesc),
	}
}

// ListAlertConfigs implements RuleStore
func (c *Store) ListAlertConfigs(ctx context.Context) (map[string]alerts.AlertConfigDesc, error) {

	configs, err := c.configClient.GetAlerts(ctx, c.since)

	if err != nil {
		return nil, err
	}

	for user, cfg := range configs.Configs {
		if cfg.IsDeleted() {
			delete(c.alertConfigs, user)
			continue
		}

		var templates []*alerts.TemplateDesc
		for fn, template := range cfg.Config.TemplateFiles {
			templates = append(templates, &alerts.TemplateDesc{
				Filename: fn,
				Body:     template,
			})
		}

		c.alertConfigs[user] = alerts.AlertConfigDesc{
			User:      user,
			RawConfig: cfg.Config.AlertmanagerConfig,
			Templates: templates,
		}
	}

	c.since = configs.GetLatestConfigID()

	return c.alertConfigs, nil
}
