package nats

import (
	"flag"

	"github.com/grafana/loki/pkg/util"
)

type Config struct {
	util.RingConfig `yaml:",inline"`
	ClusterPort     int    `yaml:"cluster_port"`
	ClientPort      int    `yaml:"client_port"`
	TraceLogging    bool   `yaml:"trace_logging"`
	DataPath        string `yaml:"data_path"`
	PushStreams     int    `yaml:"push_streams"`

	Cluster bool `yaml:"-"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("nats.", "collectors/", f)
	f.IntVar(&cfg.ClusterPort, "nats.cluster-port", 4248, "NATS cluster port.")
	f.IntVar(&cfg.ClientPort, "nats.client-port", 4222, "NATS client port.")
	f.BoolVar(&cfg.TraceLogging, "nats.trace-logging", false, "Enable NATS trace logging.")
	f.StringVar(&cfg.DataPath, "nats.data-path", "/tmp/nats/", "NATS data path.")
	f.IntVar(&cfg.PushStreams, "nats.push-streams", 16, "Number of streams to push to NATS.")
}
