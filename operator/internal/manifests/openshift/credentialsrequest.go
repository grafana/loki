package openshift

import (
	"fmt"
	"os"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/manifests/storage"
)

const (
	ccoNamespace = "openshift-cloud-credential-operator"
)

func BuildCredentialsRequest(opts Options) (*cloudcredentialv1.CredentialsRequest, error) {
	stack := client.ObjectKey{Name: opts.BuildOpts.LokiStackName, Namespace: opts.BuildOpts.LokiStackNamespace}

	providerSpec, secretName, err := encodeProviderSpec(opts.BuildOpts.LokiStackName, opts.ManagedAuthEnv)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed encoding credentialsrequest provider spec")
	}

	return &cloudcredentialv1.CredentialsRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", stack.Namespace, secretName),
			Namespace: ccoNamespace,
			Annotations: map[string]string{
				AnnotationCredentialsRequestOwner: stack.String(),
			},
		},
		Spec: cloudcredentialv1.CredentialsRequestSpec{
			SecretRef: corev1.ObjectReference{
				Name:      secretName,
				Namespace: stack.Namespace,
			},
			ProviderSpec: providerSpec,
			ServiceAccountNames: []string{
				stack.Name,
			},
			CloudTokenPath: path.Join(storage.SATokenVolumeOcpDirectory, "token"),
		},
	}, nil
}

func encodeProviderSpec(stackName string, env *ManagedAuthEnv) (*runtime.RawExtension, string, error) {
	var (
		spec       runtime.Object
		secretName string
	)

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
		secretName = fmt.Sprintf("%s-aws-creds", stackName)
	}

	encodedSpec, err := cloudcredentialv1.Codec.EncodeProviderSpec(spec.DeepCopyObject())
	return encodedSpec, secretName, err
}

func DiscoverManagedAuthEnv() *ManagedAuthEnv {
	// AWS
	roleARN := os.Getenv("ROLEARN")

	switch {
	case roleARN != "":
		return &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: roleARN,
			},
		}
	}

	return nil
}
