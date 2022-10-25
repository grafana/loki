package certrotation

import (
	stdcrypto "crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestNeedNewTargetCertKeyPairForTime(t *testing.T) {
	now := time.Now()
	nowFn := func() time.Time { return now }

	twentyMinutesBeforeNow := time.Now().Add(-20 * time.Minute)
	twentyMinutesBeforeNowFn := func() time.Time { return twentyMinutesBeforeNow }

	invalidNotAfter, _ := time.Parse(time.RFC3339, "")
	invalidNotBefore, _ := time.Parse(time.RFC3339, "")

	// A default test CA
	nowCA, err := newTestCACertificate(pkix.Name{CommonName: "creator-tests"}, int64(1), 200*time.Minute, nowFn)
	require.NoError(t, err)

	twentyMinutesBeforeCA, err := newTestCACertificate(pkix.Name{CommonName: "creator-tests"}, int64(1), 200*time.Minute, twentyMinutesBeforeNowFn)
	require.NoError(t, err)

	tt := []struct {
		desc        string
		annotations map[string]string
		signerFn    func() (*crypto.CA, error)
		refresh     time.Duration
		wantReason  string
	}{
		{
			desc: "already expired",
			annotations: map[string]string{
				CertificateIssuer:              "dev_ns@signing-ca@10000",
				CertificateNotAfterAnnotation:  invalidNotAfter.Format(time.RFC3339),
				CertificateNotBeforeAnnotation: invalidNotBefore.Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return nowCA, nil
			},
			refresh:    2 * time.Minute,
			wantReason: "already expired",
		},
		{
			desc: "refresh only when expired",
			annotations: map[string]string{
				CertificateIssuer:              "dev_ns@signing-ca@10000",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return nowCA, nil
			},
			refresh: 90 * time.Minute,
		},
		{
			desc: "at 80 percent validity",
			annotations: map[string]string{
				CertificateIssuer:              "dev_ns@signing-ca@10000",
				CertificateNotAfterAnnotation:  now.Add(18 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-72 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return nowCA, nil
			},
			refresh:    40 * time.Minute,
			wantReason: "past its latest possible time",
		},
		{
			desc: "past its refresh time",
			annotations: map[string]string{
				CertificateIssuer:              "dev_ns@signing-ca@10000",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return twentyMinutesBeforeCA, nil
			},
			refresh:    40 * time.Minute,
			wantReason: "past its refresh time",
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			rawCA, err := tc.signerFn()
			require.NoError(t, err)

			reason := needNewTargetCertKeyPairForTime(tc.annotations, rawCA, tc.refresh)
			require.Contains(t, reason, tc.wantReason)
		})
	}
}

func newTestCACertificate(subject pkix.Name, serialNumber int64, validity time.Duration, currentTime func() time.Time) (*crypto.CA, error) {
	caPublicKey, caPrivateKey, err := crypto.NewKeyPair()
	if err != nil {
		return nil, err
	}

	caCert := &x509.Certificate{
		Subject: subject,

		SignatureAlgorithm: x509.SHA256WithRSA,

		NotBefore:    currentTime().Add(-1 * time.Second),
		NotAfter:     currentTime().Add(validity),
		SerialNumber: big.NewInt(serialNumber),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	cert, err := signCertificate(caCert, caPublicKey, caCert, caPrivateKey)
	if err != nil {
		return nil, err
	}

	return &crypto.CA{
		Config: &crypto.TLSCertificateConfig{
			Certs: []*x509.Certificate{cert},
			Key:   caPrivateKey,
		},
		SerialGenerator: &crypto.RandomSerialGenerator{},
	}, nil
}

func signCertificate(template *x509.Certificate, requestKey stdcrypto.PublicKey, issuer *x509.Certificate, issuerKey stdcrypto.PrivateKey) (*x509.Certificate, error) {
	derBytes, err := x509.CreateCertificate(rand.Reader, template, issuer, requestKey, issuerKey)
	if err != nil {
		return nil, err
	}
	certs, err := x509.ParseCertificates(derBytes)
	if err != nil {
		return nil, err
	}
	if len(certs) != 1 {
		return nil, errors.New("Expected a single certificate")
	}
	return certs[0], nil
}
