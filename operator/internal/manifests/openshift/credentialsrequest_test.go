package openshift

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestBuildCredentialsRequest_HasOwnerAnnotation(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		ManagedAuthEnv: &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.Contains(t, credReq.Annotations, AnnotationCredentialsRequestOwner)
}

func TestBuildCredentialsRequest_HasSecretRef_MatchingLokiStackNamespace(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		ManagedAuthEnv: &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.Equal(t, opts.BuildOpts.LokiStackNamespace, credReq.Spec.SecretRef.Namespace)
}

func TestBuildCredentialsRequest_HasServiceAccountNames_ContainsLokiStackName(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		ManagedAuthEnv: &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.Contains(t, credReq.Spec.ServiceAccountNames, opts.BuildOpts.LokiStackName)
}

func TestBuildCredentialsRequest_CloudTokenPath_MatchinOpenShiftSADirectory(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		ManagedAuthEnv: &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(credReq.Spec.CloudTokenPath, storage.SATokenVolumeOcpDirectory))
}

func TestBuildCredentialsRequest_FollowsNamingConventions(t *testing.T) {
	tests := []struct {
		desc           string
		opts           Options
		wantName       string
		wantSecretName string
	}{
		{
			desc: "aws",
			opts: Options{
				BuildOpts: BuildOptions{
					LokiStackName:      "a-stack",
					LokiStackNamespace: "ns",
				},
				ManagedAuthEnv: &ManagedAuthEnv{
					AWS: &AWSSTSEnv{
						RoleARN: "role-arn",
					},
				},
			},
			wantName:       "ns-a-stack-aws-creds",
			wantSecretName: "a-stack-aws-creds",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			credReq, err := BuildCredentialsRequest(test.opts)
			require.NoError(t, err)
			require.Equal(t, test.wantName, credReq.Name)
			require.Equal(t, test.wantSecretName, credReq.Spec.SecretRef.Name)
		})
	}
}
