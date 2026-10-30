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

// HTTPHeaderTrustedLabelsKey is the HTTP header an authenticating gateway sets to force labels onto
// every stream of a push request, for example `{username="userA"}`. A trusted label overrides a label
// with the same name in the request body, or is added if the body doesn't have it. All other labels
// are left untouched, so the gateway doesn't have to rewrite request bodies.
//
// Loki only honours the header for tenants that enable it. Like X-Scope-OrgID, the header is trusted
// as-is: the gateway in front of Loki must strip any value sent by the client.
const HTTPHeaderTrustedLabelsKey = "X-Loki-Labels"

// maxTrustedLabelsLen bounds the header value so a misbehaving gateway can't make Loki parse
// arbitrarily large label sets on every push.
const maxTrustedLabelsLen = 4096

// trustedLabelsContextKey is used as a key for context values to avoid collisions.
type trustedLabelsContextKey int

const trustedLabelsKey trustedLabelsContextKey = 1

// ExtractAndValidateTrustedLabels reads the X-Loki-Labels header from r.
// It returns (empty, false, nil) when the header is absent or empty. When the header is set it must
// be a non-empty Prometheus label set with valid, non-empty values and no reserved (`__`-prefixed)
// label names; otherwise it returns an error so the push is rejected with HTTP 400.
func ExtractAndValidateTrustedLabels(r *http.Request) (labels.Labels, bool, error) {
	value := r.Header.Get(HTTPHeaderTrustedLabelsKey)
	if value == "" {
		return labels.EmptyLabels(), false, nil
	}
	if len(value) > maxTrustedLabelsLen {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: value length %d exceeds limit of %d", HTTPHeaderTrustedLabelsKey, len(value), maxTrustedLabelsLen)
	}

	lbs, err := syntax.ParseMetric(value)
	if err != nil {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header %q: must be a label set such as {name=\"value\"}: %w", HTTPHeaderTrustedLabelsKey, value, err)
	}
	if lbs.IsEmpty() {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header %q: must set at least one label", HTTPHeaderTrustedLabelsKey, value)
	}
	if name, dup := lbs.HasDuplicateLabelNames(); dup {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header %q: duplicate label name %q", HTTPHeaderTrustedLabelsKey, value, name)
	}

	err = lbs.Validate(func(l labels.Label) error {
		switch {
		case strings.HasPrefix(l.Name, "__"):
			return fmt.Errorf("invalid %s header %q: label name %q is reserved", HTTPHeaderTrustedLabelsKey, value, l.Name)
		case l.Value == "":
			return fmt.Errorf("invalid %s header %q: label %q must have a non-empty value", HTTPHeaderTrustedLabelsKey, value, l.Name)
		case !model.LabelValue(l.Value).IsValid():
			return fmt.Errorf("invalid %s header %q: label %q must have a valid label value", HTTPHeaderTrustedLabelsKey, value, l.Name)
		}
		return nil
	})
	if err != nil {
		return labels.EmptyLabels(), false, err
	}
	return lbs, true, nil
}

// InjectTrustedLabelsContext returns a derived context carrying the validated trusted labels.
func InjectTrustedLabelsContext(ctx context.Context, lbs labels.Labels) context.Context {
	return context.WithValue(ctx, trustedLabelsKey, lbs)
}

// ExtractTrustedLabelsContext returns the trusted labels stored in ctx, or empty labels if none are set.
func ExtractTrustedLabelsContext(ctx context.Context) labels.Labels {
	lbs, ok := ctx.Value(trustedLabelsKey).(labels.Labels)
	if !ok {
		return labels.EmptyLabels()
	}
	return lbs
}

// applyTrustedLabels returns lbs with every trusted label set, overriding labels with the same name.
func applyTrustedLabels(lbs, trusted labels.Labels) labels.Labels {
	if trusted.IsEmpty() {
		return lbs
	}
	b := labels.NewBuilder(lbs)
	trusted.Range(func(l labels.Label) {
		b.Set(l.Name, l.Value)
	})
	return b.Labels()
}

// applyTrustedLabelSet sets every trusted label on ls in place, overriding labels with the same name.
func applyTrustedLabelSet(ls model.LabelSet, trusted labels.Labels) {
	trusted.Range(func(l labels.Label) {
		ls[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	})
}
