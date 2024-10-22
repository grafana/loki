package lokistack

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

const (
	annotationRulesDiscoveredAt = "loki.grafana.com/rulesDiscoveredAt"
)

// AnnotateForDiscoveredRules adds/updates the `loki.grafana.com/rulesDiscoveredAt` annotation
// to all instance of LokiStack on all namespaces to trigger the reconciliation loop.
func AnnotateForDiscoveredRules(ctx context.Context, k k8s.Client) error {
	timeStamp := time.Now().UTC().Format(time.RFC3339)

	var stacks lokiv1.LokiStackList
	err := k.List(ctx, &stacks, client.MatchingLabelsSelector{Selector: labels.Everything()})
	if err != nil {
		return kverrors.Wrap(err, "failed to list any lokistack instances", "req")
	}

	for _, s := range stacks.Items {
		ss := s.DeepCopy()
		if err := updateAnnotation(ctx, k, ss, annotationRulesDiscoveredAt, timeStamp); err != nil {
			return kverrors.Wrap(err, "failed to update lokistack `rulesDiscoveredAt` annotation", "name", ss.Name, "namespace", ss.Namespace)
		}
	}

	return nil
}
