package rules

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

// getRulerConfig returns the ruler config spec for a lokistack resource or an error.
// If the config is not found, we skip without an error.
func getRulerConfig(ctx context.Context, k k8s.Client, key client.ObjectKey) (*lokiv1.RulerConfigSpec, error) {
	var rc lokiv1.RulerConfig
	if err := k.Get(ctx, key, &rc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, kverrors.Wrap(err, "failed to get rulerconfig", "key", key)
	}

	return &rc.Spec, nil
}
