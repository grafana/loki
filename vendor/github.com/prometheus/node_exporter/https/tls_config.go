// Copyright 2019 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package https allows the implementation of TLS.
package https

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type Config struct {
	TLSConfig TLSStruct `yaml:"tls_config"`
}

type TLSStruct struct {
	TLSCertPath string `yaml:"cert_file"`
	TLSKeyPath  string `yaml:"key_file"`
	ClientAuth  string `yaml:"client_auth_type"`
	ClientCAs   string `yaml:"client_ca_file"`
}

func getTLSConfig(configPath string) (*tls.Config, error) {
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	err = yaml.Unmarshal(content, c)
	if err != nil {
		return nil, err
	}
	return ConfigToTLSConfig(&c.TLSConfig)
}

// ConfigToTLSConfig generates the golang tls.Config from the TLSStruct config.
func ConfigToTLSConfig(c *TLSStruct) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if len(c.TLSCertPath) == 0 {
		return nil, errors.New("missing TLSCertPath")
	}
	if len(c.TLSKeyPath) == 0 {
		return nil, errors.New("missing TLSKeyPath")
	}
	loadCert := func() (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(c.TLSCertPath, c.TLSKeyPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to load X509KeyPair")
		}
		return &cert, nil
	}
	// Confirm that certificate and key paths are valid.
	if _, err := loadCert(); err != nil {
		return nil, err
	}
	cfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return loadCert()
	}

	if len(c.ClientCAs) > 0 {
		clientCAPool := x509.NewCertPool()
		clientCAFile, err := ioutil.ReadFile(c.ClientCAs)
		if err != nil {
			return nil, err
		}
		clientCAPool.AppendCertsFromPEM(clientCAFile)
		cfg.ClientCAs = clientCAPool
	}
	if len(c.ClientAuth) > 0 {
		switch s := (c.ClientAuth); s {
		case "NoClientCert":
			cfg.ClientAuth = tls.NoClientCert
		case "RequestClientCert":
			cfg.ClientAuth = tls.RequestClientCert
		case "RequireClientCert":
			cfg.ClientAuth = tls.RequireAnyClientCert
		case "VerifyClientCertIfGiven":
			cfg.ClientAuth = tls.VerifyClientCertIfGiven
		case "RequireAndVerifyClientCert":
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		case "":
			cfg.ClientAuth = tls.NoClientCert
		default:
			return nil, errors.New("Invalid ClientAuth: " + s)
		}
	}
	if len(c.ClientCAs) > 0 && cfg.ClientAuth == tls.NoClientCert {
		return nil, errors.New("Client CA's have been configured without a Client Auth Policy")
	}
	return cfg, nil
}

// Listen starts the server on the given address. If tlsConfigPath isn't empty the server connection will be started using TLS.
func Listen(server *http.Server, tlsConfigPath string) error {
	if (tlsConfigPath) == "" {
		return server.ListenAndServe()
	}
	var err error
	server.TLSConfig, err = getTLSConfig(tlsConfigPath)
	if err != nil {
		return err
	}
	// Set the GetConfigForClient method of the HTTPS server so that the config
	// and certs are reloaded on new connections.
	server.TLSConfig.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		return getTLSConfig(tlsConfigPath)
	}
	return server.ListenAndServeTLS("", "")
}
