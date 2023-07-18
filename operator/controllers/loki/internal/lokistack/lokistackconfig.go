package lokistack

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const annotationLokiStackConfigChangedAt = "loki.grafana.com/lokiStackConfigChangedAt"

// AnnotateForLokiStackConfig adds/updates the `loki.grafana.com/lokiStackConfigChangedAt` annotation
// to all instance of LokiStack on all namespaces to trigger the reconciliation loop.
func AnnotateForLokiStackConfig(ctx context.Context, k k8s.Client) error {
	timeStamp := time.Now().UTC().Format(time.RFC3339)

	var stacks lokiv1.LokiStackList
	err := k.List(ctx, &stacks, client.MatchingLabelsSelector{Selector: labels.Everything()})
	if err != nil {
		return kverrors.Wrap(err, "failed to list any lokistack instances", "req")
	}

	for _, s := range stacks.Items {
		ss := s.DeepCopy()
		if err := updateAnnotation(ctx, k, ss, annotationLokiStackConfigChangedAt, timeStamp); err != nil {
			return kverrors.Wrap(err, "failed to update lokistack `lokiStackConfigChangedAt` annotation", "name", ss.Name, "namespace", ss.Namespace)
		}
	}

	return nil
}
