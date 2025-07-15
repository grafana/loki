package serverutils

import (
	"flag"

	"dario.cat/mergo"
	"github.com/grafana/dskit/server"
)

// MergeWithDefaults applies server.Config defaults to a given and different server.Config.
func MergeWithDefaults(config server.Config) (server.Config, error) {
	// Bit of a chicken and egg problem trying to register the defaults and apply overrides from the loaded config.
	// First create an empty config and set defaults.
	mergee := server.Config{}
	mergee.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	// Then apply any config values loaded as overrides to the defaults.
	if err := mergo.Merge(&mergee, config, mergo.WithOverride); err != nil {
		return server.Config{}, err
	}
	// The merge won't overwrite with a zero value but in the case of ports 0 value
	// indicates the desire for a random port so reset these to zero if the incoming config val is 0
	if config.HTTPListenPort == 0 {
		mergee.HTTPListenPort = 0
	}
	if config.GRPCListenPort == 0 {
		mergee.GRPCListenPort = 0
	}
	return mergee, nil
}
