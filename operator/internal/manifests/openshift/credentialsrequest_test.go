package openshift

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/operator/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestBuildCredentialsRequest_HasSecretRef_MatchingLokiStackNamespace(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		TokenCCOAuth: &config.TokenCCOAuthConfig{
			AWS: &config.AWSEnvironment{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.Equal(t, opts.BuildOpts.LokiStackNamespace, credReq.Spec.SecretRef.Namespace)
}

func TestBuildCredentialsRequest_HasServiceAccountNames_ContainsAllLokiStackServiceAccounts(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		TokenCCOAuth: &config.TokenCCOAuthConfig{
			AWS: &config.AWSEnvironment{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.Contains(t, credReq.Spec.ServiceAccountNames, opts.BuildOpts.LokiStackName)
	require.Contains(t, credReq.Spec.ServiceAccountNames, rulerServiceAccountName(opts))
}

func TestBuildCredentialsRequest_CloudTokenPath_MatchinOpenShiftSADirectory(t *testing.T) {
	opts := Options{
		BuildOpts: BuildOptions{
			LokiStackName:      "a-stack",
			LokiStackNamespace: "ns",
		},
		TokenCCOAuth: &config.TokenCCOAuthConfig{
			AWS: &config.AWSEnvironment{
				RoleARN: "role-arn",
			},
		},
	}

	credReq, err := BuildCredentialsRequest(opts)
	require.NoError(t, err)
	require.Equal(t, storage.ServiceAccountTokenFilePath, credReq.Spec.CloudTokenPath)
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
				TokenCCOAuth: &config.TokenCCOAuthConfig{
					AWS: &config.AWSEnvironment{
						RoleARN: "role-arn",
					},
				},
			},
			wantName:       "a-stack",
			wantSecretName: "a-stack-managed-credentials",
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			credReq, err := BuildCredentialsRequest(test.opts)
			require.NoError(t, err)
			require.Equal(t, test.wantName, credReq.Name)
			require.Equal(t, test.wantSecretName, credReq.Spec.SecretRef.Name)
		})
	}
}
