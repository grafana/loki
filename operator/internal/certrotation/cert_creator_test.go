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

func TestCACreator_ReturnErrorOnMissingIssuer(t *testing.T) {
	c := CACreator{}
	_, err := c.NewCertificate(1 * time.Hour)
	require.ErrorIs(t, err, errMissingIssuer)
}

func TestCACreator_SetAnnotations(t *testing.T) {
	now := time.Now()
	nowFn := func() time.Time { return now }

	nowCA, err := newTestCACertificate(pkix.Name{CommonName: "creator-tests"}, int64(1), 200*time.Minute, nowFn)
	require.NoError(t, err)

	c := CACreator{}

	annotations := map[string]string{}
	c.SetAnnotations(nowCA.Config, annotations)

	require.Len(t, annotations, 3)
	require.Contains(t, annotations, CertificateIssuer)
	require.Contains(t, annotations, CertificateNotBeforeAnnotation)
	require.Contains(t, annotations, CertificateNotAfterAnnotation)
}

func TestCACreator_NeedNewCertificate(t *testing.T) {
	now := time.Now()
	invalidNotAfter, _ := time.Parse(time.RFC3339, "")
	invalidNotBefore, _ := time.Parse(time.RFC3339, "")

	tt := []struct {
		desc        string
		annotations map[string]string
		refresh     time.Duration
		wantReason  string
	}{
		{
			desc: "already expired",
			annotations: map[string]string{
				CertificateIssuer:              "creator-tests",
				CertificateNotAfterAnnotation:  invalidNotAfter.Format(time.RFC3339),
				CertificateNotBeforeAnnotation: invalidNotBefore.Format(time.RFC3339),
			},
			refresh:    2 * time.Minute,
			wantReason: "already expired",
		},
		{
			desc: "refresh only when expired",
			annotations: map[string]string{
				CertificateIssuer:              "creator-tests",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			refresh: 90 * time.Minute,
		},
		{
			desc: "at 80 percent validity",
			annotations: map[string]string{
				CertificateIssuer:              "creator-tests",
				CertificateNotAfterAnnotation:  now.Add(18 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-72 * time.Minute).Format(time.RFC3339),
			},
			refresh:    40 * time.Minute,
			wantReason: "past its latest possible time",
		},
		{
			desc: "past its refresh time",
			annotations: map[string]string{
				CertificateIssuer:              "creator-tests",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			refresh:    40 * time.Minute,
			wantReason: "past its refresh time",
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c := CACreator{}
			reason := c.NeedNewCertificate(tc.annotations, tc.refresh)
			require.Contains(t, reason, tc.wantReason)
		})
	}
}

func TestCertCreator_ReturnErrorOnMissingUserInfo(t *testing.T) {
	c := CertCreator{}
	_, err := c.NewCertificate(nil, 1*time.Hour)
	require.ErrorIs(t, err, errMissingUserInfo)
}

func TestCertCreator_ReturnErrorOnMissingHostnames(t *testing.T) {
	c := CertCreator{UserInfo: defaultUserInfo}
	_, err := c.NewCertificate(nil, 1*time.Hour)
	require.ErrorIs(t, err, errMissingHostnames)
}

func TestCertCreator_CertHasRequiredExtensions(t *testing.T) {
	now := time.Now()
	nowFn := func() time.Time { return now }

	nowCA, err := newTestCACertificate(pkix.Name{CommonName: "creator-tests"}, int64(1), 200*time.Minute, nowFn)
	require.NoError(t, err)

	c := CertCreator{
		UserInfo:  defaultUserInfo,
		Hostnames: []string{"example.org"},
	}
	cert, err := c.NewCertificate(nowCA, 1*time.Hour)
	require.NoError(t, err)

	require.Contains(t, cert.Certs[0].ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	require.Contains(t, cert.Certs[0].ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	require.Equal(t, defaultUserInfo.GetName(), cert.Certs[0].Subject.CommonName)
	require.Equal(t, defaultUserInfo.GetUID(), cert.Certs[0].Subject.SerialNumber)
	require.Equal(t, defaultUserInfo.GetGroups(), cert.Certs[0].Subject.Organization)
}

func TestCertCreator_SetAnnotations(t *testing.T) {
	now := time.Now()
	nowFn := func() time.Time { return now }

	nowCA, err := newTestCACertificate(pkix.Name{CommonName: "creator-tests"}, int64(1), 200*time.Minute, nowFn)
	require.NoError(t, err)

	c := CertCreator{Hostnames: []string{"example.org"}}

	annotations := map[string]string{}
	c.SetAnnotations(nowCA.Config, annotations)

	require.Len(t, annotations, 4)
	require.Contains(t, annotations, CertificateIssuer)
	require.Contains(t, annotations, CertificateNotBeforeAnnotation)
	require.Contains(t, annotations, CertificateNotAfterAnnotation)
	require.Contains(t, annotations, CertificateHostnames)
}

func TestCertCreator_NeedNewCertificate(t *testing.T) {
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
				CertificateIssuer:              "creator-tests",
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
				CertificateIssuer:              "creator-tests",
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
				CertificateIssuer:              "creator-tests",
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
				CertificateIssuer:              "creator-tests",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return twentyMinutesBeforeCA, nil
			},
			refresh:    40 * time.Minute,
			wantReason: "past its refresh time",
		},
		{
			desc: "missing issuer name",
			annotations: map[string]string{
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return nowCA, nil
			},
			refresh:    70 * time.Minute,
			wantReason: "missing issuer name",
		},
		{
			desc: "issuer not in ca bundle",
			annotations: map[string]string{
				CertificateIssuer:              "issuer-not-in-any-ca",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return nowCA, nil
			},
			refresh:    70 * time.Minute,
			wantReason: `issuer "issuer-not-in-any-ca", not in ca bundle`,
		},
		{
			desc: "missing hostnames",
			annotations: map[string]string{
				CertificateIssuer:              "creator-tests",
				CertificateNotAfterAnnotation:  now.Add(45 * time.Minute).Format(time.RFC3339),
				CertificateNotBeforeAnnotation: now.Add(-45 * time.Minute).Format(time.RFC3339),
			},
			signerFn: func() (*crypto.CA, error) {
				return nowCA, nil
			},
			refresh:    70 * time.Minute,
			wantReason: "are required and not existing",
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			rawCA, err := tc.signerFn()
			require.NoError(t, err)

			c := CertCreator{Hostnames: []string{"a.b.c.d", "e.d.f.g"}}
			reason := c.NeedNewCertificate(tc.annotations, rawCA, rawCA.Config.Certs, tc.refresh)
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
