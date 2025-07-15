package kafka

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"os"

	promconfig "github.com/prometheus/common/config"
	"github.com/xdg-go/scram"
)

func createTLSConfig(cfg promconfig.TLSConfig) (*tls.Config, error) {
	tc := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify, //#nosec G402 -- User has explicitly requested to disable TLS
		ServerName:         cfg.ServerName,
	}
	// load ca cert
	if len(cfg.CAFile) > 0 {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tc.RootCAs = caCertPool
	}
	// load client cert
	if len(cfg.CertFile) > 0 && len(cfg.KeyFile) > 0 {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		tc.Certificates = []tls.Certificate{cert}
	}
	return tc, nil
}

// copied from https://github.com/Shopify/sarama/blob/44627b731c60bb90efe25573e7ef2b3f8df3fa23/examples/sasl_scram_client/scram_client.go
var (
	SHA256 scram.HashGeneratorFcn = sha256.New
	SHA512 scram.HashGeneratorFcn = sha512.New
)

// XDGSCRAMClient implements sarama.SCRAMClient
type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
