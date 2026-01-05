package xcap

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("xcap")

// ExportTrace exports a Capture as OpenTelemetry traces.
//
// Each region in the capture becomes a span. Spans are linked using
// the parent-child relationships defined by the regions.
//
// Observations within a region are added as attributes to the corresponding span.
func ExportTrace(ctx context.Context, capture *Capture, logger log.Logger) {
	if capture == nil {
		return
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

	// Add events to the span
	for _, event := range region.events {
		span.AddEvent(event.Name,
			trace.WithTimestamp(event.Timestamp),
			trace.WithAttributes(event.Attributes...),
		)
	}

	// Set status if not unset
	if region.status.Code != codes.Unset {
		span.SetStatus(region.status.Code, region.status.Message)
	}

	// Recursively create spans for children
	children := parentToChildren[region.id]
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

// SummaryLogValues exports a Capture as a structured log line with aggregated statistics.
func SummaryLogValues(capture *Capture) []any {
	if capture == nil {
		return nil
	}

	return summarizeObservations(capture).toLogValues()
}

// summarizeObservations collects and summarizes observations from the capture.
func summarizeObservations(capture *Capture) *observations {
	if capture == nil {
		return nil
	}

	collect := newObservationCollector(capture)
	result := newObservations()

	// collect observations from all DataObjScan regions. observations from
	// child regions are rolled-up to include dataset reader and bucket stats.
	// streamView is excluded as it is handled separately below.
	result.merge(
		collect.fromRegions("DataObjScan", true, "streamsView.init").
			filter(
				// object store calls
				StatBucketGet.Key(), StatBucketGetRange.Key(), StatBucketAttributes.Key(),
				// dataset reader stats
				StatDatasetMaxRows.Key(), StatDatasetRowsAfterPruning.Key(), StatDatasetReadCalls.Key(),
				StatDatasetPrimaryPagesDownloaded.Key(), StatDatasetSecondaryPagesDownloaded.Key(),
				StatDatasetPrimaryColumnBytes.Key(), StatDatasetSecondaryColumnBytes.Key(),
				StatDatasetPrimaryRowsRead.Key(), StatDatasetSecondaryRowsRead.Key(),
				StatDatasetPrimaryRowBytes.Key(), StatDatasetSecondaryRowBytes.Key(),
				StatDatasetPagesScanned.Key(), StatDatasetPagesFoundInCache.Key(),
				StatDatasetPageDownloadRequests.Key(), StatDatasetPageDownloadTime.Key(),
			).
			prefix("logs_dataset_").
			normalizeKeys(),
	)

	// metastore index and resolved section stats
	result.merge(
		collect.fromRegions("ObjectMetastore.Sections", true).
			filter(StatMetastoreIndexObjects.Key(), StatMetastoreSectionsResolved.Key()).
			normalizeKeys(),
	)

	result.merge(
		collect.fromRegions("PointersScan", true).
			filter(
				StatMetastoreStreamsRead.Key(),
				StatMetastoreStreamsReadTime.Key(),
				StatMetastoreSectionPointersRead.Key(),
				StatMetastoreSectionPointersReadTime.Key(),
			).
			normalizeKeys(),
	)

	// metastore bucket and dataset reader stats
	result.merge(
		collect.fromRegions("ObjectMetastore.Sections", true).
			filter(
				StatBucketGet.Key(), StatBucketGetRange.Key(), StatBucketAttributes.Key(),
				StatDatasetPrimaryPagesDownloaded.Key(), StatDatasetSecondaryPagesDownloaded.Key(),
				StatDatasetPrimaryColumnBytes.Key(), StatDatasetSecondaryColumnBytes.Key(),
			).
			prefix("metastore_").
			normalizeKeys(),
	)

	// streamsView bucket and dataset reader stats
	result.merge(
		collect.fromRegions("streamsView.init", true).
			filter(
				StatBucketGet.Key(), StatBucketGetRange.Key(), StatBucketAttributes.Key(),
				StatDatasetPrimaryPagesDownloaded.Key(), StatDatasetSecondaryPagesDownloaded.Key(),
				StatDatasetPrimaryColumnBytes.Key(), StatDatasetSecondaryColumnBytes.Key(),
			).
			prefix("streams_").
			normalizeKeys(),
	)

	return result
}
