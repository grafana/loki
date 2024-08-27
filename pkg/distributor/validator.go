package distributor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	timeFormat = time.RFC3339
)

type Validator struct {
	Limits
	usageTracker push.UsageTracker
}

func NewValidator(l Limits, t push.UsageTracker) (*Validator, error) {
	if l == nil {
		return nil, errors.New("nil Limits")
	}
	return &Validator{l, t}, nil
}

type validationContext struct {
	rejectOldSample       bool
	rejectOldSampleMaxAge int64
	creationGracePeriod   int64

	maxLineSize         int
	maxLineSizeTruncate bool

	maxLabelNamesPerSeries int
	maxLabelNameLength     int
	maxLabelValueLength    int

	incrementDuplicateTimestamps bool
	discoverServiceName          []string
	discoverLogLevels            bool

	allowStructuredMetadata    bool
	maxStructuredMetadataSize  int
	maxStructuredMetadataCount int

	blockIngestionUntil      time.Time
	blockIngestionStatusCode int

	userID string
}

func (v Validator) getValidationContextForTime(now time.Time, userID string) validationContext {
	return validationContext{
		userID:                       userID,
		rejectOldSample:              v.RejectOldSamples(userID),
		rejectOldSampleMaxAge:        now.Add(-v.RejectOldSamplesMaxAge(userID)).UnixNano(),
		creationGracePeriod:          now.Add(v.CreationGracePeriod(userID)).UnixNano(),
		maxLineSize:                  v.MaxLineSize(userID),
		maxLineSizeTruncate:          v.MaxLineSizeTruncate(userID),
		maxLabelNamesPerSeries:       v.MaxLabelNamesPerSeries(userID),
		maxLabelNameLength:           v.MaxLabelNameLength(userID),
		maxLabelValueLength:          v.MaxLabelValueLength(userID),
		incrementDuplicateTimestamps: v.IncrementDuplicateTimestamps(userID),
		discoverServiceName:          v.DiscoverServiceName(userID),
		discoverLogLevels:            v.DiscoverLogLevels(userID),
		allowStructuredMetadata:      v.AllowStructuredMetadata(userID),
		maxStructuredMetadataSize:    v.MaxStructuredMetadataSize(userID),
		maxStructuredMetadataCount:   v.MaxStructuredMetadataCount(userID),
		blockIngestionUntil:          v.BlockIngestionUntil(userID),
		blockIngestionStatusCode:     v.BlockIngestionStatusCode(userID),
	}
}

// ValidateEntry returns an error if the entry is invalid and report metrics for invalid entries accordingly.
func (v Validator) ValidateEntry(ctx context.Context, vCtx validationContext, labels labels.Labels, entry logproto.Entry) error {
	ts := entry.Timestamp.UnixNano()
	validation.LineLengthHist.Observe(float64(len(entry.Line)))

	if vCtx.rejectOldSample && ts < vCtx.rejectOldSampleMaxAge {
		// Makes time string on the error message formatted consistently.
		formatedEntryTime := entry.Timestamp.Format(timeFormat)
		formatedRejectMaxAgeTime := time.Unix(0, vCtx.rejectOldSampleMaxAge).Format(timeFormat)
		validation.DiscardedSamples.WithLabelValues(validation.GreaterThanMaxSampleAge, vCtx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.GreaterThanMaxSampleAge, vCtx.userID).Add(float64(len(entry.Line)))
		if v.usageTracker != nil {
			v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, validation.GreaterThanMaxSampleAge, labels, float64(len(entry.Line)))
		}
		return fmt.Errorf(validation.GreaterThanMaxSampleAgeErrorMsg, labels, formatedEntryTime, formatedRejectMaxAgeTime)
	}

	if ts > vCtx.creationGracePeriod {
		formatedEntryTime := entry.Timestamp.Format(timeFormat)
		validation.DiscardedSamples.WithLabelValues(validation.TooFarInFuture, vCtx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.TooFarInFuture, vCtx.userID).Add(float64(len(entry.Line)))
		if v.usageTracker != nil {
			v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, validation.TooFarInFuture, labels, float64(len(entry.Line)))
		}
		return fmt.Errorf(validation.TooFarInFutureErrorMsg, labels, formatedEntryTime)
	}

	if maxSize := vCtx.maxLineSize; maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		validation.DiscardedSamples.WithLabelValues(validation.LineTooLong, vCtx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.LineTooLong, vCtx.userID).Add(float64(len(entry.Line)))
		if v.usageTracker != nil {
			v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, validation.LineTooLong, labels, float64(len(entry.Line)))
		}
		return fmt.Errorf(validation.LineTooLongErrorMsg, maxSize, labels, len(entry.Line))
	}

	if len(entry.StructuredMetadata) > 0 {
		if !vCtx.allowStructuredMetadata {
			validation.DiscardedSamples.WithLabelValues(validation.DisallowedStructuredMetadata, vCtx.userID).Inc()
			validation.DiscardedBytes.WithLabelValues(validation.DisallowedStructuredMetadata, vCtx.userID).Add(float64(len(entry.Line)))
			if v.usageTracker != nil {
				v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, validation.DisallowedStructuredMetadata, labels, float64(len(entry.Line)))
			}
			return fmt.Errorf(validation.DisallowedStructuredMetadataErrorMsg, labels)
		}

		var structuredMetadataSizeBytes, structuredMetadataCount int
		for _, metadata := range entry.StructuredMetadata {
			structuredMetadataSizeBytes += len(metadata.Name) + len(metadata.Value)
			structuredMetadataCount++
		}

		if maxSize := vCtx.maxStructuredMetadataSize; maxSize != 0 && structuredMetadataSizeBytes > maxSize {
			validation.DiscardedSamples.WithLabelValues(validation.StructuredMetadataTooLarge, vCtx.userID).Inc()
			validation.DiscardedBytes.WithLabelValues(validation.StructuredMetadataTooLarge, vCtx.userID).Add(float64(len(entry.Line)))
			if v.usageTracker != nil {
				v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, validation.StructuredMetadataTooLarge, labels, float64(len(entry.Line)))
			}
			return fmt.Errorf(validation.StructuredMetadataTooLargeErrorMsg, labels, structuredMetadataSizeBytes, vCtx.maxStructuredMetadataSize)
		}

		if maxCount := vCtx.maxStructuredMetadataCount; maxCount != 0 && structuredMetadataCount > maxCount {
			validation.DiscardedSamples.WithLabelValues(validation.StructuredMetadataTooMany, vCtx.userID).Inc()
			validation.DiscardedBytes.WithLabelValues(validation.StructuredMetadataTooMany, vCtx.userID).Add(float64(len(entry.Line)))
			if v.usageTracker != nil {
				v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, validation.StructuredMetadataTooMany, labels, float64(len(entry.Line)))
			}
			return fmt.Errorf(validation.StructuredMetadataTooManyErrorMsg, labels, structuredMetadataCount, vCtx.maxStructuredMetadataCount)
		}
	}

	return nil
}

// Validate labels returns an error if the labels are invalid
func (v Validator) ValidateLabels(ctx validationContext, ls labels.Labels, stream logproto.Stream) error {
	if len(ls) == 0 {
		validation.DiscardedSamples.WithLabelValues(validation.MissingLabels, ctx.userID).Inc()
		return fmt.Errorf(validation.MissingLabelsErrorMsg)
	}

	// Skip validation for aggregated metric streams, as we create those for internal use
	if ls.Has(push.AggregatedMetricLabel) {
		return nil
	}

	numLabelNames := len(ls)
	// This is a special case that's often added by the Loki infrastructure. It may result in allowing one extra label
	// if incoming requests already have a service_name
	if ls.Has(push.LabelServiceName) {
		numLabelNames--
	}

	if numLabelNames > ctx.maxLabelNamesPerSeries {
		updateMetrics(validation.MaxLabelNamesPerSeries, ctx.userID, stream)
		return fmt.Errorf(validation.MaxLabelNamesPerSeriesErrorMsg, stream.Labels, numLabelNames, ctx.maxLabelNamesPerSeries)
	}

	lastLabelName := ""
	for _, l := range ls {
		if len(l.Name) > ctx.maxLabelNameLength {
			updateMetrics(validation.LabelNameTooLong, ctx.userID, stream)
			return fmt.Errorf(validation.LabelNameTooLongErrorMsg, stream.Labels, l.Name)
		} else if len(l.Value) > ctx.maxLabelValueLength {
			updateMetrics(validation.LabelValueTooLong, ctx.userID, stream)
			return fmt.Errorf(validation.LabelValueTooLongErrorMsg, stream.Labels, l.Value)
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp == 0 {
			updateMetrics(validation.DuplicateLabelNames, ctx.userID, stream)
			return fmt.Errorf(validation.DuplicateLabelNamesErrorMsg, stream.Labels, l.Name)
		}
		lastLabelName = l.Name
	}
	return nil
}

// ShouldBlockIngestion returns whether ingestion should be blocked, until when and the status code.
func (v Validator) ShouldBlockIngestion(ctx validationContext, now time.Time) (bool, time.Time, int) {
	if ctx.blockIngestionUntil.IsZero() {
		return false, time.Time{}, 0
	}

	return now.Before(ctx.blockIngestionUntil), ctx.blockIngestionUntil, ctx.blockIngestionStatusCode
}

func updateMetrics(reason, userID string, stream logproto.Stream) {
	validation.DiscardedSamples.WithLabelValues(reason, userID).Inc()
	bytes := 0
	for _, e := range stream.Entries {
		bytes += len(e.Line)
	}
	validation.DiscardedBytes.WithLabelValues(reason, userID).Add(float64(bytes))
}
