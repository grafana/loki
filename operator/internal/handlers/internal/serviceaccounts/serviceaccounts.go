package serviceaccounts

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
)

// GetUID return the server-side generated UID for a created serviceaccount to
// associate with a Secret of type SecretServiceaccountTokenType. Returns an error if the
// associated serviceaccount is not created or the get operation failed for any reason.
func GetUID(ctx context.Context, k k8s.Client, key client.ObjectKey) (string, error) {
	sa := corev1.ServiceAccount{}
	if err := k.Get(ctx, key, &sa); err != nil {
		return "", kverrors.Wrap(err, "failed to fetch associated serviceaccount uid", "key", key)
	}

	return string(sa.UID), nil
}
