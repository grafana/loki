package push

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// HTTPHeaderTrustedLabelsKey is the HTTP header used to force labels onto pushed streams.
	HTTPHeaderTrustedLabelsKey = "X-Loki-Labels"

	// maxTrustedLabelsHeaderSize bounds the size of the forced label set in an HTTP header.
	maxTrustedLabelsHeaderSize = 4 << 10
)

type trustedLabelsContextKey struct{}

// ExtractAndValidateTrustedLabels reads and validates the X-Loki-Labels header.
// It returns no labels when the header is absent. A present header must contain a
// non-empty, valid label set within the size limit and must not contain reserved labels.
func ExtractAndValidateTrustedLabels(r *http.Request) (labels.Labels, bool, error) {
	values := r.Header.Values(HTTPHeaderTrustedLabelsKey)
	if len(values) == 0 {
		return labels.EmptyLabels(), false, nil
	}
	if len(values) > 1 {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: multiple values are not supported", HTTPHeaderTrustedLabelsKey)
	}

	value := values[0]
	if len(value) > maxTrustedLabelsHeaderSize {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: value size %d exceeds limit of %d bytes", HTTPHeaderTrustedLabelsKey, len(value), maxTrustedLabelsHeaderSize)
	}
	if strings.TrimSpace(value) == "" {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: label set must not be empty", HTTPHeaderTrustedLabelsKey)
	}

	forced, err := syntax.ParseLabels(value)
	if err != nil {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: %w", HTTPHeaderTrustedLabelsKey, err)
	}
	if forced.IsEmpty() {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: label set must not be empty", HTTPHeaderTrustedLabelsKey)
	}

	var validationErr error
	forced.Range(func(label labels.Label) {
		if validationErr != nil {
			return
		}
		if !model.UTF8Validation.IsValidLabelName(label.Name) {
			validationErr = fmt.Errorf("invalid %s header: label name %q is invalid", HTTPHeaderTrustedLabelsKey, label.Name)
			return
		}
		if !model.LabelValue(label.Value).IsValid() {
			validationErr = fmt.Errorf("invalid %s header: value for label %q is not valid UTF-8", HTTPHeaderTrustedLabelsKey, label.Name)
			return
		}
		if isReservedTrustedLabel(label.Name) {
			validationErr = fmt.Errorf("invalid %s header: label %q is reserved for Loki", HTTPHeaderTrustedLabelsKey, label.Name)
		}
	})
	if validationErr != nil {
		return labels.EmptyLabels(), false, validationErr
	}

	return forced, true, nil
}

func isReservedTrustedLabel(name string) bool {
	switch name {
	case constants.AggregatedMetricLabel,
		constants.PatternLabel,
		constants.TimeShardLabel,
		constants.StreamShardLabel,
		constants.BackfillLabel,
		constants.BackfillShardLabel:
		return true
	default:
		return false
	}
}

// InjectTrustedLabelsContext returns a derived context carrying validated forced labels.
func InjectTrustedLabelsContext(ctx context.Context, forced labels.Labels) context.Context {
	return context.WithValue(ctx, trustedLabelsContextKey{}, forced)
}

// ExtractTrustedLabelsContext returns the forced labels stored in ctx, or an empty set if none is set.
func ExtractTrustedLabelsContext(ctx context.Context) labels.Labels {
	forced, ok := ctx.Value(trustedLabelsContextKey{}).(labels.Labels)
	if !ok {
		return labels.EmptyLabels()
	}
	return forced
}

// MergeTrustedLabels replaces body labels whose names appear in forced and adds missing forced labels.
func MergeTrustedLabels(body, forced labels.Labels) labels.Labels {
	if forced.IsEmpty() {
		return body
	}

	builder := labels.NewBuilder(body)
	applyTrustedLabels(forced, func(name, value string) {
		builder.Set(name, value)
	})
	return builder.Labels()
}

func applyTrustedLabels(forced labels.Labels, set func(name, value string)) {
	forced.Range(func(label labels.Label) {
		set(label.Name, label.Value)
	})
}
