package generator

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/memberlist"
	dskit_log "github.com/grafana/dskit/log"
	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/kafka"
	frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	distributor_client "github.com/grafana/loki/v3/tools/stream-generator/distributor/client"
)

// Define default sizes for generated streams
const (
	normalLogSize    = 100 // 100 bytes for normal log lines
	entriesPerStream = 5   // 5 entries per stream
)

const (
	MetricsNamespace    = "loki_stream_generator_"
	distributorRingName = "distributor"
	distributorRingKey  = "distributor"
)

type PushModeType string

const (
	PushStreamMetadataOnly PushModeType = "stream-metadata"
	PushStream             PushModeType = "stream"
)

type Config struct {
	NumPartitions             int           `yaml:"num_partitions"`
	NumTenants                int           `yaml:"num_tenants"`
	TenantPrefix              string        `yaml:"tenant_prefix"`
	QPSPerTenant              int           `yaml:"qps_per_tenant"`
	CreateBatchSize           int           `yaml:"create_batch_size"`
	CreateNewStreamsInterval  time.Duration `yaml:"create_new_streams_interval"`
	StreamsPerTenant          int           `yaml:"streams_per_tenant"`
	StreamLabels              []string      `yaml:"stream_labels"`
	MaxGlobalStreamsPerTenant int           `yaml:"max_global_streams_per_tenant"`
	PushMode                  PushModeType  `yaml:"push_mode"`
	pushModeRaw               string

	// Stream size control parameter
	DesiredRate int `yaml:"desired_rate"`

	LogLevel       dskit_log.Level `yaml:"log_level,omitempty"`
	HTTPListenPort int             `yaml:"http_listen_port,omitempty"`

	// Memberlist config
	MemberlistKV memberlist.KVConfig `yaml:"memberlist,omitempty"`

	// Kafka config
	Kafka kafka.Config `yaml:"kafka,omitempty"`

	// Lifecycler config
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	// Client configs
	DistributorClientConfig distributor_client.Config `yaml:"distributor_client,omitempty"`
	FrontendClientConfig    frontend_client.Config    `yaml:"frontend_client,omitempty"`
}

type streamLabelsFlag []string

func (s *streamLabelsFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *streamLabelsFlag) Set(value string) error {
	*s = strings.Split(value, ",")
	return nil
}

func (c *Config) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	f.IntVar(&c.NumPartitions, "partitions.total", 64, "Number of partitions to generate metadata for")
	f.IntVar(&c.NumTenants, "tenants.total", 1, "Number of tenants to generate metadata for")
	f.StringVar(&c.TenantPrefix, "tenants.prefix", "", "Prefix for tenant IDs")
	f.IntVar(&c.QPSPerTenant, "tenants.qps", 10, "Number of QPS per tenant")
	f.IntVar(&c.CreateBatchSize, "tenants.streams.create-batch-size", 100, "Number of streams to send to Kafka per tick")
	f.DurationVar(&c.CreateNewStreamsInterval, "tenants.streams.create-interval", 1*time.Minute, "Number of milliseconds to wait between batches. If set to 0, it will be calculated based on QPSPerTenant.")
	f.IntVar(&c.StreamsPerTenant, "tenants.streams.total", 100, "Number of streams per tenant")
	f.IntVar(&c.MaxGlobalStreamsPerTenant, "tenants.max-global-streams", 1000, "Maximum number of global streams per tenant")
	f.IntVar(&c.HTTPListenPort, "http-listen-port", 3100, "HTTP Listener port")
	f.StringVar(&c.pushModeRaw, "push-mode", string(PushStreamMetadataOnly), "Push mode (values: stream-metadata (default), stream)")

	// Add ingestion rate control parameter
	f.IntVar(&c.DesiredRate, "tenants.streams.desired-rate", 1048576, "Desired ingestion rate in bytes per second (default: 1MB/s)")

	// Set default stream labels
	defaultLabels := []string{"cluster", "namespace", "job", "instance"}
	c.StreamLabels = defaultLabels

	// Create a custom flag for stream labels
	streamLabels := (*streamLabelsFlag)(&c.StreamLabels)
	f.Var(streamLabels, "stream.labels", fmt.Sprintf("The labels to generate for each stream (comma-separated) (default: %s)", strings.Join(defaultLabels, ",")))

	c.LogLevel.RegisterFlags(f)
	c.Kafka.RegisterFlags(f)
	c.LifecyclerConfig.RegisterFlagsWithPrefix("ring.", f, logger)
	c.DistributorClientConfig.RegisterFlagsWithPrefix("distributor-client", f)
	c.FrontendClientConfig.RegisterFlagsWithPrefix("ingest-limits-frontend-client", f)
	c.MemberlistKV.RegisterFlags(f)
}

func (c *Config) Validate() error {
	if c.pushModeRaw != string(PushStreamMetadataOnly) && c.pushModeRaw != string(PushStream) {
		return fmt.Errorf("invalid push mode: %s", c.pushModeRaw)
	}
	c.PushMode = PushModeType(c.pushModeRaw)

	if c.CreateNewStreamsInterval <= 0 {
		c.CreateNewStreamsInterval = time.Second / time.Duration(c.QPSPerTenant)
	}

	return nil
}
