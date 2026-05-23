package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	healthFlag       = "health"
	defaultHealthURL = "http://localhost:3100/ready"
	healthTimeout    = 5 * time.Second
)

type serverHealthConfig struct {
	Server struct {
		HTTPListenAddress string `yaml:"http_listen_address"`
		HTTPListenPort    int    `yaml:"http_listen_port"`
		HTTPTLSConfig     struct {
			TLSCertPath string `yaml:"cert_file"`
			TLSKeyPath  string `yaml:"key_file"`
			ClientCAs   string `yaml:"client_ca_file"`
		} `yaml:"http_tls_config"`
	} `yaml:"server"`
}

// CheckHealth checks if args contain the -health flag
func CheckHealth(args []string) bool {
	pattern := regexp.MustCompile(`^-+` + healthFlag + `$`)
	for _, a := range args {
		if pattern.MatchString(a) {
			return true
		}
	}
	return false
}

// RunHealthCheck performs a health check against the /ready endpoint.
// Returns exit code 0 if healthy, 1 if unhealthy.
func RunHealthCheck(args []string) int {
	serverCfg := loadServerHealthConfig(args)

	url := getHealthURL(args, serverCfg)

	tlsConfig, err := getTLSConfig(args, serverCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check TLS config error: %v\n", err)
		return 1
	}

	client := &http.Client{
		Timeout: healthTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Loki is healthy")
		return 0
	}

	fmt.Fprintf(os.Stderr, "Loki is unhealthy: status code %d\n", resp.StatusCode)
	return 1
}

// loadServerHealthConfig parses server TLS/address fields from the -config.file arg.
func loadServerHealthConfig(args []string) *serverHealthConfig {
	cfgFile := getArgValue(args, "config.file")
	if cfgFile == "" {
		return nil
	}

	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil
	}

	var cfg serverHealthConfig
	cfg.Server.HTTPListenPort = 3100
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// getHealthURL extracts the URL from args or derives it from the server config.
func getHealthURL(args []string, serverCfg *serverHealthConfig) string {
	urlPattern := regexp.MustCompile(`^-+health\.url[=:]?(.*)$`)
	argPattern := regexp.MustCompile(`^-`)

	for i, a := range args {
		if matches := urlPattern.FindStringSubmatch(a); matches != nil {
			if matches[1] != "" {
				return matches[1]
			}
			if i+1 < len(args) && !argPattern.MatchString(args[i+1]) {
				return args[i+1]
			}
		}
	}

	if serverCfg != nil {
		scheme := "http"
		if serverCfg.Server.HTTPTLSConfig.TLSCertPath != "" {
			scheme = "https"
		}
		addr := serverCfg.Server.HTTPListenAddress
		if addr == "" {
			addr = "localhost"
		}
		port := serverCfg.Server.HTTPListenPort
		if port == 0 {
			port = 3100
		}
		return fmt.Sprintf("%s://%s/ready", scheme, addr+":"+strconv.Itoa(port))
	}

	if getArgValue(args, "health.tls.cert") != "" || getArgValue(args, "health.tls.ca") != "" {
		return "https://localhost:3100/ready"
	}

	return defaultHealthURL
}

// getTLSConfig builds a *tls.Config for the health check client.
// Explicit flags take priority; falls back to server config when none are set.
func getTLSConfig(args []string, serverCfg *serverHealthConfig) (*tls.Config, error) {
	cert := getArgValue(args, "health.tls.cert")
	key := getArgValue(args, "health.tls.key")
	ca := getArgValue(args, "health.tls.ca")
	skip := hasFlag(args, "health.tls.skip-verify")

	if serverCfg != nil && cert == "" && key == "" && ca == "" && !skip {
		ca = serverCfg.Server.HTTPTLSConfig.ClientCAs
		if serverCfg.Server.HTTPTLSConfig.TLSCertPath != "" && ca == "" {
			skip = true
		}
	}

	if cert == "" && key == "" && ca == "" && !skip {
		return nil, nil
	}

	cfg := &tls.Config{
		InsecureSkipVerify: skip, //nolint:gosec
	}

	if ca != "" {
		caCert, err := os.ReadFile(ca)
		if err != nil {
			return nil, fmt.Errorf("reading CA certificate %q: %w", ca, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("no valid PEM certificates found in CA file %q", ca)
		}
		cfg.RootCAs = pool
	}

	if cert != "" || key != "" {
		if cert == "" || key == "" {
			return nil, fmt.Errorf("both -health.tls.cert and -health.tls.key must be provided together")
		}
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("loading client key pair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}

	return cfg, nil
}

// getArgValue extracts a named flag's value from args (-flag=value or -flag value).
func getArgValue(args []string, flag string) string {
	pattern := regexp.MustCompile(`^-+` + regexp.QuoteMeta(flag) + `[=:]?(.*)$`)
	argPattern := regexp.MustCompile(`^-`)

	for i, a := range args {
		if matches := pattern.FindStringSubmatch(a); matches != nil {
			if matches[1] != "" {
				return matches[1]
			}
			if i+1 < len(args) && !argPattern.MatchString(args[i+1]) {
				return args[i+1]
			}
		}
	}
	return ""
}

// hasFlag checks if a boolean flag is present in args
func hasFlag(args []string, flag string) bool {
	pattern := regexp.MustCompile(`^-+` + regexp.QuoteMeta(flag) + `$`)
	for _, a := range args {
		if pattern.MatchString(a) {
			return true
		}
	}
	return false
}
