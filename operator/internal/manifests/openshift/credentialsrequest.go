package openshift

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

const azureFallbackRegion = "centralus"

func BuildCredentialsRequest(opts Options) (*cloudcredentialv1.CredentialsRequest, error) {
	stack := client.ObjectKey{Name: opts.BuildOpts.LokiStackName, Namespace: opts.BuildOpts.LokiStackNamespace}

	providerSpec, err := encodeProviderSpec(opts.TokenCCOAuth)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed encoding credentialsrequest provider spec")
	}

	return &cloudcredentialv1.CredentialsRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stack.Name,
			Namespace: stack.Namespace,
		},
		Spec: cloudcredentialv1.CredentialsRequestSpec{
			SecretRef: corev1.ObjectReference{
				Name:      storage.ManagedCredentialsSecretName(stack.Name),
				Namespace: stack.Namespace,
			},
			ProviderSpec: providerSpec,
			ServiceAccountNames: []string{
				stack.Name,
				rulerServiceAccountName(opts),
			},
			CloudTokenPath: storage.ServiceAccountTokenFilePath,
		},
	}, nil
}

func encodeProviderSpec(env *config.TokenCCOAuthConfig) (*runtime.RawExtension, error) {
	var spec runtime.Object

	switch {
	case env.AWS != nil:
		spec = &cloudcredentialv1.AWSProviderSpec{
			StatementEntries: []cloudcredentialv1.StatementEntry{
				{
					Action: []string{
						"s3:ListBucket",
						"s3:PutObject",
						"s3:GetObject",
						"s3:DeleteObject",
					},
					Effect:   "Allow",
					Resource: "arn:aws:s3:*:*:*",
				},
			},
			STSIAMRoleARN: env.AWS.RoleARN,
		}
	case env.Azure != nil:
		azure := env.Azure
		if azure.Region == "" {
			// The OpenShift Console currently does not provide a UI to configure the Azure Region
			// for an operator using managed credentials. Because the CredentialsRequest is currently
			// not used to create a Managed Identity, the region is actually never used.
			// We default to the US region if nothing is set, so that the CredentialsRequest can be
			// created. This should have no effect on the generated credential secret.
			// The region can be configured by setting an environment variable on the operator Subscription.
			azure.Region = azureFallbackRegion
		}

		spec = &cloudcredentialv1.AzureProviderSpec{
			Permissions: []string{
				"Microsoft.Storage/storageAccounts/blobServices/read",
				"Microsoft.Storage/storageAccounts/blobServices/containers/read",
				"Microsoft.Storage/storageAccounts/blobServices/containers/write",
				"Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action",
				"Microsoft.Storage/storageAccounts/read",
				"Microsoft.Storage/storageAccounts/write",
				"Microsoft.Storage/storageAccounts/delete",
				"Microsoft.Storage/storageAccounts/listKeys/action",
				"Microsoft.Resources/tags/write",
			},
			DataPermissions: []string{
				"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/delete",
				"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write",
				"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read",
				"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/add/action",
				"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/move/action",
			},
			AzureClientID:       azure.ClientID,
			AzureRegion:         azure.Region,
			AzureSubscriptionID: azure.SubscriptionID,
			AzureTenantID:       azure.TenantID,
		}
	case env.GCP != nil:
		spec = &cloudcredentialv1.GCPProviderSpec{
			PredefinedRoles: []string{
				"roles/iam.workloadIdentityUser",
				"roles/storage.objectAdmin",
			},
			Audience:            env.GCP.Audience,
			ServiceAccountEmail: env.GCP.ServiceAccountEmail,
		}
	}

	encodedSpec, err := cloudcredentialv1.Codec.EncodeProviderSpec(spec.DeepCopyObject())
	return encodedSpec, err
}
