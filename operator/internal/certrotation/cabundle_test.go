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

func TestBuildCABundle_Create(t *testing.T) {
	rawCA, _ := newTestCABundle(t, "test-ca")

	opts := &Options{
		StackName:      "dev",
		StackNamespace: "ns",
		Signer: SigningCA{
			RawCA: rawCA,
		},
	}

	obj, err := buildCABundle(opts)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.Len(t, opts.RawCACerts, 1)
}

func TestBuildCABundle_Append(t *testing.T) {
	_, rawCABytes := newTestCABundle(t, "test-ca")
	newRawCA, _ := newTestCABundle(t, "test-ca-other")

	opts := &Options{
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
	}

	obj, err := buildCABundle(opts)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.Len(t, opts.RawCACerts, 2)
}

func newTestCABundle(t *testing.T, name string) (*crypto.CA, []byte) {
	testCA, err := crypto.MakeSelfSignedCAConfigForDuration(name, 1*time.Hour)
	require.NoError(t, err)

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	err = testCA.WriteCertConfig(certBytes, keyBytes)
	require.NoError(t, err)

	rawCA, err := crypto.GetCAFromBytes(certBytes.Bytes(), keyBytes.Bytes())
	require.NoError(t, err)

	rawCABytes, err := crypto.EncodeCertificates(rawCA.Config.Certs...)
	require.NoError(t, err)

	return rawCA, rawCABytes
}
