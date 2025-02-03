package lokistack

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

const (
	annotationRulerConfigDiscoveredAt = "loki.grafana.com/rulerConfigDiscoveredAt"
)

// AnnotateForRulerConfig adds/updates the `loki.grafana.com/rulerConfigDiscoveredAt` annotation
// to the named Lokistack in the same namespace of the RulerConfig. If no LokiStack is found, then
// skip reconciliation.
func AnnotateForRulerConfig(ctx context.Context, k k8s.Client, name, namespace string) error {
	key := client.ObjectKey{Name: name, Namespace: namespace}
	ss, err := getLokiStack(ctx, k, key)
	if ss == nil || err != nil {
		return err
	}

	timeStamp := time.Now().UTC().Format(time.RFC3339)
	if err := updateAnnotation(ctx, k, ss, annotationRulerConfigDiscoveredAt, timeStamp); err != nil {
		return kverrors.Wrap(err, "failed to update lokistack `rulerConfigDiscoveredAt` annotation", "key", key)
	}

	return nil
}

// RemoveRulerConfigAnnotation removes the `loki.grafana.com/rulerConfigDiscoveredAt` annotation
// from the named Lokistack in the same namespace of the RulerConfig. If no LokiStack is found, then
// skip reconciliation.
func RemoveRulerConfigAnnotation(ctx context.Context, k k8s.Client, name, namespace string) error {
	key := client.ObjectKey{Name: name, Namespace: namespace}
	ss, err := getLokiStack(ctx, k, key)
	if ss == nil || err != nil {
		return err
	}

	if err := removeAnnotation(ctx, k, ss, annotationRulerConfigDiscoveredAt); err != nil {
		return kverrors.Wrap(err, "failed to update lokistack `rulerConfigDiscoveredAt` annotation", "key", key)
	}

	return nil
}

func getLokiStack(ctx context.Context, k k8s.Client, key client.ObjectKey) (*lokiv1.LokiStack, error) {
	var s lokiv1.LokiStack

	if err := k.Get(ctx, key, &s); err != nil {
		if apierrors.IsNotFound(err) {
			// Do nothing
			return nil, nil
		}

		return nil, kverrors.Wrap(err, "failed to get lokistack", "key", key)
	}

	return s.DeepCopy(), nil
}
