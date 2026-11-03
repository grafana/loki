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

// HTTPHeaderTrustedLabelsKey is the HTTP header a trusted reverse proxy or gateway sets to force a
// set of labels onto every stream of a push request, for example `{service_namespace="team-a",
// username="userA"}`. A label from the header overrides a body label with the same name, or is added
// when the body does not carry it.
//
// The purpose is to let a gateway attach an authenticated identity (such as a username) to every log
// line without rewriting the request body, so one host or application cannot impersonate another by
// setting the label itself.
//
// The feature is opt-in per tenant (see the distributor Limits.TrustedLabelsHeaderEnabled); when it
// is disabled the header is ignored. Because Loki trusts these labels unconditionally, the gateway
// is responsible for overwriting any client-supplied X-Loki-Labels header rather than appending to
// it.
const HTTPHeaderTrustedLabelsKey = "X-Loki-Labels"

// maxTrustedLabelsLen bounds the raw header so a single malformed value cannot force a large
// allocation before the labels are parsed.
const maxTrustedLabelsLen = 8 * 1024

// trustedLabelsContextKey is used as a key for context values to avoid collisions.
type trustedLabelsContextKey int

const trustedLabelsKey trustedLabelsContextKey = 1

// ExtractAndValidateTrustedLabels reads the X-Loki-Labels header from r.
//
// It returns (labels.EmptyLabels(), false, nil) when the header is absent or empty. When the header
// is set it must be a single, bounded-length Prometheus label set containing only non-reserved
// labels; otherwise it returns an error so the push is rejected with HTTP 400. The error never
// echoes the raw header value, which may carry sensitive identity information.
func ExtractAndValidateTrustedLabels(r *http.Request) (labels.Labels, bool, error) {
	// A repeated header is rejected: a trusted gateway must set exactly one authoritative value, and
	// silently picking one of several would be surprising.
	values := r.Header.Values(HTTPHeaderTrustedLabelsKey)
	if len(values) > 1 {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: expected a single value, got %d", HTTPHeaderTrustedLabelsKey, len(values))
	}
	if len(values) == 0 {
		return labels.EmptyLabels(), false, nil
	}

	raw := strings.TrimSpace(values[0])
	if raw == "" {
		return labels.EmptyLabels(), false, nil
	}
	if len(raw) > maxTrustedLabelsLen {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: value length %d exceeds limit of %d", HTTPHeaderTrustedLabelsKey, len(raw), maxTrustedLabelsLen)
	}

	lbs, err := syntax.ParseLabels(raw)
	if err != nil {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: couldn't parse labels", HTTPHeaderTrustedLabelsKey)
	}
	if lbs.IsEmpty() {
		return labels.EmptyLabels(), false, nil
	}

	var reserved []string
	lbs.Range(func(l labels.Label) {
		if isReservedTrustedLabel(l.Name) {
			reserved = append(reserved, l.Name)
		}
	})
	if len(reserved) > 0 {
		return labels.EmptyLabels(), false, fmt.Errorf("invalid %s header: label(s) %s are reserved and cannot be forced", HTTPHeaderTrustedLabelsKey, strings.Join(reserved, ", "))
	}

	return lbs, true, nil
}

// isReservedTrustedLabel reports whether name may not be set through the trusted-labels header.
// Labels prefixed with "__" are internal to Loki/Prometheus, and the backfill labels are reserved
// for the X-Loki-Backfill-Shard header, so neither may be spoofed through X-Loki-Labels.
func isReservedTrustedLabel(name string) bool {
	return strings.HasPrefix(name, "__") ||
		name == constants.BackfillLabel ||
		name == constants.BackfillShardLabel
}

// InjectTrustedLabelsContext returns a derived context carrying the validated trusted labels.
func InjectTrustedLabelsContext(ctx context.Context, lbs labels.Labels) context.Context {
	return context.WithValue(ctx, trustedLabelsKey, lbs)
}

// ExtractTrustedLabelsContext returns the trusted labels stored in ctx, or empty labels if none are
// set.
func ExtractTrustedLabelsContext(ctx context.Context) labels.Labels {
	lbs, ok := ctx.Value(trustedLabelsKey).(labels.Labels)
	if !ok {
		return labels.EmptyLabels()
	}
	return lbs
}

// applyTrustedLabels returns lbs with every trusted label set on it, overriding any body label of
// the same name. It returns lbs unchanged when there are no trusted labels.
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

// applyTrustedLabelsToSet sets every trusted label on ls, overriding any entry of the same name.
// It is the model.LabelSet counterpart of applyTrustedLabels, used by the OTLP path.
func applyTrustedLabelsToSet(ls model.LabelSet, trusted labels.Labels) {
	trusted.Range(func(l labels.Label) {
		ls[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	})
}
