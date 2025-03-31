package kafka

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/grafana/dskit/flagext"
)

const (
	consumeFromLastOffset = "last-offset"
	consumeFromStart      = "start"
	consumeFromEnd        = "end"
	consumeFromTimestamp  = "timestamp"

	// ProducerBatchMaxBytes is the max allowed size of a batch of Kafka records.
	ProducerBatchMaxBytes = 16_000_000

	// MaxProducerRecordDataBytesLimit is the max allowed size of a single record data. Given we have a limit
	// on the max batch size (ProducerBatchMaxBytes), a Kafka record data can't be bigger than the batch size
	// minus some overhead required to serialise the batch and the record itself. We use 16KB as such overhead
	// in the worst case scenario, which is expected to be way above the actual one.
	MaxProducerRecordDataBytesLimit = ProducerBatchMaxBytes - 16384
	minProducerRecordDataBytesLimit = 1024 * 1024
)

var (
	ErrMissingKafkaAddress                 = errors.New("the Kafka address has not been configured")
	ErrMissingKafkaTopic                   = errors.New("the Kafka topic has not been configured")
	ErrInconsistentSASLUsernameAndPassword = errors.New("both sasl username and password must be set")
	ErrInvalidProducerMaxRecordSizeBytes   = fmt.Errorf("the configured producer max record size bytes must be a value between %d and %d", minProducerRecordDataBytesLimit, MaxProducerRecordDataBytesLimit)
)

// Config holds the generic config for the Kafka backend.
type Config struct {
	Address      string        `yaml:"address"`
	Topic        string        `yaml:"topic"`
	ClientID     string        `yaml:"client_id"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`

	SASLUsername string         `yaml:"sasl_username"`
	SASLPassword flagext.Secret `yaml:"sasl_password"`

	ConsumerGroup                     string        `yaml:"consumer_group"`
	ConsumerGroupOffsetCommitInterval time.Duration `yaml:"consumer_group_offset_commit_interval"`

	LastProducedOffsetRetryTimeout time.Duration `yaml:"last_produced_offset_retry_timeout"`

	AutoCreateTopicEnabled           bool `yaml:"auto_create_topic_enabled"`
	AutoCreateTopicDefaultPartitions int  `yaml:"auto_create_topic_default_partitions"`

	ProducerMaxRecordSizeBytes int   `yaml:"producer_max_record_size_bytes"`
	ProducerMaxBufferedBytes   int64 `yaml:"producer_max_buffered_bytes"`

	MaxConsumerLagAtStartup time.Duration `yaml:"max_consumer_lag_at_startup"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("kafka", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Address, prefix+".address", "localhost:9092", "The Kafka backend address.")
	f.StringVar(&cfg.Topic, prefix+".topic", "", "The Kafka topic name.")
	f.StringVar(&cfg.ClientID, prefix+".client-id", "", "The Kafka client ID.")
	f.DurationVar(&cfg.DialTimeout, prefix+".dial-timeout", 2*time.Second, "The maximum time allowed to open a connection to a Kafka broker.")
	f.DurationVar(&cfg.WriteTimeout, prefix+".write-timeout", 10*time.Second, "How long to wait for an incoming write request to be successfully committed to the Kafka backend.")

	f.StringVar(&cfg.SASLUsername, prefix+".sasl-username", "", "The SASL username for authentication to Kafka using the PLAIN mechanism. Both username and password must be set.")
	f.Var(&cfg.SASLPassword, prefix+".sasl-password", "The SASL password for authentication to Kafka using the PLAIN mechanism. Both username and password must be set.")

	f.StringVar(&cfg.ConsumerGroup, prefix+".consumer-group", "", "The consumer group used by the consumer to track the last consumed offset. The consumer group must be different for each ingester zone.When empty, Loki uses the ingester instance ID.")
	f.DurationVar(&cfg.ConsumerGroupOffsetCommitInterval, prefix+".consumer-group-offset-commit-interval", time.Second, "How frequently a consumer should commit the consumed offset to Kafka. The last committed offset is used at startup to continue the consumption from where it was left.")

	f.DurationVar(&cfg.LastProducedOffsetRetryTimeout, prefix+".last-produced-offset-retry-timeout", 10*time.Second, "How long to retry a failed request to get the last produced offset.")

	f.BoolVar(&cfg.AutoCreateTopicEnabled, prefix+".auto-create-topic-enabled", true, "Enable auto-creation of Kafka topic if it doesn't exist.")
	f.IntVar(&cfg.AutoCreateTopicDefaultPartitions, prefix+".auto-create-topic-default-partitions", 1000, "When auto-creation of Kafka topic is enabled and this value is positive, Kafka's num.partitions configuration option is set on Kafka brokers with this value when Loki component that uses Kafka starts. This configuration option specifies the default number of partitions that the Kafka broker uses for auto-created topics. Note that this is a Kafka-cluster wide setting, and applies to any auto-created topic. If the setting of num.partitions fails, Loki proceeds anyways, but auto-created topics could have an incorrect number of partitions.")

	f.IntVar(&cfg.ProducerMaxRecordSizeBytes, prefix+".producer-max-record-size-bytes", MaxProducerRecordDataBytesLimit, "The maximum size of a Kafka record data that should be generated by the producer. An incoming write request larger than this size is split into multiple Kafka records. We strongly recommend to not change this setting unless for testing purposes.")
	f.Int64Var(&cfg.ProducerMaxBufferedBytes, prefix+".producer-max-buffered-bytes", 1024*1024*1024, "The maximum size of (uncompressed) buffered and unacknowledged produced records sent to Kafka. The produce request fails once this limit is reached. This limit is per Kafka client. 0 to disable the limit.")

	consumerLagUsage := fmt.Sprintf("Set -%s to 0 to disable waiting for maximum consumer lag being honored at startup.", prefix+".max-consumer-lag-at-startup")
	f.DurationVar(&cfg.MaxConsumerLagAtStartup, prefix+".max-consumer-lag-at-startup", 15*time.Second, "The guaranteed maximum lag before a consumer is considered to have caught up reading from a partition at startup, becomes ACTIVE in the hash ring and passes the readiness check. "+consumerLagUsage)
}

func (cfg *Config) Validate() error {
	if cfg.Address == "" {
		return ErrMissingKafkaAddress
	}
	if cfg.Topic == "" {
		return ErrMissingKafkaTopic
	}
	if cfg.ProducerMaxRecordSizeBytes < minProducerRecordDataBytesLimit || cfg.ProducerMaxRecordSizeBytes > MaxProducerRecordDataBytesLimit {
		return ErrInvalidProducerMaxRecordSizeBytes
	}
	if (cfg.SASLUsername == "") != (cfg.SASLPassword.String() == "") {
		return ErrInconsistentSASLUsernameAndPassword
	}

	return nil
}

// GetConsumerGroup returns the consumer group to use for the given instanceID and partitionID.
func (cfg *Config) GetConsumerGroup(instanceID string) string {
	if cfg.ConsumerGroup == "" {
		return instanceID
	}
	return cfg.ConsumerGroup
}
