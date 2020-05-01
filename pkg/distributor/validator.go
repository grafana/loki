package distributor

import (
	"errors"
	"net/http"
	"strings"
	"time"

	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
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

// ValidateEntry returns an error if the entry is invalid
func (v Validator) ValidateEntry(userID string, labels string, entry logproto.Entry) error {
	if v.RejectOldSamples(userID) && entry.Timestamp.UnixNano() < time.Now().Add(-v.RejectOldSamplesMaxAge(userID)).UnixNano() {
		validation.DiscardedSamples.WithLabelValues(validation.GreaterThanMaxSampleAge, userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.GreaterThanMaxSampleAge, userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.GreaterThanMaxSampleAgeErrorMsg(labels, entry.Timestamp))
	}

	if entry.Timestamp.UnixNano() > time.Now().Add(v.CreationGracePeriod(userID)).UnixNano() {
		validation.DiscardedSamples.WithLabelValues(validation.TooFarInFuture, userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.TooFarInFuture, userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.TooFarInFutureErrorMsg(labels, entry.Timestamp))
	}

	if maxSize := v.MaxLineSize(userID); maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		validation.DiscardedSamples.WithLabelValues(validation.LineTooLong, userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.LineTooLong, userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg(maxSize, len(entry.Line), labels))
	}

	return nil
}

// Validate labels returns an error if the labels are invalid
func (v Validator) ValidateLabels(userID string, stream logproto.Stream) error {
	ls, err := util.ToClientLabels(stream.Labels)
	if err != nil {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		return httpgrpc.Errorf(http.StatusBadRequest, "error parsing labels: %v", err)
	}

	numLabelNames := len(ls)
	if numLabelNames > v.MaxLabelNamesPerSeries(userID) {
		validation.DiscardedSamples.WithLabelValues(validation.MaxLabelNamesPerSeries, userID).Inc()
		bytes := 0
		for _, e := range stream.Entries {
			bytes += len(e.Line)
		}
		validation.DiscardedBytes.WithLabelValues(validation.MaxLabelNamesPerSeries, userID).Add(float64(bytes))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.MaxLabelNamesPerSeriesErrorMsg(cortex_client.FromLabelAdaptersToMetric(ls).String(), numLabelNames, v.MaxLabelNamesPerSeries(userID)))
	}

	maxLabelNameLength := v.MaxLabelNameLength(userID)
	maxLabelValueLength := v.MaxLabelValueLength(userID)
	lastLabelName := ""
	for _, l := range ls {
		if len(l.Name) > maxLabelNameLength {
			updateMetrics(validation.LabelNameTooLong, userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.LabelNameTooLongErrorMsg(stream.Labels, l.Name))
		} else if len(l.Value) > maxLabelValueLength {
			updateMetrics(validation.LabelValueTooLong, userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.LabelValueTooLongErrorMsg(stream.Labels, l.Value))
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp == 0 {
			updateMetrics(validation.DuplicateLabelNames, userID, stream)
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
