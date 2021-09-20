// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"bytes"
	"io"

	config_util "github.com/prometheus/common/config"
	"gopkg.in/yaml.v2"
)

// UnmarshalConfig unmarshals an instance config from a reader based on a
// provided content type.
func UnmarshalConfig(r io.Reader) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(r)
	dec.SetStrict(true)
	err := dec.Decode(&cfg)
	return &cfg, err
}

// MarshalConfig marshals an instance config based on a provided content type.
func MarshalConfig(c *Config, scrubSecrets bool) ([]byte, error) {
	var buf bytes.Buffer
	err := MarshalConfigToWriter(c, &buf, scrubSecrets)
	return buf.Bytes(), err
}

// MarshalConfigToWriter marshals a config to an io.Writer.
func MarshalConfigToWriter(c *Config, w io.Writer, scrubSecrets bool) error {
	enc := yaml.NewEncoder(w)

	// If we're not sanitizing the marshaled config, we want to add in an
	// encoding hook to ignore how Secrets marshal (i.e., scrubbing the value
	// and replacing it with <secret>).
	if !scrubSecrets {
		enc.SetHook(func(in interface{}) (ok bool, out interface{}, err error) {
			switch v := in.(type) {
			case config_util.Secret:
				return true, string(v), nil
			default:
				return false, nil, nil
			}
		})
	}

	type plain Config
	return enc.Encode((*plain)(c))
}
