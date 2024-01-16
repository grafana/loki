package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// CreateCredentialsRequest creates a new CredentialsRequest resource for a Lokistack
// to request a cloud credentials Secret resource from the OpenShift cloud-credentials-operator.
func CreateCredentialsRequest(ctx context.Context, k k8s.Client, stack client.ObjectKey) (string, error) {
	managedAuthEnv := openshift.DiscoverManagedAuthEnv()
	if managedAuthEnv == nil {
		return "", nil
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
		return "", err
	}

	if err := k.Create(ctx, credReq); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", kverrors.Wrap(err, "failed to create credentialsrequest", "key", client.ObjectKeyFromObject(credReq))
		}
	}

	return credReq.Spec.SecretRef.Name, nil
}
