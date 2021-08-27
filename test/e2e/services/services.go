package services

import (
	"os"

	"github.com/cortexproject/cortex/integration/e2e"
)

// DefaultLoki returns the default image name or the override in the LOKI_IMAGE environment variable
func DefaultLoki() string {
	if os.Getenv("LOKI_IMAGE") != "" {
		return os.Getenv("LOKI_IMAGE")
	}

	return "grafana/loki"
}

// NewLoki creates and returns a new Loki service
func NewLoki(name string, flags map[string]string) *e2e.HTTPService {
	svc := e2e.NewHTTPService(
		name,
		DefaultLoki(),
		e2e.NewCommandWithoutEntrypoint("loki", e2e.BuildArgs(e2e.MergeFlags(map[string]string{
			"-target": "all",
		}, flags))...),
		e2e.NewHTTPReadinessProbe(3100, "/ready", 200, 299),
		3100,
		9095,
		7946,
	)

	return svc
}
