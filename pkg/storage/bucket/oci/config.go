package oci

import (
	"flag"
	"fmt"
	objstoreoci "github.com/thanos-io/objstore/providers/oci"
	"strings"
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
		"OCI authentication provider: default, instance-principal, raw, or oke-workload-identity.",
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
	provider := strings.ToLower(cfg.Provider)

	switch provider {
	case "default", "instance-principal", "raw", "oke-workload-identity":
		// Supported.
	case "":
		return fmt.Errorf("OCI provider must be configured")
	default:
		return fmt.Errorf("unsupported OCI provider %q", cfg.Provider)
	}
	if cfg.Provider == "" {
		return fmt.Errorf("OCI provider must be configured")
	}

	if cfg.Bucket == "" {
		return fmt.Errorf("OCI bucket must be configured")
	}

	if cfg.Provider == "oke-workload-identity" && cfg.Region == "" {
		return fmt.Errorf("OCI region must be configured when using OKE workload identity")
	}

	return nil
}
