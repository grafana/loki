package engine

import (
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
)

// Scheduler is a service that can schedule tasks to connected [Worker]
// instances.
type Scheduler struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner *scheduler.Scheduler
}

// NewScheduler creates a new Scheduler. Use [Scheduler.Service] to manage the
// lifecycle of the Scheduler.
func NewScheduler(logger log.Logger) (*Scheduler, error) {
	inner, err := scheduler.New(scheduler.Config{
		Logger: logger,
	})
	if err != nil {
		return nil, err
	}

	return &Scheduler{inner: inner}, nil
}

// Service returns the service used to manage the lifecycle of the Scheduler.
func (s *Scheduler) Service() services.Service {
	return s.inner.Service()
}
