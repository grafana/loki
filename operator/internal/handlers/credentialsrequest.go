package handlers

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/config"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// CreateUpdateDeleteCredentialsRequest creates a new CredentialsRequest resource for a Lokistack
// to request a cloud credentials Secret resource from the OpenShift cloud-credentials-operator.
func CreateUpdateDeleteCredentialsRequest(ctx context.Context, log logr.Logger, scheme *runtime.Scheme, tokenCCOAuth *config.TokenCCOAuthConfig, k k8s.Client, req ctrl.Request) error {
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

	if !hasManagedCredentialMode(&stack) {
		// Operator is running in managed-mode, but stack specifies non-managed mode -> skip CredentialsRequest
		var credReq cloudcredentialv1.CredentialsRequest
		if err := k.Get(ctx, req.NamespacedName, &credReq); err != nil {
			if apierrors.IsNotFound(err) {
				// CredentialsRequest does not exist -> this is what we want
				return nil
			}

			return kverrors.Wrap(err, "failed to lookup CredentialsRequest", "name", req.String())
		}

		if err := k.Delete(ctx, &credReq); err != nil {
			return kverrors.Wrap(err, "failed to remove CredentialsRequest", "name", req.String())
		}
		return nil
	}

	opts := openshift.Options{
		BuildOpts: openshift.BuildOptions{
			LokiStackName:      stack.Name,
			LokiStackNamespace: stack.Namespace,
			RulerName:          manifests.RulerName(stack.Name),
		},
		TokenCCOAuth: tokenCCOAuth,
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

// hasManagedCredentialMode returns true, if the LokiStack is configured to run in managed-mode.
// This function assumes only being called when the operator is running in managed-credentials mode, so it is
// only returning false when the credential-mode has been explicitly configured, even if the target storage
// is not supporting managed-mode at all.
func hasManagedCredentialMode(stack *lokiv1.LokiStack) bool {
	switch stack.Spec.Storage.Secret.CredentialMode {
	case lokiv1.CredentialModeStatic, lokiv1.CredentialModeToken:
		return false
	case lokiv1.CredentialModeTokenCCO:
		return true
	default:
	}

	return true
}
