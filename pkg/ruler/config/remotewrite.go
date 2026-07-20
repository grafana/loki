package config

import (
	remoteapi "github.com/prometheus/client_golang/exp/api/remote"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/storage/remote/azuread"
	"github.com/prometheus/prometheus/storage/remote/googleiam"
	"github.com/prometheus/sigv4"
)

// RemoteWriteConfig is the per-tenant overrides configuration for remote write.
// This is a copy of the [promconfig.RemoteWriteConfig] without custom unmarshaling.
// It is needed because we don't want field validation during unmarshaling, e.g. for null values.
// That allows tenants to specify only a subset of fields in the overrides.
type RemoteWriteConfig struct {
	URL                  *commonconfig.URL `yaml:"url,omitempty"`
	RemoteTimeout        model.Duration    `yaml:"remote_timeout,omitempty"`
	Headers              map[string]string `yaml:"headers,omitempty"`
	WriteRelabelConfigs  []*relabel.Config `yaml:"write_relabel_configs,omitempty"`
	Name                 string            `yaml:"name,omitempty"`
	SendExemplars        bool              `yaml:"send_exemplars,omitempty"`
	SendNativeHistograms bool              `yaml:"send_native_histograms,omitempty"`
	RoundRobinDNS        bool              `yaml:"round_robin_dns,omitempty"`
	// ProtobufMessage specifies the protobuf message to use against the remote
	// receiver as specified in https://prometheus.io/docs/specs/remote_write_spec_2_0/
	ProtobufMessage remoteapi.WriteMessageType `yaml:"protobuf_message,omitempty"`

	// // We cannot do proper Go type embedding below as the parser will then parse
	// // values arbitrarily into the overflow maps of further-down types.
	HTTPClientConfig commonconfig.HTTPClientConfig `yaml:",inline"`
	QueueConfig      promconfig.QueueConfig        `yaml:"queue_config,omitempty"`
	MetadataConfig   promconfig.MetadataConfig     `yaml:"metadata_config,omitempty"`
	SigV4Config      *sigv4.SigV4Config            `yaml:"sigv4,omitempty"`
	AzureADConfig    *azuread.AzureADConfig        `yaml:"azuread,omitempty"`
	GoogleIAMConfig  *googleiam.Config             `yaml:"google_iam,omitempty"`
}

// Static check to ensure safe conversion between structs
var _ = (*promconfig.RemoteWriteConfig)(&RemoteWriteConfig{})
