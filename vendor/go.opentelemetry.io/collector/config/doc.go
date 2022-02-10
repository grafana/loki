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

// Package config defines the data models for entities. This file defines the
// models for configuration format. The defined entities are:
// Config (the top-level structure), Receivers, Exporters, Processors, Pipelines.
//
// Receivers, Exporters and Processors typically have common configuration settings, however
// sometimes specific implementations will have extra configuration settings.
// This requires the configuration data for these entities to be polymorphic.
//
// To satisfy these requirements we declare interfaces Receiver, Exporter, Processor,
// which define the behavior. We also provide helper structs ReceiverSettings, ExporterSettings,
// ProcessorSettings, which define the common settings and unmarshaling from config files.
//
// Specific Receivers/Exporters/Processors are expected to at the minimum implement the
// corresponding interface and if they have additional settings they must also extend
// the corresponding common settings struct (the easiest approach is to embed the common struct).
package config // import "go.opentelemetry.io/collector/config"
