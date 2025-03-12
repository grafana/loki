package handlers

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client" //nolint:typecheck
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// CreateClusterScopedResources handles the LokiStack cluster scoped create events.
func CreateClusterScopedResources(ctx context.Context, log logr.Logger, dashboards bool, operatorNs string, k k8s.Client, s *runtime.Scheme, stacks lokiv1.LokiStackList) error {
	// This has to be done here as to not introduce a circular dependency.
	gatewaySubjects := make([]rbacv1.Subject, 0, len(stacks.Items))
	rulerSubjects := make([]rbacv1.Subject, 0, len(stacks.Items))
	for _, stack := range stacks.Items {
		gatewaySubjects = append(gatewaySubjects, rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      manifests.GatewayName(stack.Name),
			Namespace: stack.Namespace,
		})
		rulerSubjects = append(rulerSubjects, rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      manifests.RulerName(stack.Name),
			Namespace: stack.Namespace,
		})
	}
	opts := openshift.NewOptionsClusterScope(operatorNs, manifests.ClusterScopeLabels(), gatewaySubjects, rulerSubjects)

	var objs []client.Object
	if dashboards {
		dashObjs, err := openshift.BuildDashboards(opts)
		if err != nil {
			return kverrors.Wrap(err, "failed to build dashboard manifests")
		}
		objs = append(objs, dashObjs...)
	}

	rbacOBjs, err := openshift.BuildRBAC(opts)
	if err != nil {
		return kverrors.Wrap(err, "failed to build RBAC manifests")
	}
	objs = append(objs, rbacOBjs...)

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
