// Copyright 2026 Google LLC
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

const (
	// envOtelMetrics is the environment variable that allows the user to
	// enable OpenTelemetry metrics. When set to "true", the storage client will
	// capture standard operation and transport-level metrics.
	envOtelMetrics = "GCP_STORAGE_GO_ENABLE_OTEL_METRICS"

	// envOtelDebugMetrics is the environment variable that allows the user to
	// enable advanced OpenTelemetry debug and network metrics. When set to "true",
	// the storage client will capture additional connection metrics such as DNS, TCP, and TLS durations.
	envOtelDebugMetrics = "GCP_STORAGE_GO_ENABLE_OTEL_DEBUG_METRICS"

	// storageOtelTracingDevVar is the environment variable that enables OTel tracing.
	storageOtelTracingDevVar = "GO_STORAGE_DEV_OTEL_TRACING"

	// storageBucketMetadataDisabledVar is the environment variable that disables
	// Autorequester Bucket Metadata optimization.
	storageBucketMetadataDisabledVar = "GO_OTEL_BUCKETMETADATA_DISABLED"
)
