package openshift

import (
	"context"
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/grafana/loki/operator/internal/status"
)

const (
	ccoNamespace = "openshift-cloud-credential-operator"
)

func GetManagedAuthCredentials(ctx context.Context, k k8s.Client, l logr.Logger, stack client.ObjectKey, fg configv1.FeatureGates) (*corev1.Secret, error) {
	var managedAuthCreds corev1.Secret
	managedAuthEnv := DiscoverManagedAuthEnv()
	if managedAuthEnv == nil || !fg.OpenShift.Enabled {
		return nil, nil
	}

	l.Info("discovered managed authentication credentials cluster", "env", managedAuthEnv)

	managedAuthCredsKey, err := CreateCredentialsRequest(ctx, k, stack, managedAuthEnv)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed creating OpenShift CCO CredentialsRequest", "name", stack)
	}

	if err := k.Get(ctx, managedAuthCredsKey, &managedAuthCreds); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &status.DegradedError{
				Message: "Missing OpenShift CCO managed authentication credentials secret",
				Reason:  lokiv1.ReasonMissingManagedAuthSecret,
				Requeue: true,
			}
		}
		return nil, kverrors.Wrap(err, "failed to lookup OpenShift CCO managed authentication credentials secret", "name", stack)
	}

	return &managedAuthCreds, nil
}

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
