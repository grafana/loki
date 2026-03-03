package client

import (
	"errors"
	"flag"
	"time"

	"github.com/grafana/dskit/flagext"
)

// Config contains configuration options for a [kgo.Client].
type Config struct {
	ClientID     string         `yaml:"client_id,omitempty"`
	Address      string         `yaml:"address,omitempty"`
	SASLUsername string         `yaml:"sasl_username,omitempty"`
	SASLPassword flagext.Secret `yaml:"sasl_password,omitempty"`
	DialTimeout  time.Duration  `yaml:"dial_timeout,omitempty"`
	Topic        string         `yaml:"topic,omitempty"`

	// Consume specific configuration options.

	// True if the topic name is a regular expression.
	TopicRegex bool `yaml:"topic_regex,omitempty"`

	// The (optional) name of the Consumer Group. Leave blank if direct
	// consuming.
	ConsumerGroup string `yaml:"consumer_group,omitempty"`

	// The minimum number of bytes a broker will attempt to send during a
	// fetch. Is used in [kgo.FetchMinBytes].
	FetchMinBytes int `yaml:"fetch_min_bytes,omitempty"`

	// The maximum number of bytes a broker will attempt to send during a
	// fetch. Is used in [kgo.FetchMaxBytes].
	FetchMaxBytes int `yaml:"fetch_max_bytes,omitempty"`

	// The maximum amount of time a broker will wait for a fetch response
	// to reach FetchMinBytes. Is used in [kgo.FetchMaxWait].
	FetchMaxWait time.Duration `yaml:"fetch_max_wait,omitempty"`

	// The maximum number of bytes consumed per partition per fetch request.
	// Is used in [kgo.FetchMaxPartitionBytes].
	FetchMaxPartitionBytes int `yaml:"fetch_max_partition_bytes,omitempty"`

	// Produce specific configuration options.

	// The maximum number of inflight requests per broker. Is used in
	// [kgo.MaxInFlightRequestsPerBroker].
	MaxInFlightRequestsPerBroker int `yaml:"max_in_flight_requests_per_broker,omitempty"`
}

// RegisterFlags registers the flags with the default prefix.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("kafka-clientv2", f)
}

// RegisterFlagsWithPrefix registers the flags with the given prefix.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.ClientID, prefix+"client-id", "", "The client ID.")
	f.StringVar(&c.Address, prefix+"address", "localhost:9092", "The address of the Kafka broker.")
	f.StringVar(&c.SASLUsername, prefix+"sasl-username", "", "The SASL username for authentication to Kafka using the PLAIN mechanism. Both username and password must be set.")
	f.Var(&c.SASLPassword, prefix+"sasl-password", "The SASL password for authentication to Kafka using the PLAIN mechanism. Both username and password must be set.")
	f.DurationVar(&c.DialTimeout, prefix+"dial-timeout", 10*time.Second, "")
	f.StringVar(&c.Topic, prefix+"topic", "", "The topic name.")

	// Consume specific configuration options.
	f.BoolVar(&c.TopicRegex, prefix+"topic-regex", false, "The topic name is a regular expression.")
	f.IntVar(&c.FetchMinBytes, prefix+"fetch-min-bytes", 1, "The minimum number of bytes a broker will attempt to send during a fetch.")
	f.IntVar(&c.FetchMaxBytes, prefix+"fetch-max-bytes", 100*1024*1024, "The maximum number of bytes a broker will attempt to send during a fetch.")
	f.DurationVar(&c.FetchMaxWait, prefix+"fetch-max-wait", 5*time.Second, "The maximum amount of time a broker will wait for a fetch response to reach fetch-max-bytes.")
	f.IntVar(&c.FetchMaxPartitionBytes, prefix+"fetch-max-partition-bytes", 50*1024*1024, "The maximum number of bytes consumed per partition per fetch request.")

	// Produce specific configuration options.
	f.IntVar(&c.MaxInFlightRequestsPerBroker, prefix+"max-in-flight-requests-per-broker", 1, "The maximum number of inflight requests per broker.")
}

func (c *Config) Validate() error {
	if c.ClientID == "" {
		return errors.New("The Client ID is required.")
	}
	if c.Address == "" {
		return errors.New("The address of the Kafka broker is required.")
	}
	if c.DialTimeout <= 0 {
		return errors.New("The dial timeout must be greater than 0.")
	}
	if c.FetchMinBytes <= 0 {
		return errors.New("Fetch min bytes must be greater than 0.")
	}
	if c.FetchMaxBytes <= 0 {
		return errors.New("Fetch max bytes must be greater than 0.")
	}
	if c.FetchMaxWait <= 0 {
		return errors.New("Fetch max wait must be greater than 0.")
	}
	if c.FetchMaxPartitionBytes <= 0 {
		return errors.New("Fetch max partition bytes must be greater than 0.")
	}
	if c.MaxInFlightRequestsPerBroker <= 0 {
		return errors.New("Max in-flight requests per broker must be greater than 0.")
	}
	return nil
}
