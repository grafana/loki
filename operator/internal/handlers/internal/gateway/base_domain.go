package gateway

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/status"
)

// GetOpenShiftBaseDomain returns the cluster DNS base domain on OpenShift
// clusters to auto-create redirect URLs for OpenShift Auth or an error.
// If the config.openshift.io/DNS object is not found the whole lokistack
// resoure is set to a degraded state.
func GetOpenShiftBaseDomain(ctx context.Context, k k8s.Client, req ctrl.Request) (string, error) {
	var cluster configv1.DNS
	key := client.ObjectKey{Name: "cluster"}
	if err := k.Get(ctx, key, &cluster); err != nil {

		if apierrors.IsNotFound(err) {
			return "", &status.DegradedError{
				Message: "Missing cluster DNS configuration to read base domain",
				Reason:  lokiv1.ReasonMissingGatewayOpenShiftBaseDomain,
				Requeue: true,
			}
		}
		return "", kverrors.Wrap(err, "failed to lookup lokistack gateway base domain",
			"name", key)
	}

	return cluster.Spec.BaseDomain, nil
}
