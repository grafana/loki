// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package httpconfig

import (
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/pkg/errors"
)

// Config is a structure that allows pointing to various HTTP endpoint, e.g ruler connecting to queriers.
type Config struct {
	HTTPClientConfig ClientConfig    `yaml:"http_config"`
	EndpointsConfig  EndpointsConfig `yaml:",inline"`
}

func DefaultConfig() Config {
	return Config{
		EndpointsConfig: EndpointsConfig{
			Scheme:          "http",
			StaticAddresses: []string{},
			FileSDConfigs:   []FileSDConfig{},
		},
	}
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig()
	type plain Config
	return unmarshal((*plain)(c))
}

// LoadConfigs loads a list of Config from YAML data.
func LoadConfigs(confYAML []byte) ([]Config, error) {
	var queryCfg []Config
	if err := yaml.UnmarshalStrict(confYAML, &queryCfg); err != nil {
		return nil, err
	}
	return queryCfg, nil
}

// BuildConfig returns a configuration from a static addresses.
func BuildConfig(addrs []string) ([]Config, error) {
	configs := make([]Config, 0, len(addrs))
	for i, addr := range addrs {
		if addr == "" {
			return nil, errors.Errorf("static address cannot be empty at index %d", i)
		}
		// If addr is missing schema, add http.
		if !strings.Contains(addr, "://") {
			addr = fmt.Sprintf("http://%s", addr)
		}
		u, err := url.Parse(addr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse addr %q", addr)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errors.Errorf("%q is not supported scheme for address", u.Scheme)
		}
		configs = append(configs, Config{
			EndpointsConfig: EndpointsConfig{
				Scheme:          u.Scheme,
				StaticAddresses: []string{u.Host},
				PathPrefix:      u.Path,
			},
		})
	}
	return configs, nil
}
