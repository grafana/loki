package certrotation

import (
	"bytes"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildCABundle(t *testing.T) {
	// A default test CA
	testCA, err := crypto.MakeSelfSignedCAConfigForDuration("test-build-ca-bundle", 30*24*time.Hour)
	require.NoError(t, err)

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	err = testCA.WriteCertConfig(certBytes, keyBytes)
	require.NoError(t, err)

	rawCA, err := crypto.GetCAFromBytes(certBytes.Bytes(), keyBytes.Bytes())
	require.NoError(t, err)

	rawCABytes, err := crypto.EncodeCertificates(rawCA.Config.Certs...)
	require.NoError(t, err)

	// A new test CA for append test
	newCA, err := crypto.MakeSelfSignedCAConfigForDuration("test-build-ca-bundle-1", 30*24*time.Hour)
	require.NoError(t, err)

	newCertBytes := &bytes.Buffer{}
	newKeyBytes := &bytes.Buffer{}
	err = newCA.WriteCertConfig(newCertBytes, newKeyBytes)
	require.NoError(t, err)

	newRawCA, err := crypto.GetCAFromBytes(newCertBytes.Bytes(), newKeyBytes.Bytes())
	require.NoError(t, err)

	tt := []struct {
		desc    string
		opts    *Options
		wantLen int
	}{
		{
			desc: "create",
			opts: &Options{
				StackName:      "dev",
				StackNamespace: "ns",
				Signer: SigningCA{
					RawCA: rawCA,
				},
			},
			wantLen: 1,
		},
		{
			desc: "append",
			opts: &Options{
				StackName:      "dev",
				StackNamespace: "ns",
				Signer: SigningCA{
					RawCA: newRawCA,
				},
				CABundle: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dev-ca-bundle",
						Namespace: "ns",
					},
					Data: map[string]string{
						CAFile: string(rawCABytes),
					},
				},
			},
			wantLen: 2,
		},
	}

	for _, test := range tt {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			obj, err := buildCABundle(test.opts)
			require.NoError(t, err)
			require.NotNil(t, obj)
			require.Len(t, test.opts.RawCACerts, test.wantLen)
		})
	}
}
