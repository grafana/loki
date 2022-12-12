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

package web

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/activation"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	config_util "github.com/prometheus/common/config"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
)

var (
	errNoTLSConfig = errors.New("TLS config is not present")
)

type Config struct {
	TLSConfig  TLSConfig                     `yaml:"tls_server_config"`
	HTTPConfig HTTPConfig                    `yaml:"http_server_config"`
	Users      map[string]config_util.Secret `yaml:"basic_auth_users"`
}

type TLSConfig struct {
	TLSCertPath              string     `yaml:"cert_file"`
	TLSKeyPath               string     `yaml:"key_file"`
	ClientAuth               string     `yaml:"client_auth_type"`
	ClientCAs                string     `yaml:"client_ca_file"`
	CipherSuites             []Cipher   `yaml:"cipher_suites"`
	CurvePreferences         []Curve    `yaml:"curve_preferences"`
	MinVersion               TLSVersion `yaml:"min_version"`
	MaxVersion               TLSVersion `yaml:"max_version"`
	PreferServerCipherSuites bool       `yaml:"prefer_server_cipher_suites"`
}

type FlagConfig struct {
	WebListenAddresses *[]string
	WebSystemdSocket   *bool
	WebConfigFile      *string
}

// SetDirectory joins any relative file paths with dir.
func (t *TLSConfig) SetDirectory(dir string) {
	t.TLSCertPath = config_util.JoinDir(dir, t.TLSCertPath)
	t.TLSKeyPath = config_util.JoinDir(dir, t.TLSKeyPath)
	t.ClientCAs = config_util.JoinDir(dir, t.ClientCAs)
}

type HTTPConfig struct {
	HTTP2  bool              `yaml:"http2"`
	Header map[string]string `yaml:"headers,omitempty"`
}

func getConfig(configPath string) (*Config, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	c := &Config{
		TLSConfig: TLSConfig{
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
		},
		HTTPConfig: HTTPConfig{HTTP2: true},
	}
	err = yaml.UnmarshalStrict(content, c)
	if err == nil {
		err = validateHeaderConfig(c.HTTPConfig.Header)
	}
	c.TLSConfig.SetDirectory(filepath.Dir(configPath))
	return c, err
}

func getTLSConfig(configPath string) (*tls.Config, error) {
	c, err := getConfig(configPath)
	if err != nil {
		return nil, err
	}
	return ConfigToTLSConfig(&c.TLSConfig)
}

// ConfigToTLSConfig generates the golang tls.Config from the TLSConfig struct.
func ConfigToTLSConfig(c *TLSConfig) (*tls.Config, error) {
	if c.TLSCertPath == "" && c.TLSKeyPath == "" && c.ClientAuth == "" && c.ClientCAs == "" {
		return nil, errNoTLSConfig
	}

	if c.TLSCertPath == "" {
		return nil, errors.New("missing cert_file")
	}

	if c.TLSKeyPath == "" {
		return nil, errors.New("missing key_file")
	}

	loadCert := func() (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(c.TLSCertPath, c.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load X509KeyPair: %w", err)
		}
		return &cert, nil
	}

	// Confirm that certificate and key paths are valid.
	if _, err := loadCert(); err != nil {
		return nil, err
	}

	cfg := &tls.Config{
		MinVersion:               (uint16)(c.MinVersion),
		MaxVersion:               (uint16)(c.MaxVersion),
		PreferServerCipherSuites: c.PreferServerCipherSuites,
	}

	cfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return loadCert()
	}

	var cf []uint16
	for _, c := range c.CipherSuites {
		cf = append(cf, (uint16)(c))
	}
	if len(cf) > 0 {
		cfg.CipherSuites = cf
	}

	var cp []tls.CurveID
	for _, c := range c.CurvePreferences {
		cp = append(cp, (tls.CurveID)(c))
	}
	if len(cp) > 0 {
		cfg.CurvePreferences = cp
	}

	if c.ClientCAs != "" {
		clientCAPool := x509.NewCertPool()
		clientCAFile, err := os.ReadFile(c.ClientCAs)
		if err != nil {
			return nil, err
		}
		clientCAPool.AppendCertsFromPEM(clientCAFile)
		cfg.ClientCAs = clientCAPool
	}

	switch c.ClientAuth {
	case "RequestClientCert":
		cfg.ClientAuth = tls.RequestClientCert
	case "RequireAnyClientCert", "RequireClientCert": // Preserved for backwards compatibility.
		cfg.ClientAuth = tls.RequireAnyClientCert
	case "VerifyClientCertIfGiven":
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	case "RequireAndVerifyClientCert":
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	case "", "NoClientCert":
		cfg.ClientAuth = tls.NoClientCert
	default:
		return nil, errors.New("Invalid ClientAuth: " + c.ClientAuth)
	}

	if c.ClientCAs != "" && cfg.ClientAuth == tls.NoClientCert {
		return nil, errors.New("Client CA's have been configured without a Client Auth Policy")
	}

	return cfg, nil
}

// ServeMultiple starts the server on the given listeners. The FlagConfig is
// also passed on to Serve.
func ServeMultiple(listeners []net.Listener, server *http.Server, flags *FlagConfig, logger log.Logger) error {
	errs := new(errgroup.Group)
	for _, l := range listeners {
		l := l
		errs.Go(func() error {
			return Serve(l, server, flags, logger)
		})
	}
	return errs.Wait()
}

// ListenAndServe starts the server on addresses given in WebListenAddresses in
// the FlagConfig or instead uses systemd socket activated listeners if
// WebSystemdSocket in the FlagConfig is true. The FlagConfig is also passed on
// to ServeMultiple.
func ListenAndServe(server *http.Server, flags *FlagConfig, logger log.Logger) error {
	if *flags.WebSystemdSocket {
		level.Info(logger).Log("msg", "Listening on systemd activated listeners instead of port listeners.")
		listeners, err := activation.Listeners()
		if err != nil {
			return err
		}
		if len(listeners) < 1 {
			return errors.New("no socket activation file descriptors found")
		}
		return ServeMultiple(listeners, server, flags, logger)
	}
	listeners := make([]net.Listener, 0, len(*flags.WebListenAddresses))
	for _, address := range *flags.WebListenAddresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		defer listener.Close()
		listeners = append(listeners, listener)
	}
	return ServeMultiple(listeners, server, flags, logger)
}

// Server starts the server on the given listener. Based on the file path
// WebConfigFile in the FlagConfig, TLS or basic auth could be enabled.
func Serve(l net.Listener, server *http.Server, flags *FlagConfig, logger log.Logger) error {
	level.Info(logger).Log("msg", "Listening on", "address", l.Addr().String())
	tlsConfigPath := *flags.WebConfigFile
	if tlsConfigPath == "" {
		level.Info(logger).Log("msg", "TLS is disabled.", "http2", false, "address", l.Addr().String())
		return server.Serve(l)
	}

	if err := validateUsers(tlsConfigPath); err != nil {
		return err
	}

	// Setup basic authentication.
	var handler http.Handler = http.DefaultServeMux
	if server.Handler != nil {
		handler = server.Handler
	}

	c, err := getConfig(tlsConfigPath)
	if err != nil {
		return err
	}

	server.Handler = &webHandler{
		tlsConfigPath: tlsConfigPath,
		logger:        logger,
		handler:       handler,
		cache:         newCache(),
	}

	config, err := ConfigToTLSConfig(&c.TLSConfig)
	switch err {
	case nil:
		if !c.HTTPConfig.HTTP2 {
			server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
		}
		// Valid TLS config.
		level.Info(logger).Log("msg", "TLS is enabled.", "http2", c.HTTPConfig.HTTP2, "address", l.Addr().String())
	case errNoTLSConfig:
		// No TLS config, back to plain HTTP.
		level.Info(logger).Log("msg", "TLS is disabled.", "http2", false, "address", l.Addr().String())
		return server.Serve(l)
	default:
		// Invalid TLS config.
		return err
	}

	server.TLSConfig = config

	// Set the GetConfigForClient method of the HTTPS server so that the config
	// and certs are reloaded on new connections.
	server.TLSConfig.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		config, err := getTLSConfig(tlsConfigPath)
		if err != nil {
			return nil, err
		}
		config.NextProtos = server.TLSConfig.NextProtos
		return config, nil
	}
	return server.ServeTLS(l, "", "")
}

// Validate configuration file by reading the configuration and the certificates.
func Validate(tlsConfigPath string) error {
	if tlsConfigPath == "" {
		return nil
	}
	if err := validateUsers(tlsConfigPath); err != nil {
		return err
	}
	c, err := getConfig(tlsConfigPath)
	if err != nil {
		return err
	}
	_, err = ConfigToTLSConfig(&c.TLSConfig)
	if err == errNoTLSConfig {
		return nil
	}
	return err
}

type Cipher uint16

func (c *Cipher) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	err := unmarshal((*string)(&s))
	if err != nil {
		return err
	}
	for _, cs := range tls.CipherSuites() {
		if cs.Name == s {
			*c = (Cipher)(cs.ID)
			return nil
		}
	}
	return errors.New("unknown cipher: " + s)
}

func (c Cipher) MarshalYAML() (interface{}, error) {
	return tls.CipherSuiteName((uint16)(c)), nil
}

type Curve tls.CurveID

var curves = map[string]Curve{
	"CurveP256": (Curve)(tls.CurveP256),
	"CurveP384": (Curve)(tls.CurveP384),
	"CurveP521": (Curve)(tls.CurveP521),
	"X25519":    (Curve)(tls.X25519),
}

func (c *Curve) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	err := unmarshal((*string)(&s))
	if err != nil {
		return err
	}
	if curveid, ok := curves[s]; ok {
		*c = curveid
		return nil
	}
	return errors.New("unknown curve: " + s)
}

func (c *Curve) MarshalYAML() (interface{}, error) {
	for s, curveid := range curves {
		if *c == curveid {
			return s, nil
		}
	}
	return fmt.Sprintf("%v", c), nil
}

type TLSVersion uint16

var tlsVersions = map[string]TLSVersion{
	"TLS13": (TLSVersion)(tls.VersionTLS13),
	"TLS12": (TLSVersion)(tls.VersionTLS12),
	"TLS11": (TLSVersion)(tls.VersionTLS11),
	"TLS10": (TLSVersion)(tls.VersionTLS10),
}

func (tv *TLSVersion) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	err := unmarshal((*string)(&s))
	if err != nil {
		return err
	}
	if v, ok := tlsVersions[s]; ok {
		*tv = v
		return nil
	}
	return errors.New("unknown TLS version: " + s)
}

func (tv *TLSVersion) MarshalYAML() (interface{}, error) {
	for s, v := range tlsVersions {
		if *tv == v {
			return s, nil
		}
	}
	return fmt.Sprintf("%v", tv), nil
}

// Listen starts the server on the given address. Based on the file
// tlsConfigPath, TLS or basic auth could be enabled.
//
// Deprecated: Use ListenAndServe instead.
func Listen(server *http.Server, flags *FlagConfig, logger log.Logger) error {
	return ListenAndServe(server, flags, logger)
}
