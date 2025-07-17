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

type GoldfishConfig struct {
	Enable           bool   `yaml:"enable"`            // Whether to enable the Goldfish query comparison feature.
	CloudSQLUser     string `yaml:"cloudsql_user"`     // CloudSQL username
	CloudSQLHost     string `yaml:"cloudsql_host"`     // CloudSQL host
	CloudSQLPort     int    `yaml:"cloudsql_port"`     // CloudSQL port
	CloudSQLDatabase string `yaml:"cloudsql_database"` // CloudSQL database name
	MaxConnections   int    `yaml:"max_connections"`   // Maximum number of database connections
	MaxIdleTime      int    `yaml:"max_idle_time"`     // Maximum idle time for connections in seconds
}

type Config struct {
	Enabled             bool           `yaml:"enabled"`                            // Whether to enable the UI.
	NodeName            string         `yaml:"node_name" doc:"default=<hostname>"` // Name to use for this node in the cluster.
	AdvertiseAddr       string         `yaml:"advertise_addr"`
	InfNames            []string       `yaml:"interface_names" doc:"default=[<private network interfaces>]"`
	RejoinInterval      time.Duration  `yaml:"rejoin_interval"`        // How frequently to rejoin the cluster to address split brain issues.
	ClusterMaxJoinPeers int            `yaml:"cluster_max_join_peers"` // Number of initial peers to join from the discovered set.
	ClusterName         string         `yaml:"cluster_name"`           // Name to prevent nodes without this identifier from joining the cluster.
	EnableIPv6          bool           `yaml:"enable_ipv6"`
	Debug               bool           `yaml:"debug"`
	Goldfish            GoldfishConfig `yaml:"goldfish"` // Goldfish query comparison configuration
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
	f.BoolVar(&cfg.Enabled, "ui.enabled", false, "Enable the experimental Loki UI.")
	f.Var((*flagext.StringSlice)(&cfg.InfNames), "ui.interface", "Name of network interface to read address from.")
	f.StringVar(&cfg.NodeName, "ui.node-name", hostname, "Name to use for this node in the cluster.")
	f.StringVar(&cfg.AdvertiseAddr, "ui.advertise-addr", "", "IP address to advertise in the cluster.")
	f.DurationVar(&cfg.RejoinInterval, "ui.rejoin-interval", 3*time.Minute, "How frequently to rejoin the cluster to address split brain issues.")
	f.IntVar(&cfg.ClusterMaxJoinPeers, "ui.cluster-max-join-peers", 3, "Number of initial peers to join from the discovered set.")
	f.StringVar(&cfg.ClusterName, "ui.cluster-name", "", "Name to prevent nodes without this identifier from joining the cluster.")
	f.BoolVar(&cfg.EnableIPv6, "ui.enable-ipv6", false, "Enable using a IPv6 instance address.")
	f.Var((*flagext.StringSlice)(&cfg.Discovery.JoinPeers), "ui.discovery.join-peers", "List of peers to join the cluster. Supports multiple values separated by commas. Each value can be a hostname, an IP address, or a DNS name (A/AAAA and SRV records).")
	f.BoolVar(&cfg.Debug, "ui.debug", false, "Enable debug logging for the UI.")

	// Goldfish configuration
	f.BoolVar(&cfg.Goldfish.Enable, "ui.goldfish.enable", false, "Enable the Goldfish query comparison feature.")
	f.StringVar(&cfg.Goldfish.CloudSQLUser, "ui.goldfish.cloudsql-user", "", "CloudSQL username for Goldfish database.")
	f.StringVar(&cfg.Goldfish.CloudSQLHost, "ui.goldfish.cloudsql-host", "127.0.0.1", "CloudSQL host for Goldfish database.")
	f.IntVar(&cfg.Goldfish.CloudSQLPort, "ui.goldfish.cloudsql-port", 3306, "CloudSQL port for Goldfish database.")
	f.StringVar(&cfg.Goldfish.CloudSQLDatabase, "ui.goldfish.cloudsql-database", "goldfish", "CloudSQL database name for Goldfish.")
	f.IntVar(&cfg.Goldfish.MaxConnections, "ui.goldfish.max-connections", 10, "Maximum number of database connections for Goldfish.")
	f.IntVar(&cfg.Goldfish.MaxIdleTime, "ui.goldfish.max-idle-time", 300, "Maximum idle time for database connections in seconds.")
}

func (cfg Config) Validate() error {
	// Skip validation if UI is disabled
	if !cfg.Enabled {
		return nil
	}

	// Perform normal validation for enabled UI
	if cfg.NodeName == "" {
		return errors.New("node name is required")
	}

	return nil
}
