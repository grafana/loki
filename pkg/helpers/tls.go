package helpers

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"strings"
)

// NewTLSConfigFromOptions return a tls.Config given parameters provided
func NewTLSConfigFromOptions(url, ca, cert, certKey, certKeyPass string, skipVerify bool) (*tls.Config, error) {

	tlsConfig := tls.Config{}

	// if url schme is https prefixed
	if strings.HasPrefix(url, "https://") {

		tlsConfig.InsecureSkipVerify = skipVerify

		// Add any provided CA
		if ca != "" {
			CACert, err := ioutil.ReadFile(ca)
			if err != nil {
				return nil, err
			}

			capool, err := x509.SystemCertPool()
			if err != nil {
				return nil, fmt.Errorf("unable to read system cert pool: %s", err)
			}

			capool.AppendCertsFromPEM(CACert)

			tlsConfig.RootCAs = capool
		}

		// Add any client certs
		if cert != "" && certKey != "" {

			certPemBytes, err := ioutil.ReadFile(cert)
			if err != nil {
				return nil, fmt.Errorf("unable to read pem file: %s", err)
			}

			keyPemBytes, err := ioutil.ReadFile(certKey)
			if err != nil {
				return nil, fmt.Errorf("unable to read key pem file: %s", err)
			}

			// Parse key
			keyBlock, rest := pem.Decode(keyPemBytes)
			if keyBlock == nil {
				return nil, fmt.Errorf("could not read key data from bytes: '%s'", string(keyPemBytes))
			}

			if len(rest) > 0 {
				return nil, fmt.Errorf("multiple private keys found: this is not supported")
			}

			// Decrypt if needed
			if x509.IsEncryptedPEMBlock(keyBlock) {

				data, err := x509.DecryptPEMBlock(keyBlock, []byte(certKeyPass))
				if err != nil {
					return nil, err
				}

				keyBlock = &pem.Block{
					Type:  keyBlock.Type,
					Bytes: data,
				}
			}

			clientCert, err := tls.X509KeyPair(certPemBytes, pem.EncodeToMemory(keyBlock))
			if err != nil {
				return nil, err
			}

			tlsConfig.Certificates = []tls.Certificate{clientCert}
		}

	}
	return &tlsConfig, nil
}
