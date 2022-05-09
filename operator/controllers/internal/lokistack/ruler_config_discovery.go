package lokistack

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokistackv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AnnotateForRulerConfig adds/updates the `loki.grafana.com/rulerConfigDiscoveredAt` annotation
// to the named Lokistack in the same namespace of the RulerConfig. If no LokiStack is found, then
// skip reconciliation.
func AnnotateForRulerConfig(ctx context.Context, k k8s.Client, name, namespace string) error {
	var s lokistackv1beta1.LokiStack
	key := client.ObjectKey{Name: name, Namespace: namespace}

	if err := k.Get(ctx, key, &s); err != nil {
		if apierrors.IsNotFound(err) {
			// Do nothing
			return nil
		}

		return kverrors.Wrap(err, "failed to get lokistack", "key", key)
	}

	ss := s.DeepCopy()
	if ss.Annotations == nil {
		ss.Annotations = make(map[string]string)
	}

	ss.Annotations["loki.grafana.com/rulerConfigDiscoveredAt"] = time.Now().Format(time.RFC3339)

	if err := k.Update(ctx, ss); err != nil {
		return kverrors.Wrap(err, "failed to update lokistack `rulerConfigDiscoveredAt` annotation", "key", key)
	}

	return nil
}
