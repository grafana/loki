package certrotation

import (
	"bytes"
	"crypto/x509"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/cert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// buildCABundle returns a ConfigMap including all known non-expired signing CAs across rotations.
func buildCABundle(opts *Options) (client.Object, error) {
	cm := newConfigMap(*opts)

	certs, err := manageCABundleConfigMap(cm, opts.Signer.RawCA.Config.Certs[0])
	if err != nil {
		return nil, err
	}

	opts.RawCACerts = certs

	caBytes, err := crypto.EncodeCertificates(certs...)
	if err != nil {
		return nil, err
	}

	cm.Data[CAFile] = string(caBytes)

	return cm, nil
}

func newConfigMap(opts Options) *corev1.ConfigMap {
	current := opts.CABundle.DeepCopy()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CABundleName(opts.StackName),
			Namespace: opts.StackNamespace,
		},
	}

	if current != nil {
		cm.Annotations = current.Annotations
		cm.Labels = current.Labels
		cm.Data = current.Data
	}

	return cm
}

// manageCABundleConfigMap adds the new certificate to the list of cabundles, eliminates duplicates, and prunes the list of expired
// certs to trust as signers
func manageCABundleConfigMap(caBundleConfigMap *corev1.ConfigMap, currentSigner *x509.Certificate) ([]*x509.Certificate, error) {
	if caBundleConfigMap.Data == nil {
		caBundleConfigMap.Data = map[string]string{}
	}

	certificates := []*x509.Certificate{}
	caBundle := caBundleConfigMap.Data[CAFile]
	if len(caBundle) > 0 {
		var err error
		certificates, err = cert.ParseCertsPEM([]byte(caBundle))
		if err != nil {
			return nil, err
		}
	}
	certificates = append([]*x509.Certificate{currentSigner}, certificates...)
	certificates = crypto.FilterExpiredCerts(certificates...)

	finalCertificates := []*x509.Certificate{}
	// now check for duplicates. n^2, but super simple
nextCertificate:
	for i := range certificates {
		for j := range finalCertificates {
			if bytes.Equal(certificates[i].Raw, finalCertificates[j].Raw) {
				continue nextCertificate
			}
		}
		finalCertificates = append(finalCertificates, certificates[i])
	}

	return finalCertificates, nil
}
