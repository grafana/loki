// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package secretvalue holds the switch that controls whether secret.Secret
// values are exposed when marshalled. It lets package config keep
// config.MarshalSecretValue as the single, user-facing switch.
package secretvalue

// Marshal points to the switch consulted when marshalling a secret.Secret.
// Package config points it at config.MarshalSecretValue.
var Marshal = new(bool)
