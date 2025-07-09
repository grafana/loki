package lokistack

import (
	"context"
	"fmt"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

const certRotationRequiredAtKey = "loki.grafana.com/certRotationRequiredAt"

// AnnotateForRequiredCertRotation adds/updates the `loki.grafana.com/certRotationRequiredAt` annotation
// to the named Lokistack if any of the managed client/serving/ca certificates expired. If no LokiStack
// is found, then skip reconciliation.
func AnnotateForRequiredCertRotation(ctx context.Context, k k8s.Client, name, namespace string) error {
	var s lokiv1.LokiStack
	key := client.ObjectKey{Name: name, Namespace: namespace}

	if err := k.Get(ctx, key, &s); err != nil {
		if apierrors.IsNotFound(err) {
			// Do nothing
			return nil
		}

		return kverrors.Wrap(err, "failed to get lokistack", "key", key)
	}

	ss := s.DeepCopy()
	timeStamp := time.Now().UTC().Format(time.RFC3339)
	if err := updateAnnotation(ctx, k, ss, certRotationRequiredAtKey, timeStamp); err != nil {
		return kverrors.Wrap(err, fmt.Sprintf("failed to update lokistack `%s` annotation", certRotationRequiredAtKey), "key", key)
	}

	return nil
}
