// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package cluster

import (
	"fmt"

	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/aws"
	"github.com/prometheus/prometheus/discovery/azure"
	"github.com/prometheus/prometheus/discovery/consul"
	"github.com/prometheus/prometheus/discovery/digitalocean"
	"github.com/prometheus/prometheus/discovery/dns"
	"github.com/prometheus/prometheus/discovery/eureka"
	"github.com/prometheus/prometheus/discovery/file"
	"github.com/prometheus/prometheus/discovery/gce"
	"github.com/prometheus/prometheus/discovery/hetzner"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/marathon"
	"github.com/prometheus/prometheus/discovery/moby"
	"github.com/prometheus/prometheus/discovery/openstack"
	"github.com/prometheus/prometheus/discovery/scaleway"
	"github.com/prometheus/prometheus/discovery/triton"
	"github.com/prometheus/prometheus/discovery/zookeeper"
)

func validateNofiles(c *instance.Config) error {
	for i, rw := range c.RemoteWrite {
		if err := validateHTTPNoFiles(&rw.HTTPClientConfig); err != nil {
			return fmt.Errorf("failed to validate remote_write at index %d: %w", i, err)
		}
	}

	for i, sc := range c.ScrapeConfigs {
		if err := validateHTTPNoFiles(&sc.HTTPClientConfig); err != nil {
			return fmt.Errorf("failed to validate scrape_config at index %d: %w", i, err)
		}

		for j, disc := range sc.ServiceDiscoveryConfigs {
			if err := validateDiscoveryNoFiles(disc); err != nil {
				return fmt.Errorf("failed to validate service discovery at index %d within scrape_config at index %d: %w", j, i, err)
			}
		}
	}

	return nil
}

func validateHTTPNoFiles(cfg *config.HTTPClientConfig) error {
	checks := []struct {
		name  string
		check func() bool
	}{
		{"bearer_token_file", func() bool { return cfg.BearerTokenFile != "" }},
		{"password_file", func() bool { return cfg.BasicAuth != nil && cfg.BasicAuth.PasswordFile != "" }},
		{"credentials_file", func() bool { return cfg.Authorization != nil && cfg.Authorization.CredentialsFile != "" }},
		{"ca_file", func() bool { return cfg.TLSConfig.CAFile != "" }},
		{"cert_file", func() bool { return cfg.TLSConfig.CertFile != "" }},
		{"key_file", func() bool { return cfg.TLSConfig.KeyFile != "" }},
	}
	for _, check := range checks {
		if check.check() {
			return fmt.Errorf("%s must be empty unless dangerous_allow_reading_files is set", check.name)
		}
	}
	return nil
}

func validateDiscoveryNoFiles(disc discovery.Config) error {
	switch d := disc.(type) {
	case discovery.StaticConfig:
		// no-op
	case *azure.SDConfig:
		// no-op
	case *consul.SDConfig:
		if err := validateHTTPNoFiles(&config.HTTPClientConfig{TLSConfig: d.TLSConfig}); err != nil {
			return err
		}
	case *digitalocean.SDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
	case *dns.SDConfig:
		// no-op
	case *moby.DockerSwarmSDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
	case *aws.EC2SDConfig:
		// no-op
	case *eureka.SDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
	case *file.SDConfig:
		// no-op
	case *gce.SDConfig:
		// no-op
	case *hetzner.SDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
	case *kubernetes.SDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
	case *marathon.SDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
		if d.AuthTokenFile != "" {
			return fmt.Errorf("auth_token_file must be empty unless dangerous_allow_reading_files is set")
		}
	case *openstack.SDConfig:
		if err := validateHTTPNoFiles(&config.HTTPClientConfig{TLSConfig: d.TLSConfig}); err != nil {
			return err
		}
	case *scaleway.SDConfig:
		if err := validateHTTPNoFiles(&d.HTTPClientConfig); err != nil {
			return err
		}
	case *triton.SDConfig:
		if err := validateHTTPNoFiles(&config.HTTPClientConfig{TLSConfig: d.TLSConfig}); err != nil {
			return err
		}
	case *zookeeper.NerveSDConfig:
		// no-op
	case *zookeeper.ServersetSDConfig:
		// no-op
	default:
		return fmt.Errorf("unknown service discovery %s; rejecting config for safety. set dangerous_allow_reading_files to ignore", d.Name())
	}

	return nil
}
