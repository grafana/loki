package lokistack

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AnnotateForDiscoveredRules adds/updates the `loki.grafana.com/rulesDiscoveredAt` annotation
// to all instance of LokiStack on all namespaces to trigger the reconciliation loop.
func AnnotateForDiscoveredRules(ctx context.Context, k k8s.Client) error {
	var stacks lokiv1beta1.LokiStackList
	err := k.List(ctx, &stacks, client.MatchingLabelsSelector{Selector: labels.Everything()})
	if err != nil {
		return kverrors.Wrap(err, "failed to list any lokistack instances", "req")
	}

	for _, s := range stacks.Items {
		ss := s.DeepCopy()
		if ss.Annotations == nil {
			ss.Annotations = make(map[string]string)
		}

		ss.Annotations["loki.grafana.com/rulesDiscoveredAt"] = time.Now().UTC().Format(time.RFC3339)

		if err := k.Update(ctx, ss); err != nil {
			return kverrors.Wrap(err, "failed to update lokistack `rulesDiscoveredAt` annotation", "name", ss.Name, "namespace", ss.Namespace)
		}
	}

	return nil
}
