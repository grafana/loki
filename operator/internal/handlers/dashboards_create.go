package handlers

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client" //nolint:typecheck
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// CreateDashboards handles the LokiStack dashboards create events.
func CreateDashboards(ctx context.Context, log logr.Logger, operatorNs string, k k8s.Client, s *runtime.Scheme) error {
	objs, err := openshift.BuildDashboards(operatorNs)
	if err != nil {
		return kverrors.Wrap(err, "failed to build dashboard manifests")
	}

	var errCount int32
	for _, obj := range objs {
		desired := obj.DeepCopyObject().(client.Object)
		mutateFn := manifests.MutateFuncFor(obj, desired, nil)

		op, err := ctrl.CreateOrUpdate(ctx, k, obj, mutateFn)
		if err != nil {
			log.Error(err, "failed to configure resource")
			errCount++
			continue
		}

		msg := fmt.Sprintf("Resource has been %s", op)
		switch op {
		case ctrlutil.OperationResultNone:
			log.V(1).Info(msg)
		default:
			log.Info(msg)
		}
	}

	if errCount > 0 {
		return kverrors.New("failed to configure lokistack dashboard resources")
	}

	return nil
}
