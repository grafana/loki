package handlers

import (
	"context"
	"errors"
	"github.com/grafana/loki/operator/internal/config"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

var (
	errAzureNoSecretFound = errors.New("can not create CredentialsRequest: no azure secret found")
	errAzureNoRegion      = errors.New("can not create CredentialsRequest: missing secret field: region")
)

// CreateCredentialsRequest creates a new CredentialsRequest resource for a Lokistack
// to request a cloud credentials Secret resource from the OpenShift cloud-credentials-operator.
func CreateCredentialsRequest(ctx context.Context, k k8s.Client, stack client.ObjectKey, secret *corev1.Secret) (string, error) {
	managedAuthEnv := config.DiscoverManagedAuthEnv()
	if managedAuthEnv == nil {
		return "", nil
	}

	if managedAuthEnv.Azure != nil && managedAuthEnv.Azure.Region == "" {
		// Managed environment for Azure does not provide Region, but we need this for the CredentialsRequest.
		// This looks like an oversight when creating the UI in OpenShift, but for now we need to pull this data
		// from somewhere else -> the Azure Storage Secret
		if secret == nil {
			return "", errAzureNoSecretFound
		}

		region := secret.Data[storage.KeyAzureRegion]
		if len(region) == 0 {
			return "", errAzureNoRegion
		}

		managedAuthEnv.Azure.Region = string(region)
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
