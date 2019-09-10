package cfg

import (
	"github.com/kelseyhightower/envconfig"
)

// Env returns a Source that takes configuration from environment variables.
func Env(prefix string) Source {
	return func(dst interface{}) error {
		return envconfig.Process(prefix, dst)
	}
}
