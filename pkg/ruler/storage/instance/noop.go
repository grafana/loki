// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"context"

	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
)

// NoOpInstance implements the Instance interface in pkg/prom
// but does not do anything. Useful for tests.
type NoOpInstance struct{}

// Run implements Instance.
func (NoOpInstance) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Update implements Instance.
func (NoOpInstance) Update(_ Config) error {
	return nil
}

// TargetsActive implements Instance.
func (NoOpInstance) TargetsActive() map[string][]*scrape.Target {
	return nil
}

// StorageDirectory implements Instance.
func (NoOpInstance) StorageDirectory() string {
	return ""
}

// Appender implements Instance
func (NoOpInstance) Appender(_ context.Context) storage.Appender {
	return nil
}
