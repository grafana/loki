package handlers

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
func CreateClusterScopedResources(ctx context.Context, log logr.Logger, dashboards bool, operatorNs string, k k8s.Client, s *runtime.Scheme, stacks []lokiv1.LokiStack) error {
	// This has to be done here as to not introduce a circular dependency.
	rulerSubjects := make([]rbacv1.Subject, 0, len(stacks))
	for _, stack := range stacks {
		rulerSubjects = append(rulerSubjects, rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      manifests.RulerName(stack.Name),
			Namespace: stack.Namespace,
		})
	}
	opts := openshift.NewOptionsClusterScope(operatorNs, manifests.ClusterScopeLabels(), rulerSubjects)

	objs := openshift.BuildRBAC(opts)
	if dashboards {
		objs = append(objs, openshift.BuildDashboards(opts.OperatorNs)...)
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
		return kverrors.New("failed to configure lokistack cluster-scoped resources")
	}

	// Delete legacy RBAC resources
	// This needs to live here and not in DeleteClusterScopedResources as we want to
	// delete the legacy RBAC resources when LokiStack is reconciled and not on delete.
	var legacyObjs []client.Object
	for _, stack := range stacks {
		// This name would clash with the new cluster-scoped resources. Skip it.
		if stack.Name == "lokistack" {
			continue
		}
		legacyObjs = append(legacyObjs, openshift.LegacyRBAC(manifests.GatewayName(stack.Name), manifests.RulerName(stack.Name))...)
	}
	for _, obj := range legacyObjs {
		key := client.ObjectKeyFromObject(obj)
		if err := k.Delete(ctx, obj, &client.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return kverrors.Wrap(err, "failed to delete resource", "kind", obj.GetObjectKind(), "key", key)
		}
	}

	return nil
}
