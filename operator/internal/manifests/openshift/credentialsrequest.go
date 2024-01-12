package openshift

import (
	"context"
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

const (
	ccoNamespace = "openshift-cloud-credential-operator"
)

func CreateCredentialsRequest(ctx context.Context, k k8s.Client, stack client.ObjectKey, sts *ManagedAuthEnv) (client.ObjectKey, error) {
	var secretKey client.ObjectKey
	providerSpec, secretName, err := encodeProvideSpec(stack.Name, sts)
	if err != nil {
		return secretKey, kverrors.Wrap(err, "failed encoding credentialsrequest provider spec")
	}

	secretKey = client.ObjectKey{Name: secretName, Namespace: stack.Namespace}
	credReq := &cloudcredentialv1.CredentialsRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stack.Namespace + "-" + secretKey.Name,
			Namespace: ccoNamespace,
			Annotations: map[string]string{
				openshift.AnnotationCredentialsRequestOwner: stack.String(),
			},
		},
		Spec: cloudcredentialv1.CredentialsRequestSpec{
			SecretRef: corev1.ObjectReference{
				Name:      secretKey.Name,
				Namespace: secretKey.Namespace,
			},
			ProviderSpec: providerSpec,
			ServiceAccountNames: []string{
				stack.Name,
			},
			CloudTokenPath: path.Join(storage.SATokenVolumeOcpDirectory, "token"),
		},
	}

	if err := k.Create(ctx, credReq); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return secretKey, kverrors.Wrap(err, "failed to create credentialsrequest", "key", client.ObjectKeyFromObject(credReq))
		}
	}

	return secretKey, nil
}

func encodeProvideSpec(stackKey string, maEnv *ManagedAuthEnv) (*runtime.RawExtension, string, error) {
	var (
		spec       runtime.Object
		secretName string
	)

	switch {
	case maEnv.AWS != nil:
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
			STSIAMRoleARN: maEnv.AWS.RoleARN,
		}
		secretName = fmt.Sprintf("%s-aws-creds", stackKey)
	}

	encodedSpec, err := cloudcredentialv1.Codec.EncodeProviderSpec(spec.DeepCopyObject())
	return encodedSpec, secretName, err
}
