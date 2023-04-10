package rules

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

// GetRulerConfig returns the ruler config spec for a lokistack resource or an error.
// If the config is not found, we skip without an error.
func GetRulerConfig(ctx context.Context, k k8s.Client, req ctrl.Request) (*lokiv1.RulerConfigSpec, error) {
	var rc lokiv1.RulerConfig

	key := client.ObjectKey{Name: req.Name, Namespace: req.Namespace}
	if err := k.Get(ctx, key, &rc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, kverrors.Wrap(err, "failed to get rulerconfig", "key", key)
	}

	return &rc.Spec, nil
}
