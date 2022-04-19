package common

import (
	"flag"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/netutil"

	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/pkg/util"

	util_log "github.com/grafana/loki/pkg/util/log"
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
	// If an interface does not have a private IP address it is filtered out, falling back to "eth0" and "en0" if none are left.
	InstanceInterfaceNames []string `yaml:"instance_interface_names" doc:"default=[<private network interfaces>]"`

	// InstanceAddr represents a common ip used by instances to advertise their address.
	//
	// For instance, the different Loki rings will have this stored in its key-value store to be later retrieved by other components.
	// You can check this during Loki execution under ring status pages (ex: `/ring` will output the address of the different ingester
	// instances).
	InstanceAddr string `yaml:"instance_addr"`
}

func (c *Config) RegisterFlags(_ *flag.FlagSet) {
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)
	throwaway.IntVar(&c.ReplicationFactor, "common.replication-factor", 3, "How many ingesters incoming data should be replicated to.")
	c.Storage.RegisterFlagsWithPrefix("common.storage", throwaway)
	c.Ring.RegisterFlagsWithPrefix("", "collectors/", throwaway)

	// instance related flags.
	c.InstanceInterfaceNames = netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
	throwaway.StringVar(&c.InstanceAddr, "common.instance-addr", "", "Default advertised address to be used by Loki components.")
	throwaway.Var((*flagext.StringSlice)(&c.InstanceInterfaceNames), "common.instance-interface-names", "List of network interfaces to read address from.")
}

type Storage struct {
	S3       aws.S3Config              `yaml:"s3"`
	GCS      gcp.GCSConfig             `yaml:"gcs"`
	Azure    azure.BlobStorageConfig   `yaml:"azure"`
	BOS      baidubce.BOSStorageConfig `yaml:"bos"`
	Swift    openstack.SwiftConfig     `yaml:"swift"`
	FSConfig FilesystemConfig          `yaml:"filesystem"`
	Hedging  hedging.Config            `yaml:"hedging"`
}

func (s *Storage) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	s.S3.RegisterFlagsWithPrefix(prefix+".s3", f)
	s.GCS.RegisterFlagsWithPrefix(prefix+".gcs", f)
	s.Azure.RegisterFlagsWithPrefix(prefix+".azure", f)
	s.Swift.RegisterFlagsWithPrefix(prefix+".swift", f)
	s.BOS.RegisterFlagsWithPrefix(prefix+".bos", f)
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
