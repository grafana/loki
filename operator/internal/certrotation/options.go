package certrotation

import (
	"crypto/x509"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
)

// ComponentCertificates is a map of lokistack component names to TLS certificates
type ComponentCertificates map[string]SelfSignedCertKey

// Options is a set of configuration values to use when
// building manifests for LokiStack certificates.
type Options struct {
	StackName      string
	StackNamespace string

	CACertValidity     time.Duration
	CACertRefresh      time.Duration
	TargetCertValidity time.Duration
	TargetCertRefresh  time.Duration

	Signer       SigningCA
	CABundle     *corev1.ConfigMap
	RawCACerts   []*x509.Certificate
	Certificates ComponentCertificates
}

// SigningCA rotates a self-signed signing CA stored in a secret. It creates a new one when
// - refresh duration is over
// - or 80% of validity is over
// - or the CA is expired.
type SigningCA struct {
	RawCA   *crypto.CA
	Secret  *corev1.Secret
	creator signerRotation
}

// SelfSignedCertKey rotates a key and cert signed by a signing CA and stores it in a secret.
//
// It creates a new one when
// - refresh duration is over
// - or 80% of validity is over
// - or the cert is expired.
// - or the signing CA changes.
type SelfSignedCertKey struct {
	Secret  *corev1.Secret
	creator certificateRotation
}
