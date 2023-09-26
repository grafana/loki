package distributor

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/validation"
)

const (
	timeFormat = time.RFC3339
)

type Validator struct {
	Limits
}

func NewValidator(l Limits) (*Validator, error) {
	if l == nil {
		return nil, errors.New("nil Limits")
	}
	return &Validator{l}, nil
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

	allowStructuredMetadata    bool
	maxStructuredMetadataSize  int
	maxStructuredMetadataCount int

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
		allowStructuredMetadata:      v.AllowStructuredMetadata(userID),
		maxStructuredMetadataSize:    v.MaxStructuredMetadataSize(userID),
		maxStructuredMetadataCount:   v.MaxStructuredMetadataCount(userID),
	}
}

// ValidateEntry returns an error if the entry is invalid and report metrics for invalid entries accordingly.
func (v Validator) ValidateEntry(ctx validationContext, labels string, entry logproto.Entry) error {
	ts := entry.Timestamp.UnixNano()
	validation.LineLengthHist.Observe(float64(len(entry.Line)))

	if ctx.rejectOldSample && ts < ctx.rejectOldSampleMaxAge {
		// Makes time string on the error message formatted consistently.
		formatedEntryTime := entry.Timestamp.Format(timeFormat)
		formatedRejectMaxAgeTime := time.Unix(0, ctx.rejectOldSampleMaxAge).Format(timeFormat)
		validation.DiscardedSamples.WithLabelValues(validation.GreaterThanMaxSampleAge, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.GreaterThanMaxSampleAge, ctx.userID).Add(float64(len(entry.Line)))
		return fmt.Errorf(validation.GreaterThanMaxSampleAgeErrorMsg, labels, formatedEntryTime, formatedRejectMaxAgeTime)
	}

	if ts > ctx.creationGracePeriod {
		formatedEntryTime := entry.Timestamp.Format(timeFormat)
		validation.DiscardedSamples.WithLabelValues(validation.TooFarInFuture, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.TooFarInFuture, ctx.userID).Add(float64(len(entry.Line)))
		return fmt.Errorf(validation.TooFarInFutureErrorMsg, labels, formatedEntryTime)
	}

	if maxSize := ctx.maxLineSize; maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		validation.DiscardedSamples.WithLabelValues(validation.LineTooLong, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.LineTooLong, ctx.userID).Add(float64(len(entry.Line)))
		return fmt.Errorf(validation.LineTooLongErrorMsg, maxSize, labels, len(entry.Line))
	}

	if len(entry.StructuredMetadata) > 0 {
		if !ctx.allowStructuredMetadata {
			validation.DiscardedSamples.WithLabelValues(validation.DisallowedStructuredMetadata, ctx.userID).Inc()
			validation.DiscardedBytes.WithLabelValues(validation.DisallowedStructuredMetadata, ctx.userID).Add(float64(len(entry.Line)))
			return fmt.Errorf(validation.DisallowedStructuredMetadataErrorMsg, labels)
		}

		var structuredMetadataSizeBytes, structuredMetadataCount int
		for _, metadata := range entry.StructuredMetadata {
			structuredMetadataSizeBytes += len(metadata.Name) + len(metadata.Value)
			structuredMetadataCount++
		}

		if maxSize := ctx.maxStructuredMetadataSize; maxSize != 0 && structuredMetadataSizeBytes > maxSize {
			validation.DiscardedSamples.WithLabelValues(validation.StructuredMetadataTooLarge, ctx.userID).Inc()
			validation.DiscardedBytes.WithLabelValues(validation.StructuredMetadataTooLarge, ctx.userID).Add(float64(len(entry.Line)))
			return fmt.Errorf(validation.StructuredMetadataTooLargeErrorMsg, labels, structuredMetadataSizeBytes, ctx.maxStructuredMetadataSize)
		}

		if maxCount := ctx.maxStructuredMetadataCount; maxCount != 0 && structuredMetadataCount > maxCount {
			validation.DiscardedSamples.WithLabelValues(validation.StructuredMetadataTooMany, ctx.userID).Inc()
			validation.DiscardedBytes.WithLabelValues(validation.StructuredMetadataTooMany, ctx.userID).Add(float64(len(entry.Line)))
			return fmt.Errorf(validation.StructuredMetadataTooManyErrorMsg, labels, structuredMetadataCount, ctx.maxStructuredMetadataCount)
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
	numLabelNames := len(ls)
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

func updateMetrics(reason, userID string, stream logproto.Stream) {
	validation.DiscardedSamples.WithLabelValues(reason, userID).Inc()
	bytes := 0
	for _, e := range stream.Entries {
		bytes += len(e.Line)
	}
	validation.DiscardedBytes.WithLabelValues(reason, userID).Add(float64(bytes))
}
