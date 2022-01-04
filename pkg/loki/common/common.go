package common

import (
	"flag"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/pkg/storage/chunk/aws"
	"github.com/grafana/loki/pkg/storage/chunk/azure"
	"github.com/grafana/loki/pkg/storage/chunk/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/openstack"
	"github.com/grafana/loki/pkg/util"
)

// Config holds common config that can be shared between multiple other config sections.
//
// Values defined under this common configuration are supersede if a more specific value is defined.
type Config struct {
	PathPrefix        string          `yaml:"path_prefix"`
	Storage           Storage         `yaml:"storage"`
	PersistTokens     bool            `yaml:"persist_tokens"`
	ReplicationFactor int             `yaml:"replication_factor"`
	Ring              util.RingConfig `yaml:"ring"`

	// InstanceInterfaceNames represents a common list of net interfaces used to look for host addresses.
	//
	// Internally, addresses will be resolved in the order that this is configured.
	// By default, the list of used interfaces are, in order: "eth0", "en0", and your loopback net interface (probably "lo").
	InstanceInterfaceNames []string `yaml:"instance_interface_names"`

	// InstanceAddr represents a common ip used by instances to advertise their address.
	//
	// For instance, the different Loki rings will use this
	InstanceAddr string `yaml:"instance_addr"`
}

func (c *Config) RegisterFlags(_ *flag.FlagSet) {
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)
	throwaway.IntVar(&c.ReplicationFactor, "common.replication-factor", 3, "How many ingesters incoming data should be replicated to.")
	c.Storage.RegisterFlagsWithPrefix("common.storage", throwaway)
	c.Ring.RegisterFlagsWithPrefix("", "collectors/", throwaway)

	// instance related flags.
	c.InstanceInterfaceNames = []string{"eth0", "en0"}
	throwaway.StringVar(&c.InstanceAddr, "common.instance-addr", "", "Default advertised address to be used by Loki components.")
	throwaway.Var((*flagext.StringSlice)(&c.InstanceInterfaceNames), "common.instance-interface-names", "List of network interfaces to read address from.")
}

type Storage struct {
	S3       aws.S3Config            `yaml:"s3"`
	GCS      gcp.GCSConfig           `yaml:"gcs"`
	Azure    azure.BlobStorageConfig `yaml:"azure"`
	Swift    openstack.SwiftConfig   `yaml:"swift"`
	FSConfig FilesystemConfig        `yaml:"filesystem"`
	Hedging  hedging.Config          `yaml:"hedging"`
}

func (s *Storage) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	s.S3.RegisterFlagsWithPrefix(prefix+".s3", f)
	s.GCS.RegisterFlagsWithPrefix(prefix+".gcs", f)
	s.Azure.RegisterFlagsWithPrefix(prefix+".azure", f)
	s.Swift.RegisterFlagsWithPrefix(prefix+".swift", f)
	s.FSConfig.RegisterFlagsWithPrefix(prefix+".filesystem", f)
	s.Hedging.RegisterFlagsWithPrefix(prefix, f)
}

type FilesystemConfig struct {
	ChunksDirectory string `yaml:"chunks_directory"`
	RulesDirectory  string `yaml:"rules_directory"`
}

func (cfg *FilesystemConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.ChunksDirectory, prefix+".chunk-directory", "", "Directory to store chunks in.")
	f.StringVar(&cfg.RulesDirectory, prefix+".rules-directory", "", "Directory to store rules in.")
}
