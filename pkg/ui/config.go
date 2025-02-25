package ui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/netutil"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Config struct {
	NodeName            string        `yaml:"node_name" doc:"default=<hostname>"` // Name to use for this node in the cluster.
	AdvertiseAddr       string        `yaml:"advertise_addr"`
	InfNames            []string      `yaml:"interface_names" doc:"default=[<private network interfaces>]"`
	RejoinInterval      time.Duration `yaml:"rejoin_interval"`        // How frequently to rejoin the cluster to address split brain issues.
	ClusterMaxJoinPeers int           `yaml:"cluster_max_join_peers"` // Number of initial peers to join from the discovered set.
	ClusterName         string        `yaml:"cluster_name"`           // Name to prevent nodes without this identifier from joining the cluster.
	EnableIPv6          bool          `yaml:"enable_ipv6"`
	Debug               bool          `yaml:"debug"`
	Discovery           struct {
		JoinPeers []string `yaml:"join_peers"`
	} `yaml:"discovery"`

	AdvertisePort int `yaml:"-"`
}

func (cfg Config) WithAdvertisePort(port int) Config {
	cfg.AdvertisePort = port
	return cfg
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	hostname, err := os.Hostname()
	if err != nil {
		panic(fmt.Errorf("failed to get hostname %s", err))
	}
	cfg.InfNames = netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
	f.Var((*flagext.StringSlice)(&cfg.InfNames), "ui.interface", "Name of network interface to read address from.")
	f.StringVar(&cfg.NodeName, "ui.node-name", hostname, "Name to use for this node in the cluster.")
	f.StringVar(&cfg.AdvertiseAddr, "ui.advertise-addr", "", "IP address to advertise in the cluster.")
	f.DurationVar(&cfg.RejoinInterval, "ui.rejoin-interval", 3*time.Minute, "How frequently to rejoin the cluster to address split brain issues.")
	f.IntVar(&cfg.ClusterMaxJoinPeers, "ui.cluster-max-join-peers", 3, "Number of initial peers to join from the discovered set.")
	f.StringVar(&cfg.ClusterName, "ui.cluster-name", "", "Name to prevent nodes without this identifier from joining the cluster.")
	f.BoolVar(&cfg.EnableIPv6, "ui.enable-ipv6", false, "Enable using a IPv6 instance address.")
	f.Var((*flagext.StringSlice)(&cfg.Discovery.JoinPeers), "ui.discovery.join-peers", "List of peers to join the cluster. Supports multiple values separated by commas. Each value can be a hostname, an IP address, or a DNS name (A/AAAA and SRV records).")
	f.BoolVar(&cfg.Debug, "ui.debug", false, "Enable debug logging for the UI.")
}

func (cfg Config) Validate() error {
	if cfg.NodeName == "" {
		return errors.New("node name is required")
	}

	return nil
}
