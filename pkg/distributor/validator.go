package distributor

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"
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
	maxLineSize           int

	maxLabelNamesPerSeries int
	maxLabelNameLength     int
	maxLabelValueLength    int

	userID string
}

func (v Validator) getValidationContextFor(userID string) validationContext {
	now := time.Now()
	return validationContext{
		userID:                 userID,
		rejectOldSample:        v.RejectOldSamples(userID),
		rejectOldSampleMaxAge:  now.Add(-v.RejectOldSamplesMaxAge(userID)).UnixNano(),
		creationGracePeriod:    now.Add(v.CreationGracePeriod(userID)).UnixNano(),
		maxLineSize:            v.MaxLineSize(userID),
		maxLabelNamesPerSeries: v.MaxLabelNamesPerSeries(userID),
		maxLabelNameLength:     v.MaxLabelNameLength(userID),
		maxLabelValueLength:    v.MaxLabelValueLength(userID),
	}
}

// ValidateEntry returns an error if the entry is invalid
func (v Validator) ValidateEntry(ctx validationContext, labels string, entry logproto.Entry) error {
	ts := entry.Timestamp.UnixNano()
	if ctx.rejectOldSample && ts < ctx.rejectOldSampleMaxAge {
		validation.DiscardedSamples.WithLabelValues(validation.GreaterThanMaxSampleAge, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.GreaterThanMaxSampleAge, ctx.userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.GreaterThanMaxSampleAgeErrorMsg(labels, entry.Timestamp))
	}

	if ts > ctx.creationGracePeriod {
		validation.DiscardedSamples.WithLabelValues(validation.TooFarInFuture, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.TooFarInFuture, ctx.userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.TooFarInFutureErrorMsg(labels, entry.Timestamp))
	}

	if maxSize := ctx.maxLineSize; maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		validation.DiscardedSamples.WithLabelValues(validation.LineTooLong, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.LineTooLong, ctx.userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg(maxSize, len(entry.Line), labels))
	}

	return nil
}

// Validate labels returns an error if the labels are invalid
func (v Validator) ValidateLabels(ctx validationContext, ls labels.Labels, stream logproto.Stream) error {
	numLabelNames := len(ls)
	if numLabelNames > ctx.maxLabelNamesPerSeries {
		validation.DiscardedSamples.WithLabelValues(validation.MaxLabelNamesPerSeries, ctx.userID).Inc()
		bytes := 0
		for _, e := range stream.Entries {
			bytes += len(e.Line)
		}
		validation.DiscardedBytes.WithLabelValues(validation.MaxLabelNamesPerSeries, ctx.userID).Add(float64(bytes))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.MaxLabelNamesPerSeriesErrorMsg(stream.Labels, numLabelNames, ctx.maxLabelNamesPerSeries))
	}

	lastLabelName := ""
	for _, l := range ls {
		if len(l.Name) > ctx.maxLabelNameLength {
			updateMetrics(validation.LabelNameTooLong, ctx.userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.LabelNameTooLongErrorMsg(stream.Labels, l.Name))
		} else if len(l.Value) > ctx.maxLabelValueLength {
			updateMetrics(validation.LabelValueTooLong, ctx.userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.LabelValueTooLongErrorMsg(stream.Labels, l.Value))
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp == 0 {
			updateMetrics(validation.DuplicateLabelNames, ctx.userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.DuplicateLabelNamesErrorMsg(stream.Labels, l.Name))
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
