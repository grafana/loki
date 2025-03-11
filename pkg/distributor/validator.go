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
	"github.com/grafana/loki/v3/pkg/util"
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
	discoverGenericFields        map[string][]string
	discoverLogLevels            bool
	logLevelFields               []string
	logLevelFromJSONMaxDepth     int

	allowStructuredMetadata    bool
	maxStructuredMetadataSize  int
	maxStructuredMetadataCount int

	blockIngestionUntil      time.Time
	blockIngestionStatusCode int
	enforcedLabels           []string

	userID string

	validationMetrics validationMetrics
}

func (v Validator) getValidationContextForTime(now time.Time, userID string) validationContext {
	retentionHours := util.RetentionHours(v.RetentionPeriod(userID))

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
		logLevelFields:               v.LogLevelFields(userID),
		logLevelFromJSONMaxDepth:     v.LogLevelFromJSONMaxDepth(userID),
		discoverGenericFields:        v.DiscoverGenericFields(userID),
		allowStructuredMetadata:      v.AllowStructuredMetadata(userID),
		maxStructuredMetadataSize:    v.MaxStructuredMetadataSize(userID),
		maxStructuredMetadataCount:   v.MaxStructuredMetadataCount(userID),
		blockIngestionUntil:          v.BlockIngestionUntil(userID),
		blockIngestionStatusCode:     v.BlockIngestionStatusCode(userID),
		enforcedLabels:               v.EnforcedLabels(userID),
		validationMetrics:            newValidationMetrics(retentionHours),
	}
}

// ValidateEntry returns an error if the entry is invalid and report metrics for invalid entries accordingly.
func (v Validator) ValidateEntry(ctx context.Context, vCtx validationContext, labels labels.Labels, entry logproto.Entry, retentionHours string, policy string) error {
	ts := entry.Timestamp.UnixNano()
	validation.LineLengthHist.Observe(float64(len(entry.Line)))
	structuredMetadataCount := len(entry.StructuredMetadata)
	structuredMetadataSizeBytes := util.StructuredMetadataSize(entry.StructuredMetadata)
	entrySize := float64(len(entry.Line) + structuredMetadataSizeBytes)

	if vCtx.rejectOldSample && ts < vCtx.rejectOldSampleMaxAge {
		// Makes time string on the error message formatted consistently.
		formatedEntryTime := entry.Timestamp.Format(timeFormat)
		formatedRejectMaxAgeTime := time.Unix(0, vCtx.rejectOldSampleMaxAge).Format(timeFormat)
		v.reportDiscardedDataWithTracker(ctx, validation.GreaterThanMaxSampleAge, vCtx, labels, retentionHours, policy, int(entrySize), 1)
		return fmt.Errorf(validation.GreaterThanMaxSampleAgeErrorMsg, labels, formatedEntryTime, formatedRejectMaxAgeTime)
	}

	if ts > vCtx.creationGracePeriod {
		formatedEntryTime := entry.Timestamp.Format(timeFormat)
		v.reportDiscardedDataWithTracker(ctx, validation.TooFarInFuture, vCtx, labels, retentionHours, policy, int(entrySize), 1)
		return fmt.Errorf(validation.TooFarInFutureErrorMsg, labels, formatedEntryTime)
	}

	if maxSize := vCtx.maxLineSize; maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		v.reportDiscardedDataWithTracker(ctx, validation.LineTooLong, vCtx, labels, retentionHours, policy, int(entrySize), 1)
		return fmt.Errorf(validation.LineTooLongErrorMsg, maxSize, labels, len(entry.Line))
	}

	if structuredMetadataCount > 0 {
		if !vCtx.allowStructuredMetadata {
			v.reportDiscardedDataWithTracker(ctx, validation.DisallowedStructuredMetadata, vCtx, labels, retentionHours, policy, int(entrySize), 1)
			return fmt.Errorf(validation.DisallowedStructuredMetadataErrorMsg, labels)
		}

		if maxSize := vCtx.maxStructuredMetadataSize; maxSize != 0 && structuredMetadataSizeBytes > maxSize {
			v.reportDiscardedDataWithTracker(ctx, validation.StructuredMetadataTooLarge, vCtx, labels, retentionHours, policy, int(entrySize), 1)
			return fmt.Errorf(validation.StructuredMetadataTooLargeErrorMsg, labels, structuredMetadataSizeBytes, vCtx.maxStructuredMetadataSize)
		}

		if maxCount := vCtx.maxStructuredMetadataCount; maxCount != 0 && structuredMetadataCount > maxCount {
			v.reportDiscardedDataWithTracker(ctx, validation.StructuredMetadataTooMany, vCtx, labels, retentionHours, policy, int(entrySize), 1)
			return fmt.Errorf(validation.StructuredMetadataTooManyErrorMsg, labels, structuredMetadataCount, vCtx.maxStructuredMetadataCount)
		}
	}

	return nil
}

func (v Validator) IsAggregatedMetricStream(ls labels.Labels) bool {
	return ls.Has(push.AggregatedMetricLabel)
}

// Validate labels returns an error if the labels are invalid and if the stream is an aggregated metric stream
func (v Validator) ValidateLabels(vCtx validationContext, ls labels.Labels, stream logproto.Stream, retentionHours, policy string) error {
	if len(ls) == 0 {
		// TODO: is this one correct?
		validation.DiscardedSamples.WithLabelValues(validation.MissingLabels, vCtx.userID, retentionHours, policy).Inc()
		return fmt.Errorf(validation.MissingLabelsErrorMsg)
	}

	// Skip validation for aggregated metric streams, as we create those for internal use
	if v.IsAggregatedMetricStream(ls) {
		return nil
	}

	numLabelNames := len(ls)
	// This is a special case that's often added by the Loki infrastructure. It may result in allowing one extra label
	// if incoming requests already have a service_name
	if ls.Has(push.LabelServiceName) {
		numLabelNames--
	}

	entriesSize := util.EntriesTotalSize(stream.Entries)

	if numLabelNames > vCtx.maxLabelNamesPerSeries {
		v.reportDiscardedData(validation.MaxLabelNamesPerSeries, vCtx, retentionHours, policy, entriesSize, len(stream.Entries))
		return fmt.Errorf(validation.MaxLabelNamesPerSeriesErrorMsg, stream.Labels, numLabelNames, vCtx.maxLabelNamesPerSeries)
	}

	lastLabelName := ""
	for _, l := range ls {
		if len(l.Name) > vCtx.maxLabelNameLength {
			v.reportDiscardedData(validation.LabelNameTooLong, vCtx, retentionHours, policy, entriesSize, len(stream.Entries))
			return fmt.Errorf(validation.LabelNameTooLongErrorMsg, stream.Labels, l.Name)
		} else if len(l.Value) > vCtx.maxLabelValueLength {
			v.reportDiscardedData(validation.LabelValueTooLong, vCtx, retentionHours, policy, entriesSize, len(stream.Entries))
			return fmt.Errorf(validation.LabelValueTooLongErrorMsg, stream.Labels, l.Value)
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp == 0 {
			v.reportDiscardedData(validation.DuplicateLabelNames, vCtx, retentionHours, policy, entriesSize, len(stream.Entries))
			return fmt.Errorf(validation.DuplicateLabelNamesErrorMsg, stream.Labels, l.Name)
		}
		lastLabelName = l.Name
	}
	return nil
}

func (v Validator) reportDiscardedData(reason string, vCtx validationContext, retentionHours string, policy string, entrySize, entryCount int) {
	validation.DiscardedSamples.WithLabelValues(reason, vCtx.userID, retentionHours, policy).Add(float64(entryCount))
	validation.DiscardedBytes.WithLabelValues(reason, vCtx.userID, retentionHours, policy).Add(float64(entrySize))
}

func (v Validator) reportDiscardedDataWithTracker(ctx context.Context, reason string, vCtx validationContext, labels labels.Labels, retentionHours string, policy string, entrySize, entryCount int) {
	v.reportDiscardedData(reason, vCtx, retentionHours, policy, entrySize, entryCount)
	if v.usageTracker != nil {
		v.usageTracker.DiscardedBytesAdd(ctx, vCtx.userID, reason, labels, float64(entrySize))
	}
}

// ShouldBlockIngestion returns whether ingestion should be blocked, until when and the status code.
// priority is: Per-tenant block > named policy block > Global policy block
func (v Validator) ShouldBlockIngestion(ctx validationContext, now time.Time, policy string) (bool, int, string, error) {
	if block, until, code := v.shouldBlockTenant(ctx, now); block {
		err := fmt.Errorf(validation.BlockedIngestionErrorMsg, ctx.userID, until.Format(time.RFC3339), code)
		return true, code, validation.BlockedIngestion, err
	}

	if block, until, code := v.shouldBlockPolicy(ctx, policy, now); block {
		err := fmt.Errorf(validation.BlockedIngestionPolicyErrorMsg, ctx.userID, until.Format(time.RFC3339), code)
		return true, code, validation.BlockedIngestionPolicy, err
	}

	return false, 0, "", nil
}

func (v Validator) shouldBlockTenant(ctx validationContext, now time.Time) (bool, time.Time, int) {
	if ctx.blockIngestionUntil.IsZero() {
		return false, time.Time{}, 0
	}

	if now.Before(ctx.blockIngestionUntil) {
		return true, ctx.blockIngestionUntil, ctx.blockIngestionStatusCode
	}

	return false, time.Time{}, 0
}

// ShouldBlockPolicy checks if ingestion should be blocked for the given policy.
// It returns true if ingestion should be blocked, along with the block until time and status code.
func (v Validator) shouldBlockPolicy(ctx validationContext, policy string, now time.Time) (bool, time.Time, int) {
	// Check if this policy is blocked in tenant configs
	blockUntil := v.Limits.BlockIngestionPolicyUntil(ctx.userID, policy)
	if blockUntil.IsZero() {
		return false, time.Time{}, 0
	}

	if now.Before(blockUntil) {
		return true, blockUntil, ctx.blockIngestionStatusCode
	}

	return false, time.Time{}, 0
}
