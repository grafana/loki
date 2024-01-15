package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

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
		return kverrors.Wrap(err, "failed to delete credentialsrequest", "key", client.ObjectKeyFromObject(credReq))
	}

	return nil
}
