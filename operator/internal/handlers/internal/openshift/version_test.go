package openshift

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	manifests "github.com/grafana/loki/operator/internal/manifests/openshift"
)

func TestFetchVersion(t *testing.T) {
	tests := []struct {
		name            string
		clusterVersion  *configv1.ClusterVersion
		expectError     bool
		expectedVersion *manifests.OpenShiftRelease
	}{
		{
			name: "valid cluster version",
			clusterVersion: &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "version",
				},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{
							Version: "4.20.1",
							State:   configv1.CompletedUpdate,
						},
					},
				},
			},
			expectError: false,
			expectedVersion: &manifests.OpenShiftRelease{
				Major: 4,
				Minor: 20,
			},
		},
		{
			name: "valid cluster version with multiple history entries",
			clusterVersion: &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "version",
				},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{
							Version: "4.20.1",
							State:   configv1.PartialUpdate,
						},
						{
							Version: "4.19.5",
							State:   configv1.CompletedUpdate,
						},
					},
				},
			},
			expectError: false,
			expectedVersion: &manifests.OpenShiftRelease{
				Major: 4,
				Minor: 19,
			},
		},
		{
			name:            "no cluster version resource",
			clusterVersion:  nil,
			expectError:     false,
			expectedVersion: nil,
		},
		{
			name: "cluster version with empty history",
			clusterVersion: &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "version",
				},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{},
				},
			},
			expectError:     true,
			expectedVersion: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()

			var fakeClient client.Client
			if tt.clusterVersion != nil {
				require.NoError(t, configv1.AddToScheme(scheme))
				fakeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(tt.clusterVersion).
					Build()
			} else {
				fakeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					Build()
			}

			ctx := context.Background()

			// Test FetchVersion
			version, err := FetchVersion(ctx, fakeClient)

			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, version)
			} else {
				require.NoError(t, err)
				if tt.expectedVersion != nil {
					require.NotNil(t, version)
					require.Equal(t, tt.expectedVersion.Major, version.Major)
					require.Equal(t, tt.expectedVersion.Minor, version.Minor)
				} else {
					require.Nil(t, version)
				}
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name        string
		versionStr  string
		expected    *manifests.OpenShiftRelease
		expectError bool
	}{
		{
			name:       "valid version with patch",
			versionStr: "4.20.1",
			expected: &manifests.OpenShiftRelease{
				Major: 4,
				Minor: 20,
			},
		},
		{
			name:       "valid version without patch",
			versionStr: "4.19",
			expected: &manifests.OpenShiftRelease{
				Major: 4,
				Minor: 19,
			},
		},
		{
			name:       "version with v prefix",
			versionStr: "v4.20.0",
			expected: &manifests.OpenShiftRelease{
				Major: 4,
				Minor: 20,
			},
		},
		{
			name:       "version with build metadata",
			versionStr: "4.20.0-rc.1+build.123",
			expected: &manifests.OpenShiftRelease{
				Major: 4,
				Minor: 20,
			},
		},
		{
			name:        "invalid version - no minor",
			versionStr:  "4",
			expectError: true,
		},
		{
			name:        "invalid version - non-numeric major",
			versionStr:  "x.20.1",
			expectError: true,
		},
		{
			name:        "invalid version - non-numeric minor",
			versionStr:  "4.x.1",
			expectError: true,
		},
		{
			name:        "empty version",
			versionStr:  "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseVersion(tt.versionStr)

			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tt.expected.Major, result.Major)
				require.Equal(t, tt.expected.Minor, result.Minor)
			}
		})
	}
}
