package handlers

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/config"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// CreateCredentialsRequest creates a new CredentialsRequest resource for a Lokistack
// to request a cloud credentials Secret resource from the OpenShift cloud-credentials-operator.
func CreateCredentialsRequest(ctx context.Context, log logr.Logger, scheme *runtime.Scheme, managedAuth *config.ManagedAuthConfig, k k8s.Client, req ctrl.Request) error {
	ll := log.WithValues("lokistack", req.NamespacedName, "event", "createCredentialsRequest")

	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			// maybe the user deleted it before we could react? Either way this isn't an issue
			ll.Error(err, "could not find the requested LokiStack", "name", req.String())
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup LokiStack", "name", req.String())
	}

	opts := openshift.Options{
		BuildOpts: openshift.BuildOptions{
			LokiStackName:      stack.Name,
			LokiStackNamespace: stack.Namespace,
			RulerName:          manifests.RulerName(stack.Name),
		},
		ManagedAuth: managedAuth,
	}

	credReq, err := openshift.BuildCredentialsRequest(opts)
	if err != nil {
		return err
	}

	err = ctrl.SetControllerReference(&stack, credReq, scheme)
	if err != nil {
		return kverrors.Wrap(err, "failed to set controller owner reference to resource")
	}

	desired := credReq.DeepCopyObject().(client.Object)
	mutateFn := manifests.MutateFuncFor(credReq, desired, map[string]string{})

	op, err := ctrl.CreateOrUpdate(ctx, k, credReq, mutateFn)
	if err != nil {
		return kverrors.Wrap(err, "failed to configure CredentialRequest")
	}

	msg := fmt.Sprintf("Resource has been %s", op)
	switch op {
	case ctrlutil.OperationResultNone:
		ll.V(1).Info(msg)
	default:
		ll.Info(msg)
	}

	return nil
}
