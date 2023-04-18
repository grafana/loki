package http

import (
	"github.com/weaveworks/common/server"
)

// Config wraps the server.Config object.
type Config struct {
	// Server is the weaveworks server config for listening connections
	Server server.Config `yaml:"server"`
}
