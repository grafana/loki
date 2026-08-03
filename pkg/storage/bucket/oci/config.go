package oci

import (
	"flag"
	"fmt"
	"strings"

	objstoreoci "github.com/thanos-io/objstore/providers/oci"
)

type Config struct {
	Provider             string                 `yaml:"provider"`
	Bucket               string                 `yaml:"bucket"`
	CompartmentOCID      string                 `yaml:"compartment_ocid"`
	TenancyOCID          string                 `yaml:"tenancy_ocid"`
	UserOCID             string                 `yaml:"user_ocid"`
	Region               string                 `yaml:"region"`
	PartSize             int64                  `yaml:"part_size"`
	MaxRequestRetries    int                    `yaml:"max_request_retries"`
	RequestRetryInterval int                    `yaml:"request_retry_interval"`
	HTTPConfig           objstoreoci.HTTPConfig `yaml:"http_config"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(
		&cfg.Provider,
		prefix+"oci.provider",
		"",
		"OCI authentication provider: default, instance-principal, or oke-workload-identity.",
	)

	f.StringVar(
		&cfg.Bucket,
		prefix+"oci.bucket",
		"",
		"OCI Object Storage bucket name.",
	)

	f.StringVar(
		&cfg.CompartmentOCID,
		prefix+"oci.compartment-ocid",
		"",
		"OCI compartment OCID.",
	)

	f.StringVar(
		&cfg.Region,
		prefix+"oci.region",
		"",
		"OCI region.",
	)

	f.Int64Var(
		&cfg.PartSize,
		prefix+"oci.part-size",
		0,
		"OCI multipart upload part size.",
	)

	f.IntVar(
		&cfg.MaxRequestRetries,
		prefix+"oci.max-request-retries",
		0,
		"Maximum number of OCI request retries.",
	)

	f.IntVar(
		&cfg.RequestRetryInterval,
		prefix+"oci.request-retry-interval",
		0,
		"OCI request retry interval.",
	)
}
func (cfg *Config) Validate() error {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	cfg.Provider = provider

	switch provider {
	case "default", "instance-principal", "oke-workload-identity":
		// Supported.
	case "":
		return fmt.Errorf("OCI provider must be configured")
	default:
		return fmt.Errorf("unsupported OCI provider %q", cfg.Provider)
	}

	if cfg.Bucket == "" {
		return fmt.Errorf("OCI bucket must be configured")
	}

	if provider == "oke-workload-identity" && cfg.Region == "" {
		return fmt.Errorf("OCI region must be configured when using OKE workload identity")
	}

	return nil
}
func (cfg Config) IsConfigured() bool {
	return cfg.Provider != "" ||
		cfg.Bucket != "" ||
		cfg.CompartmentOCID != "" ||
		cfg.TenancyOCID != "" ||
		cfg.UserOCID != "" ||
		cfg.Region != "" ||
		cfg.PartSize != 0 ||
		cfg.MaxRequestRetries != 0 ||
		cfg.RequestRetryInterval != 0 ||
		httpConfigIsConfigured(cfg.HTTPConfig)
}

func httpConfigIsConfigured(cfg objstoreoci.HTTPConfig) bool {
	return cfg.IdleConnTimeout != 0 ||
		cfg.ResponseHeaderTimeout != 0 ||
		cfg.InsecureSkipVerify ||
		cfg.TLSHandshakeTimeout != 0 ||
		cfg.ExpectContinueTimeout != 0 ||
		cfg.MaxIdleConns != 0 ||
		cfg.MaxIdleConnsPerHost != 0 ||
		cfg.MaxConnsPerHost != 0 ||
		cfg.DisableCompression ||
		cfg.ClientTimeout != 0 ||
		cfg.Transport != nil
}
