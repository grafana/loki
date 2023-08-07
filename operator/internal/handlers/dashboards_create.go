package handlers

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// CreateDashboards handles the LokiStack dashboards create events.
func CreateDashboards(ctx context.Context, log logr.Logger, req ctrl.Request, k k8s.Client, s *runtime.Scheme) error {
	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			// maybe the user deleted it before we could react? Either way this isn't an issue
			log.Error(err, "could not find the requested loki stack", "name", req.NamespacedName)
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	objs, err := openshift.BuildDashboards(openshift.Options{
		BuildOpts: openshift.BuildOptions{
			LokiStackName:      req.Name,
			LokiStackNamespace: req.Namespace,
		},
	})
	if err != nil {
		return kverrors.Wrap(err, "failed to build dashboards manifests", "req", req)
	}

	var errCount int32

	for _, obj := range objs {
		if isManagedResource(obj, req.Namespace) {
			obj.SetNamespace(req.Namespace)

			if err := ctrl.SetControllerReference(&stack, obj, s); err != nil {
				log.Error(err, "failed to set controller owner reference to resource")
				errCount++
				continue
			}
		}

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
