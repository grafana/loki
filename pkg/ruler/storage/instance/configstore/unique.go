package configstore

import (
	"github.com/grafana/agent/pkg/metrics/instance"
)

// checkUnique validates that cfg is unique from all, ensuring that no two
// configs share a job_name.
func checkUnique(all <-chan instance.Config, cfg *instance.Config) error {
	defer func() {
		// Drain the channel, which is necessary if we're returning an error.
		for range all {
		}
	}()

	newJobNames := make(map[string]struct{}, len(cfg.ScrapeConfigs))
	for _, sc := range cfg.ScrapeConfigs {
		newJobNames[sc.JobName] = struct{}{}
	}

	for otherConfig := range all {
		// If the other config is the one we're validating, skip it.
		if otherConfig.Name == cfg.Name {
			continue
		}

		for _, otherScrape := range otherConfig.ScrapeConfigs {
			if _, exist := newJobNames[otherScrape.JobName]; exist {
				return NotUniqueError{ScrapeJob: otherScrape.JobName}
			}
		}
	}

	return nil
}
