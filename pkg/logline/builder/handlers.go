package builder

import (
	"github.com/go-kit/log"
	"github.com/grafana/dskit/server"
)

// RegisterHandlers registers HTTP handlers for the logline-index-builder target.
func RegisterHandlers(srv *server.Server, logger log.Logger) error {
	return nil
}
