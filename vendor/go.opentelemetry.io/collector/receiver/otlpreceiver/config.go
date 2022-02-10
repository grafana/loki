// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package otlpreceiver // import "go.opentelemetry.io/collector/receiver/otlpreceiver"

import (
	"fmt"

	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confighttp"
)

const (
	// Protocol values.
	protoGRPC          = "grpc"
	protoHTTP          = "http"
	protocolsFieldName = "protocols"
)

// Protocols is the configuration for the supported protocols.
type Protocols struct {
	GRPC *configgrpc.GRPCServerSettings `mapstructure:"grpc"`
	HTTP *confighttp.HTTPServerSettings `mapstructure:"http"`
}

// Config defines configuration for OTLP receiver.
type Config struct {
	config.ReceiverSettings `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct
	// Protocols is the configuration for the supported protocols, currently gRPC and HTTP (Proto and JSON).
	Protocols `mapstructure:"protocols"`
}

var _ config.Receiver = (*Config)(nil)
var _ config.Unmarshallable = (*Config)(nil)

// Validate checks the receiver configuration is valid
func (cfg *Config) Validate() error {
	if cfg.GRPC == nil &&
		cfg.HTTP == nil {
		return fmt.Errorf("must specify at least one protocol when using the OTLP receiver")
	}
	return nil
}

// Unmarshal a config.Map into the config struct.
func (cfg *Config) Unmarshal(componentParser *config.Map) error {
	if componentParser == nil || len(componentParser.AllKeys()) == 0 {
		return fmt.Errorf("empty config for OTLP receiver")
	}
	// first load the config normally
	err := componentParser.UnmarshalExact(cfg)
	if err != nil {
		return err
	}

	// next manually search for protocols in the config.Map, if a protocol is not present it means it is disabled.
	protocols, err := componentParser.Sub(protocolsFieldName)
	if err != nil {
		return err
	}

	if !protocols.IsSet(protoGRPC) {
		cfg.GRPC = nil
	}

	if !protocols.IsSet(protoHTTP) {
		cfg.HTTP = nil
	}

	return nil
}
