package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// DeleteCredentialsRequest deletes a LokiStack's accompanying CredentialsRequest resource
// to trigger the OpenShift cloud-credentials-operator to wipe out any credentials related
// Secret resource on the LokiStack namespace.
func DeleteCredentialsRequest(ctx context.Context, k k8s.Client, stack client.ObjectKey) error {
	managedAuthEnv := openshift.DiscoverManagedAuthEnv()
	if managedAuthEnv == nil {
		return nil
	}

	opts := openshift.Options{
		BuildOpts: openshift.BuildOptions{
			LokiStackName:      stack.Name,
			LokiStackNamespace: stack.Namespace,
		},
		ManagedAuthEnv: managedAuthEnv,
	}

	credReq, err := openshift.BuildCredentialsRequest(opts)
	if err != nil {
		return kverrors.Wrap(err, "failed to build credentialsrequest", "key", stack)
	}

	if err := k.Delete(ctx, credReq); err != nil {
		if !errors.IsNotFound(err) {
			return kverrors.Wrap(err, "failed to delete credentialsrequest", "key", client.ObjectKeyFromObject(credReq))
		}
	}

	return nil
}
