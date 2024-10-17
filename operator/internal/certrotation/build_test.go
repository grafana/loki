package certrotation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
)

func TestBuildAll(t *testing.T) {
	cfg := configv1.BuiltInCertManagement{
		CACertValidity: "10m",
		CACertRefresh:  "5m",
		CertValidity:   "2m",
		CertRefresh:    "1m",
	}

	opts := Options{
		StackName:      "dev",
		StackNamespace: "ns",
	}
	err := ApplyDefaultSettings(&opts, cfg)
	require.NoError(t, err)

	objs, err := BuildAll(opts)
	require.NoError(t, err)
	require.Len(t, objs, 17)

	for _, obj := range objs {
		require.True(t, strings.HasPrefix(obj.GetName(), opts.StackName))
		require.Equal(t, obj.GetNamespace(), opts.StackNamespace)

		switch o := obj.(type) {
		case *corev1.Secret:
			require.Contains(t, o.Annotations, CertificateIssuer)
			require.Contains(t, o.Annotations, CertificateNotAfterAnnotation)
			require.Contains(t, o.Annotations, CertificateNotBeforeAnnotation)
		}
	}
}

func TestApplyDefaultSettings_EmptySecrets(t *testing.T) {
	cfg := configv1.BuiltInCertManagement{
		CACertValidity: "10m",
		CACertRefresh:  "5m",
		CertValidity:   "2m",
		CertRefresh:    "1m",
	}

	opts := Options{
		StackName:      "lokistack-dev",
		StackNamespace: "ns",
	}

	err := ApplyDefaultSettings(&opts, cfg)
	require.NoError(t, err)

	cs := ComponentCertSecretNames(opts.StackName)

	for _, name := range cs {
		cert, ok := opts.Certificates[name]
		require.True(t, ok)
		require.NotEmpty(t, cert.Rotation)

		hostnames := []string{
			fmt.Sprintf("%s.%s.svc", name, opts.StackNamespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", name, opts.StackNamespace),
		}

		require.ElementsMatch(t, hostnames, sets.List[string](cert.Rotation.Hostnames))
		require.Equal(t, defaultUserInfo, cert.Rotation.UserInfo)
		require.Nil(t, cert.Secret)
	}
}

func TestApplyDefaultSettings_ExistingSecrets(t *testing.T) {
	const (
		stackName      = "dev"
		stackNamespace = "ns"
	)

	cfg := configv1.BuiltInCertManagement{
		CACertValidity: "10m",
		CACertRefresh:  "5m",
		CertValidity:   "2m",
		CertRefresh:    "1m",
	}

	opts := Options{
		StackName:      stackName,
		StackNamespace: stackNamespace,
		Certificates:   ComponentCertificates{},
	}

	cs := ComponentCertSecretNames(opts.StackName)

	for _, name := range cs {
		opts.Certificates[name] = SelfSignedCertKey{
			Secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: stackNamespace,
					Annotations: map[string]string{
						CertificateNotBeforeAnnotation: "not-before",
						CertificateNotAfterAnnotation:  "not-after",
					},
				},
			},
		}
	}

	err := ApplyDefaultSettings(&opts, cfg)
	require.NoError(t, err)

	for _, name := range cs {
		cert, ok := opts.Certificates[name]
		require.True(t, ok)
		require.NotEmpty(t, cert.Rotation)

		hostnames := []string{
			fmt.Sprintf("%s.%s.svc", name, opts.StackNamespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", name, opts.StackNamespace),
		}

		require.ElementsMatch(t, hostnames, sets.List[string](cert.Rotation.Hostnames))
		require.Equal(t, defaultUserInfo, cert.Rotation.UserInfo)

		require.NotNil(t, cert.Secret)
	}
}
