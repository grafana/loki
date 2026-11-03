package push

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// HTTPHeaderForcedLabelsKey is the HTTP header an authenticating proxy in front of Loki can set to
// force labels onto every stream of a push request, so that clients cannot spoof them. The value is
// a label set in LogQL selector syntax, for example {user="alice", site="edge-07"}. Each label is set
// on every stream, overwriting a stream label with the same name.
//
// The header is only read when the forced_labels_header_enabled limit is enabled for the tenant.
// The proxy must then set the header and strip any value sent by clients.
const HTTPHeaderForcedLabelsKey = "X-Loki-Forced-Labels"

const (
	// maxForcedLabelsHeaderLen bounds the header value.
	maxForcedLabelsHeaderLen = 1024
	// maxForcedLabels bounds the number of labels in the header.
	maxForcedLabels = 10
)

// forcedLabelsContextKey is used as a key for context values to avoid collisions.
type forcedLabelsContextKey int

const forcedLabelsKey forcedLabelsContextKey = 1

// ExtractAndValidateForcedLabels reads the X-Loki-Forced-Labels header from r.
// It returns (empty, false, nil) when the header is absent or empty. When the header is set it must
// be a single, bounded label set of valid, non-reserved label names and non-empty label values;
// otherwise it returns an error so the push is rejected with HTTP 400.
func ExtractAndValidateForcedLabels(r *http.Request) (labels.Labels, bool, error) {
	values := r.Header.Values(HTTPHeaderForcedLabelsKey)
	if len(values) > 1 {
		// A proxy that adds the header instead of replacing it would otherwise let a client
		// value through, so the header is rejected rather than picking one of the values.
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: set more than once", HTTPHeaderForcedLabelsKey)
	}
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return labels.EmptyLabels(), false, nil
	}
	value := values[0]

	if len(value) > maxForcedLabelsHeaderLen {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: value length %d exceeds limit of %d", HTTPHeaderForcedLabelsKey, len(value), maxForcedLabelsHeaderLen)
	}

	// ParseMetric, unlike ParseLabels, keeps empty values, so they are rejected below instead of
	// being dropped silently. It also rejects values that are not valid UTF-8.
	lbs, err := syntax.ParseMetric(value)
	if err != nil {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: must be a label set such as {user=\"alice\"}: %w", HTTPHeaderForcedLabelsKey, err)
	}
	if lbs.IsEmpty() {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: must contain at least one label", HTTPHeaderForcedLabelsKey)
	}
	if lbs.Len() > maxForcedLabels {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: %d labels exceeds limit of %d", HTTPHeaderForcedLabelsKey, lbs.Len(), maxForcedLabels)
	}
	if name, dup := lbs.HasDuplicateLabelNames(); dup {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: duplicate label name %q", HTTPHeaderForcedLabelsKey, name)
	}

	err = lbs.Validate(func(l labels.Label) error {
		switch {
		case strings.HasPrefix(l.Name, "__"):
			// Names starting with __ are reserved for Loki, such as the backfill labels.
			return fmt.Errorf("invalid %s header: reserved label name %q", HTTPHeaderForcedLabelsKey, l.Name)
		case !model.LegacyValidation.IsValidLabelName(l.Name):
			return fmt.Errorf("invalid %s header: invalid label name %q", HTTPHeaderForcedLabelsKey, l.Name)
		case l.Value == "":
			return fmt.Errorf("invalid %s header: label %q has an empty value", HTTPHeaderForcedLabelsKey, l.Name)
		}
		return nil
	})
	if err != nil {
		return labels.EmptyLabels(), false, err
	}
	return lbs, true, nil
}

// InjectForcedLabelsContext returns a derived context carrying the validated forced labels.
func InjectForcedLabelsContext(ctx context.Context, lbs labels.Labels) context.Context {
	return context.WithValue(ctx, forcedLabelsKey, lbs)
}

// ExtractForcedLabelsContext returns the forced labels stored in ctx, or empty labels if none are set.
func ExtractForcedLabelsContext(ctx context.Context) labels.Labels {
	lbs, ok := ctx.Value(forcedLabelsKey).(labels.Labels)
	if !ok {
		return labels.EmptyLabels()
	}
	return lbs
}

// setForcedLabels sets the forced labels on lbs, overwriting labels with the same name.
func setForcedLabels(lbs, forced labels.Labels) labels.Labels {
	if forced.IsEmpty() {
		return lbs
	}
	b := labels.NewBuilder(lbs)
	forced.Range(func(l labels.Label) {
		b.Set(l.Name, l.Value)
	})
	return b.Labels()
}

// setForcedLabelsOnLabelSet sets the forced labels on ls, overwriting labels with the same name.
func setForcedLabelsOnLabelSet(ls model.LabelSet, forced labels.Labels) {
	forced.Range(func(l labels.Label) {
		ls[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	})
}
