package certrotation

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildAll(t *testing.T) {
	// A default test CA
	testCA, err := crypto.MakeSelfSignedCAConfigForDuration("lokistack-dev-ca-bundle", 30*24*time.Hour)
	require.NoError(t, err)

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	err = testCA.WriteCertConfig(certBytes, keyBytes)
	require.NoError(t, err)

	rawCA, err := crypto.GetCAFromBytes(certBytes.Bytes(), keyBytes.Bytes())
	require.NoError(t, err)

	cfg := configv1.BuiltInCertManagement{
		CACertValidity: "10m",
		CACertRefresh:  "5m",
		CertValidity:   "2m",
		CertRefresh:    "1m",
	}

	opts := Options{
		StackName:      "dev",
		StackNamespace: "ns",
		Signer: SigningCA{
			RawCA: rawCA,
		},
		RawCACerts: rawCA.Config.Certs,
	}
	err = ApplyDefaultSettings(&opts, cfg)
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

func TestApplyDefaultSettings(t *testing.T) {
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

		hostnames := []string{
			fmt.Sprintf("%s.%s.svc", name, opts.StackNamespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", name, opts.StackNamespace),
		}

		c := cert.Creator.(*servingCertCreator)
		require.ElementsMatch(t, c.Hostnames, hostnames)
		require.Equal(t, c.UserInfo, defaultUserInfo)
	}
}
