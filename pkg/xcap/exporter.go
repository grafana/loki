package xcap

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("xcap")

// ExportTrace exports a Capture as OpenTelemetry traces.
//
// Each region in the capture becomes a span. Spans are linked using
// the parent-child relationships defined by the regions.
//
// Observations within a region are added as attributes to the corresponding span.
func ExportTrace(ctx context.Context, capture *Capture, logger log.Logger) error {
	if capture == nil {
		return nil
	}

	regions := capture.regions

	// Build a map from region id to list of child regions
	idToChildren := make(map[identifier][]*Region)
	for _, r := range regions {
		// Ensure region is ended. If its already ended, this is a no-op.
		r.End()

		idToChildren[r.parentID] = append(idToChildren[r.parentID], r)
	}

	// Find all root regions (regions with zero parentID)
	rootRegions := idToChildren[zeroID]
	for _, rootRegion := range rootRegions {
		if err := createSpans(ctx, rootRegion, idToChildren); err != nil {
			level.Error(logger).Log("msg", "failed to create spans for root region", "id", rootRegion.id.String(), "err", err)
			continue
		}
	}

	return nil
}

// createSpans creates a span for the given region and recursively creates spans for its children.
func createSpans(ctx context.Context, region *Region, parentToChildren map[identifier][]*Region) error {
	// Build span options
	opts := []trace.SpanStartOption{
		trace.WithTimestamp(region.startTime),
		trace.WithAttributes(region.attributes...),
	}

	// Add observations as attributes
	for key, obs := range region.observations {
		opts = append(opts, trace.WithAttributes(observationToAttribute(key, obs)))
	}

	// Create the span
	ctx, span := tracer.Start(ctx, region.name, opts...)

	children := parentToChildren[region.id]
	// Recursively create spans for children
	for _, child := range children {
		if err := createSpans(ctx, child, parentToChildren); err != nil {
			return err
		}
	}

	// End the span with the region's end time
	span.End(trace.WithTimestamp(region.endTime))
	return nil
}

// observationToAttribute converts an observation to an OpenTelemetry attribute.
// The attribute key is the statistic name, and the value depends on the data type.
func observationToAttribute(key StatisticKey, obs *AggregatedObservation) attribute.KeyValue {
	attrKey := attribute.Key(key.Name)

	// TODO: export _ns as duration string for readability.

	switch key.DataType {
	case DataTypeInt64:
		if val, ok := obs.Value.(int64); ok {
			return attrKey.Int64(val)
		}
	case DataTypeFloat64:
		if val, ok := obs.Value.(float64); ok {
			return attrKey.Float64(val)
		}
	case DataTypeBool:
		if val, ok := obs.Value.(bool); ok {
			return attrKey.Bool(val)
		}
	}

	// Fallback: convert to string
	return attrKey.String(fmt.Sprintf("%v", obs.Value))
}
