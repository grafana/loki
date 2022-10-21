package certrotation

import (
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CertificatesExpired returns an error if any certificates expired and the list of expiry reasons.
func CertificatesExpired(opts Options) error {
	if opts.Signer.Secret == nil || opts.CABundle == nil {
		return nil
	}

	if opts.GatewayClientCertificate.Secret == nil {
		return nil
	}

	for _, cert := range opts.Certificates {
		if cert.Secret == nil {
			return nil
		}
	}

	rawCA, err := crypto.GetCAFromBytes(opts.Signer.Secret.Data[corev1.TLSCertKey], opts.Signer.Secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return kverrors.Wrap(err, "failed to get signing CA from secret")
	}

	caBundle := opts.CABundle.Data[CAFile]
	caCerts, err := crypto.CertsFromPEM([]byte(caBundle))
	if err != nil {
		return kverrors.Wrap(err, "failed to get ca bundle certificates from configmap")
	}

	var reasons []string

	annotations := opts.GatewayClientCertificate.Secret.Annotations
	reason := opts.GatewayClientCertificate.Creator.NeedNewTargetCertKeyPair(annotations, rawCA, caCerts, opts.TargetCertRefresh, opts.RefreshOnlyWhenExpired)
	reasons = append(reasons, reason)

	for _, cert := range opts.Certificates {
		reason = cert.Creator.NeedNewTargetCertKeyPair(cert.Secret.Annotations, rawCA, caCerts, opts.TargetCertRefresh, opts.RefreshOnlyWhenExpired)
		reasons = append(reasons, reason)
	}

	if len(reasons) == 0 {
		return nil
	}

	return &CertExpiredError{Message: "certificates expired", Reasons: reasons}
}

// buildTargetCertKeyPairSecrets returns a slice of all rotated client and serving lokistack certificates.
func buildTargetCertKeyPairSecrets(opts Options) ([]client.Object, error) {
	var (
		res                    = make([]client.Object, 0)
		ns                     = opts.StackNamespace
		rawCA                  = opts.Signer.RawCA
		caBundle               = opts.RawCACerts
		stackName              = opts.StackName
		validity               = opts.TargetCertValidity
		refresh                = opts.TargetCertRefresh
		refreshOnlyWhenExpired = opts.RefreshOnlyWhenExpired
	)

	// Build Index Gateway Client Secret
	gwSecret := newTargetCertificateSecret(GatewayClientSecretName(stackName), ns, opts.GatewayClientCertificate.Secret)
	reason := opts.GatewayClientCertificate.Creator.NeedNewTargetCertKeyPair(gwSecret.Annotations, rawCA, caBundle, refresh, refreshOnlyWhenExpired)
	if len(reason) > 0 {
		if err := setTargetCertKeyPairSecret(gwSecret, validity, rawCA, opts.GatewayClientCertificate.Creator); err != nil {
			return nil, err
		}
	}

	res = append(res, gwSecret)

	for name, cert := range opts.Certificates {
		secret := newTargetCertificateSecret(name, ns, cert.Secret)
		reason := cert.Creator.NeedNewTargetCertKeyPair(secret.Annotations, rawCA, caBundle, refresh, refreshOnlyWhenExpired)
		if len(reason) > 0 {
			if err := setTargetCertKeyPairSecret(secret, validity, rawCA, cert.Creator); err != nil {
				return nil, err
			}
		}

		res = append(res, secret)
	}

	return res, nil
}

func newTargetCertificateSecret(name, ns string, s *corev1.Secret) *corev1.Secret {
	current := s.DeepCopy()

	ss := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Type: corev1.SecretTypeTLS,
	}

	if current != nil {
		ss.Annotations = current.Annotations
		ss.Labels = current.Labels
		ss.Data = current.Data
	}

	return ss
}

// setTargetCertKeyPairSecret creates a new cert/key pair and sets them in the secret.  Only one of client, serving, or signer rotation may be specified.
func setTargetCertKeyPairSecret(targetCertKeyPairSecret *corev1.Secret, validity time.Duration, signer *crypto.CA, certCreator TargetCertCreator) error {
	if targetCertKeyPairSecret.Annotations == nil {
		targetCertKeyPairSecret.Annotations = map[string]string{}
	}
	if targetCertKeyPairSecret.Data == nil {
		targetCertKeyPairSecret.Data = map[string][]byte{}
	}

	// our annotation is based on our cert validity, so we want to make sure that we don't specify something past our signer
	targetValidity := validity
	remainingSignerValidity := time.Until(signer.Config.Certs[0].NotAfter)
	if remainingSignerValidity < validity {
		targetValidity = remainingSignerValidity
	}

	certKeyPair, err := certCreator.NewCertificate(signer, targetValidity)
	if err != nil {
		return err
	}

	targetCertKeyPairSecret.Data[corev1.TLSCertKey], targetCertKeyPairSecret.Data[corev1.TLSPrivateKeyKey], err = certKeyPair.GetPEMBytes()
	if err != nil {
		return err
	}
	targetCertKeyPairSecret.Annotations[CertificateNotAfterAnnotation] = certKeyPair.Certs[0].NotAfter.Format(time.RFC3339)
	targetCertKeyPairSecret.Annotations[CertificateNotBeforeAnnotation] = certKeyPair.Certs[0].NotBefore.Format(time.RFC3339)
	targetCertKeyPairSecret.Annotations[CertificateIssuer] = certKeyPair.Certs[0].Issuer.CommonName
	certCreator.SetAnnotations(certKeyPair, targetCertKeyPairSecret.Annotations)

	return nil
}
