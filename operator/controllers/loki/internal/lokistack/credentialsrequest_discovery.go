package lokistack

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// AnnotateForCredentialsRequest adds the `loki.grafana.com/credentials-request-secret-ref` annotation
// to the named Lokistack. If no LokiStack is found, then skip reconciliation. Or else return an error.
func AnnotateForCredentialsRequest(ctx context.Context, k k8s.Client, key client.ObjectKey, secretRef string) error {
	stack, err := getLokiStack(ctx, k, key)
	if stack == nil || err != nil {
		return err
	}

	if val, ok := stack.Annotations[storage.AnnotationCredentialsRequestsSecretRef]; ok && val == secretRef {
		return nil
	}

	if err := updateAnnotation(ctx, k, stack, storage.AnnotationCredentialsRequestsSecretRef, secretRef); err != nil {
		return kverrors.Wrap(err, "failed to update lokistack `credentialsRequestSecretRef` annotation", "key", key)
	}

	return nil
}
