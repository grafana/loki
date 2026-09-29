package builder

import (
	"github.com/go-kit/log"
	"github.com/grafana/dskit/server"
)

// RegisterHandlers registers HTTP handlers for the logline-index-builder target.
func RegisterHandlers(_ *server.Server, _ log.Logger) error {
	return nil
}
