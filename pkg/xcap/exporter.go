package xcap

import (
	"context"
	"fmt"
	"time"

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
// When cfg.SeparateDetailedTrace is enabled (default), the export creates two traces:
//  1. A summary span in the global trace (connected to the parent context) that
//     represents the entire xcap execution with aggregated statistics.
//  2. A separate detailed trace where xcap is the root span, containing all
//     individual region spans with their full hierarchy.
//
// The summary span includes a span link pointing to the detailed trace root,
// allowing users to navigate from the high-level view to the detailed breakdown.
//
// When cfg.SeparateDetailedTrace is disabled, all spans are created directly under
// the parent context, resulting in a single trace with nested xcap spans.
//
// Observations within a region are added as attributes to the corresponding span.
func ExportTrace(ctx context.Context, capture *Capture, cfg Config, logger log.Logger) {
	if capture == nil {
		return
	}

	regions := capture.regions
	if len(regions) == 0 {
		return
	}

	// Build a map from region id to list of child regions
	idToChildren := make(map[identifier][]*Region)
	for _, r := range regions {
		// Ensure region is ended. If its already ended, this is a no-op.
		r.End()

		idToChildren[r.parentID] = append(idToChildren[r.parentID], r)
	}

	// Find all root regions (regions with zero parentID)
	rootRegions := idToChildren[zeroID]
	if len(rootRegions) == 0 {
		return
	}

	if cfg.SeparateDetailedTrace {
		exportSeparateTraces(ctx, capture, regions, rootRegions, idToChildren, logger)
	} else {
		exportNestedTrace(ctx, rootRegions, idToChildren, logger)
	}
}

// exportSeparateTraces exports xcap as two separate traces:
// 1. A summary span in the global trace with a link to the detailed trace
// 2. A separate detailed trace where xcap is the root span
func exportSeparateTraces(ctx context.Context, capture *Capture, regions []*Region, rootRegions []*Region, idToChildren map[identifier][]*Region, logger log.Logger) {
	// Calculate the time bounds for the entire capture
	startTime, endTime := calculateCaptureBounds(regions)

	// Step 1: Create the detailed trace with xcap as root (new trace, no parent)
	// We use a fresh context to start a new trace
	detailedCtx, detailedRootSpan := tracer.Start(
		context.Background(),
		"xcap",
		trace.WithTimestamp(startTime),
		trace.WithNewRoot(), // Ensures this is a new trace
		trace.WithAttributes(
			attribute.Int("xcap.region_count", len(regions)),
			attribute.Int("xcap.root_region_count", len(rootRegions)),
		),
	)

	// Create all child spans under the detailed root
	for _, rootRegion := range rootRegions {
		if err := createSpans(detailedCtx, rootRegion, idToChildren); err != nil {
			level.Error(logger).Log("msg", "failed to create spans for root region", "id", rootRegion.id.String(), "err", err)
			continue
		}
	}

	// End the detailed root span
	detailedRootSpan.End(trace.WithTimestamp(endTime))

	// Step 2: Create a summary span in the global trace with a link to the detailed trace
	detailedSpanCtx := detailedRootSpan.SpanContext()
	summaryOpts := []trace.SpanStartOption{
		trace.WithTimestamp(startTime),
		trace.WithLinks(trace.Link{
			SpanContext: detailedSpanCtx,
			Attributes: []attribute.KeyValue{
				attribute.String("link.type", "detailed_trace"),
				attribute.String("link.description", "Link to detailed xcap trace"),
			},
		}),
	}

	// Add summary statistics as attributes
	summaryObs := summarizeObservations(capture)
	if summaryObs != nil {
		for key, obs := range summaryObs.data {
			summaryOpts = append(summaryOpts, trace.WithAttributes(observationToAttribute(key, obs)))
		}
	}

	_, summarySpan := tracer.Start(ctx, "xcap", summaryOpts...)
	summarySpan.End(trace.WithTimestamp(endTime))
}

// exportNestedTrace exports xcap spans directly under the parent context.
// This results in all xcap spans being nested within the global trace hierarchy.
func exportNestedTrace(ctx context.Context, rootRegions []*Region, idToChildren map[identifier][]*Region, logger log.Logger) {
	for _, rootRegion := range rootRegions {
		if err := createSpans(ctx, rootRegion, idToChildren); err != nil {
			level.Error(logger).Log("msg", "failed to create spans for root region", "id", rootRegion.id.String(), "err", err)
			continue
		}
	}
}

// calculateCaptureBounds returns the earliest start time and latest end time
// across all regions in the capture.
func calculateCaptureBounds(regions []*Region) (start, end time.Time) {
	if len(regions) == 0 {
		now := time.Now()
		return now, now
	}

	start = regions[0].startTime
	end = regions[0].endTime

	for _, r := range regions[1:] {
		if r.startTime.Before(start) {
			start = r.startTime
		}
		if r.endTime.After(end) {
			end = r.endTime
		}
	}

	// Handle case where end time is zero (region not ended)
	if end.IsZero() {
		end = time.Now()
	}

	return start, end
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

	// range aggregation stats
	result.merge(
		collect.fromRegions("RangeAggregation", false).
			filter(
				StatPipelineReadDuration.Key(),
				StatPipelineExecDuration.Key(),
			).
			prefix("range_aggregation_").
			normalizeKeys(),
	)

	// vector aggregation stats
	result.merge(
		collect.fromRegions("VectorAggregation", false).
			filter(
				StatPipelineReadDuration.Key(),
				StatPipelineExecDuration.Key(),
			).
			prefix("vector_aggregation_").
			normalizeKeys(),
	)

	// metastore index and resolved section stats
	result.merge(
		collect.fromRegions("ObjectMetastore.Sections", true).
			filter(StatMetastoreIndexObjects.Key(), StatMetastoreSectionsResolved.Key()).
			normalizeKeys(),
	)

	// metastore streams and pointers scan stats
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

	// metastore dataset reader and task stats
	result.merge(
		collect.fromRegions("ObjectMetastore.Sections", true).
			filter(
				StatBucketGet.Key(), StatBucketGetRange.Key(), StatBucketAttributes.Key(),
				StatDatasetPrimaryPagesDownloaded.Key(), StatDatasetSecondaryPagesDownloaded.Key(),
				StatDatasetPrimaryColumnBytes.Key(), StatDatasetSecondaryColumnBytes.Key(),
				// physical planning task information
				StatTaskCount.Key(),
				StatTaskAdmissionWaitDuration.Key(), StatTaskAssignmentTailDuration.Key(),
				StatTaskMaxQueueDuration.Key(),
				// task send/recv durations
				TaskRecvDuration.Key(),
				TaskSendDuration.Key(),
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

	// task scheduling and recv/send stats
	// exclude `ObjectMetastore.Sections` to only get execution specific stats.
	result.merge(
		collect.fromRegions("Engine.Execute", true, "ObjectMetastore.Sections").
			filter(
				StatTaskCount.Key(),
				StatTaskAdmissionWaitDuration.Key(), StatTaskAssignmentTailDuration.Key(),
				StatTaskMaxQueueDuration.Key(),
				TaskRecvDuration.Key(), TaskSendDuration.Key(),
			).
			normalizeKeys(),
	)

	return result
}
