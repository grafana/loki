package gateway

import (
	"context"

	"github.com/ViaQ/logerr/kverrors"
	lokiv1beta1 "github.com/grafana/loki-operator/api/v1beta1"
	"github.com/grafana/loki-operator/internal/external/k8s"
	"github.com/grafana/loki-operator/internal/status"
	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			statusErr := status.SetDegradedCondition(ctx, k, req,
				"Missing cluster DNS configuration to read base domain",
				lokiv1beta1.ReasonMissingGatewayOpenShiftBaseDomain,
			)
			if statusErr != nil {
				return "", statusErr
			}

			return "", kverrors.Wrap(err, "Missing cluster DNS configuration to read base domain")
		}
		return "", kverrors.Wrap(err, "failed to lookup lokistack gateway base domain",
			"name", key)
	}

	return cluster.Spec.BaseDomain, nil
}
