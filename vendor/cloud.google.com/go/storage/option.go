// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
)

// storageConfig contains the Storage client option configuration that can be
// set through storageClientOptions.
type storageConfig struct {
	useJSONforReads      bool
	readAPIWasSet        bool
	disableClientMetrics bool
}

// newStorageConfig generates a new storageConfig with all the given
// storageClientOptions applied.
func newStorageConfig(opts ...option.ClientOption) storageConfig {
	var conf storageConfig
	for _, opt := range opts {
		if storageOpt, ok := opt.(storageClientOption); ok {
			storageOpt.ApplyStorageOpt(&conf)
		}
	}
	return conf
}

// A storageClientOption is an option for a Google Storage client.
type storageClientOption interface {
	option.ClientOption
	ApplyStorageOpt(*storageConfig)
}

// WithJSONReads is an option that may be passed to [NewClient].
// It sets the client to use the Cloud Storage JSON API for object
// reads. Currently, the default API used for reads is XML, but JSON will
// become the default in a future release.
//
// Setting this option is required to use the GenerationNotMatch condition. We
// also recommend using JSON reads to ensure consistency with other client
// operations (all of which use JSON by default).
//
// Note that when this option is set, reads will return a zero date for
// [ReaderObjectAttrs].LastModified and may return a different value for
// [ReaderObjectAttrs].CacheControl.
func WithJSONReads() option.ClientOption {
	return &withReadAPI{useJSON: true}
}

// WithXMLReads is an option that may be passed to [NewClient].
// It sets the client to use the Cloud Storage XML API for object reads.
//
// This is the current default, but the default will switch to JSON in a future
// release.
func WithXMLReads() option.ClientOption {
	return &withReadAPI{useJSON: false}
}

type withReadAPI struct {
	internaloption.EmbeddableAdapter
	useJSON bool
}

func (w *withReadAPI) ApplyStorageOpt(c *storageConfig) {
	c.useJSONforReads = w.useJSON
	c.readAPIWasSet = true
}

type withDisabledClientMetrics struct {
	internaloption.EmbeddableAdapter
	disabledClientMetrics bool
}

// WithDisabledClientMetrics is an option that may be passed to [NewClient].
// gRPC metrics are enabled by default in the GCS client and will export the
// gRPC telemetry discussed in [gRFC/66] and [gRFC/78] to
// [Google Cloud Monitoring]. The option is used to disable metrics.
// Google Cloud Support can use this information to more quickly diagnose
// problems related to GCS and gRPC.
// Sending this data does not incur any billing charges, and requires minimal
// CPU (a single RPC every few minutes) or memory (a few KiB to batch the
// telemetry).
//
// The default is to enable client metrics. To opt-out of metrics collected use
// this option.
//
// [gRFC/66]: https://github.com/grpc/proposal/blob/master/A66-otel-stats.md
// [gRFC/78]: https://github.com/grpc/proposal/blob/master/A78-grpc-metrics-wrr-pf-xds.md
// [Google Cloud Monitoring]: https://cloud.google.com/monitoring/docs
func WithDisabledClientMetrics() option.ClientOption {
	return &withDisabledClientMetrics{disabledClientMetrics: true}
}

func (w *withDisabledClientMetrics) ApplyStorageOpt(c *storageConfig) {
	c.disableClientMetrics = w.disabledClientMetrics
}
