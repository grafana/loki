// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/tracing/tracing.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var tracer = otel.Tracer("dskit/tracing")

type Closer struct {
	*tracesdk.TracerProvider
}

func (c Closer) Close() error {
	return c.Shutdown(context.Background())
}

// NewResource creates a new OpenTelemetry resource using the provided service name and custom attributes.
// This resource will be used for creating both tracers and meters, enriching telemetry data with context.
func NewResource(serviceName string, customAttributes []attribute.KeyValue) (*resource.Resource, error) {
	// Append the service name as an attribute to the custom attributes list.
	customAttributes = append(customAttributes, semconv.ServiceName(serviceName))

	// Merge the default resource with the new resource containing custom attributes.
	// This ensures that standard attributes are retained while adding custom ones.
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			customAttributes...,
		),
	)
}
