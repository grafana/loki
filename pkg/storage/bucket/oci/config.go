package oci

import (
	"flag"

	"github.com/thanos-io/objstore/providers/oci"
)

// Config holds the config options for OCI backend
type Config struct {
	oci.Config `yaml:",inline"`
}

// RegisterFlags registers the flags for OCI storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for OCI storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Bucket, prefix+"oci.bucket", "", "OCI bucket name")
	f.StringVar(&cfg.Provider, prefix+"oci.provider", "default", "Credential provider (default, instance-principal, oke-workload-identity, or raw)")
	f.StringVar(&cfg.Region, prefix+"oci.region", "", "OCI Region name")
	f.StringVar(&cfg.Compartment, prefix+"oci.compartment-ocid", "", "Compartment OCID")
	f.IntVar(&cfg.MaxRequestRetries, prefix+"oci.max-request-retries", 3, "Max retries on request error")
}
