// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package metric provides an OpenTelemetry metric exporter for Google Cloud Monitoring.
//
// Deprecated: Google Cloud OpenTelemetry Monitoring exporter for Go is deprecated and will be archived after January 1st, 2027.
// Please migrate to the OpenTelemetry OTLP exporters. For migration details, see
// https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/MIGRATION.md
package metric

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"golang.org/x/oauth2/google"
)

var logDeprecatedOnce sync.Once

func logDeprecated() {
	logDeprecatedOnce.Do(func() {
		log.Println("Google Cloud OpenTelemetry Monitoring exporter for Go is deprecated and will be archived after January 1st, 2027. Please migrate to the OpenTelemetry OTLP exporters. For migration details, see https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/MIGRATION.md")
	})
}

// New creates a new Exporter thats implements metric.Exporter.
//
// Deprecated: Google Cloud OpenTelemetry Monitoring exporter for Go is deprecated and will be archived after January 1st, 2027.
// Please migrate to the OpenTelemetry OTLP exporters. For migration details, see
// https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/MIGRATION.md
func New(opts ...Option) (sdkmetric.Exporter, error) {
	logDeprecated()
	o := options{
		context:                 context.Background(),
		resourceAttributeFilter: DefaultResourceAttributesFilter,
	}
	for _, opt := range opts {
		opt(&o)
	}

	if o.projectID == "" {
		creds, err := google.FindDefaultCredentials(o.context, monitoring.DefaultAuthScopes()...)
		if err != nil {
			return nil, fmt.Errorf("failed to find Google Cloud credentials: %v", err)
		}
		if creds.ProjectID == "" {
			return nil, errors.New("google cloud monitoring: no project found with application default credentials")
		}
		o.projectID = creds.ProjectID
	}
	return newMetricExporter(&o)
}
