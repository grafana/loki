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
