package http

import (
	"github.com/weaveworks/common/server"
)

type Config struct {
	// Server is the weaveworks server config for listening connections
	Server server.Config `yaml:"server"`
}
