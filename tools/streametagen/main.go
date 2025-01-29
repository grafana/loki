package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/memberlist"
	dskit_log "github.com/grafana/dskit/log"
	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	metricsNamespace = "loki_streammetagen_"
	ingesterRingName = "ingester"
)

type Config struct {
	NumTenants       int      `yaml:"num_tenants"`
	QPSPerTenant     int      `yaml:"qps_per_tenant"`
	StreamsPerTenant int      `yaml:"streams_per_tenant"`
	StreamLabels     []string `yaml:"stream_labels"`

	LogLevel       dskit_log.Level `yaml:"log_level"`
	HttpListenPort int             `yaml:"http_listen_port"`

	MemberlistKV        memberlist.KVConfig   `yaml:"memberlist"`
	Kafka               kafka.Config          `yaml:"kafka"`
	PartitionRingConfig partitionring.Config  `yaml:"partition_ring" category:"experimental"`
	LifecyclerConfig    ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the ingester will operate and where it will register for discovery."`
}

func (c *Config) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	f.IntVar(&c.NumTenants, "tenants.total", 1, "Number of tenants to generate metadata for")
	f.IntVar(&c.QPSPerTenant, "tenants.qps", 10, "Number of QPS per tenant")
	f.IntVar(&c.StreamsPerTenant, "tenants.streams.total", 100, "Number of streams per tenant")
	f.IntVar(&c.HttpListenPort, "http-listen-port", 3100, "HTTP Listener port")

	streamLabelsDefault := []string{"cluster", "namespace", "job", "instance"}
	streamLabelsVar := flag.Value((*util.StringSliceCSV)(&c.StreamLabels))
	f.Var(streamLabelsVar, "stream.labels", fmt.Sprintf("The labels to generate for each stream (comma-separated) (default: %s)", strings.Join(streamLabelsDefault, ",")))
	c.StreamLabels = streamLabelsDefault

	c.LogLevel.RegisterFlags(f)
	c.Kafka.RegisterFlags(f)
	c.PartitionRingConfig.RegisterFlagsWithPrefix("", f)
	c.LifecyclerConfig.RegisterFlagsWithPrefix("stream-metadata-generator.", f, logger)
	c.MemberlistKV.RegisterFlags(f)
}

// ... rest of the file unchanged ...
