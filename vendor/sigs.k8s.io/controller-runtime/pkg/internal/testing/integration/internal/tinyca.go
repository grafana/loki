package internal

// NB(directxman12): nothing has verified that this has good settings.  In fact,
// the setting generated here are probably terrible, but they're fine for integration
// tests.  These ABSOLUTELY SHOULD NOT ever be exposed in the public API.  They're
// ONLY for use with envtest's ability to configure webhook testing.
// If I didn't otherwise not want to add a dependency on cfssl, I'd just use that.

import (
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	certutil "k8s.io/client-go/util/cert"
)

var (
	rsaKeySize = 2048 // a decent number, as of 2019
	bigOne     = big.NewInt(1)
)

// CertPair is a private key and certificate for use for client auth, as a CA, or serving.
type CertPair struct {
	Key  crypto.Signer
	Cert *x509.Certificate
}

// CertBytes returns the PEM-encoded version of the certificate for this pair.
func (k CertPair) CertBytes() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: k.Cert.Raw,
	})
}

// AsBytes encodes keypair in the appropriate formats for on-disk storage (PEM and
// PKCS8, respectively).
func (k CertPair) AsBytes() (cert []byte, key []byte, err error) {
	cert = k.CertBytes()

	rawKeyData, err := x509.MarshalPKCS8PrivateKey(k.Key)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to encode private key: %v", err)
	}

	key = pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: rawKeyData,
	})

	return cert, key, nil
}

// TinyCA supports signing serving certs and client-certs,
// and can be used as an auth mechanism with envtest.
type TinyCA struct {
	CA      CertPair
	orgName string

	nextSerial *big.Int
}

// newPrivateKey generates a new private key of a relatively sane size (see
// rsaKeySize).
func newPrivateKey() (crypto.Signer, error) {
	return rsa.GenerateKey(crand.Reader, rsaKeySize)
}

// NewTinyCA creates a new a tiny CA utility for provisioning serving certs and client certs FOR TESTING ONLY.
// Don't use this for anything else!
func NewTinyCA() (*TinyCA, error) {
	caPrivateKey, err := newPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("unable to generate private key for CA: %v", err)
	}
	caCfg := certutil.Config{CommonName: "envtest-environment", Organization: []string{"envtest"}}
	caCert, err := certutil.NewSelfSignedCACert(caCfg, caPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("unable to generate certificate for CA: %v", err)
	}

	return &TinyCA{
		CA:         CertPair{Key: caPrivateKey, Cert: caCert},
		orgName:    "envtest",
		nextSerial: big.NewInt(1),
	}, nil
}

func (c *TinyCA) makeCert(cfg certutil.Config) (CertPair, error) {
	now := time.Now()

	key, err := newPrivateKey()
	if err != nil {
		return CertPair{}, fmt.Errorf("unable to create private key: %v", err)
	}

	serial := new(big.Int).Set(c.nextSerial)
	c.nextSerial.Add(c.nextSerial, bigOne)

	template := x509.Certificate{
		Subject:      pkix.Name{CommonName: cfg.CommonName, Organization: cfg.Organization},
		DNSNames:     cfg.AltNames.DNSNames,
		IPAddresses:  cfg.AltNames.IPs,
		SerialNumber: serial,

		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: cfg.Usages,

		// technically not necessary for testing, but let's set anyway just in case.
		NotBefore: now.UTC(),
		// 1 week -- the default for cfssl, and just long enough for a
		// long-term test, but not too long that anyone would try to use this
		// seriously.
		NotAfter: now.Add(168 * time.Hour).UTC(),
	}

	certRaw, err := x509.CreateCertificate(crand.Reader, &template, c.CA.Cert, key.Public(), c.CA.Key)
	if err != nil {
		return CertPair{}, fmt.Errorf("unable to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certRaw)
	if err != nil {
		return CertPair{}, fmt.Errorf("generated invalid certificate, could not parse: %v", err)
	}

	return CertPair{
		Key:  key,
		Cert: cert,
	}, nil
}

// NewServingCert returns a new CertPair for a serving HTTPS on localhost (or other specified names).
func (c *TinyCA) NewServingCert(names ...string) (CertPair, error) {
	if len(names) == 0 {
		names = []string{"localhost"}
	}
	dnsNames, ips, err := resolveNames(names)
	if err != nil {
		return CertPair{}, err
	}

	return c.makeCert(certutil.Config{
		CommonName:   "localhost",
		Organization: []string{c.orgName},
		AltNames: certutil.AltNames{
			DNSNames: dnsNames,
			IPs:      ips,
		},
		Usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
}

func resolveNames(names []string) ([]string, []net.IP, error) {
	dnsNames := []string{}
	ips := []net.IP{}
	for _, name := range names {
		if name == "" {
			continue
		}
		ip := net.ParseIP(name)
		if ip == nil {
			dnsNames = append(dnsNames, name)
			// Also resolve to IPs.
			nameIPs, err := net.LookupHost(name)
			if err != nil {
				return nil, nil, err
			}
			for _, nameIP := range nameIPs {
				ip = net.ParseIP(nameIP)
				if ip != nil {
					ips = append(ips, ip)
				}
			}
		} else {
			ips = append(ips, ip)
		}
	}
	return dnsNames, ips, nil
}
