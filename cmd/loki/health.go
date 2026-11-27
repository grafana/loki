package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/util/cfg"
)

const (
	healthFlag       = "health"
	defaultHealthURL = "http://localhost:3100/ready"
	healthTimeout    = 5 * time.Second
)

type healthFlags struct {
	health          bool
	healthURL       string
	configFile      string
	configExpandEnv bool
	tlsCert         string
	tlsKey          string
	tlsCA           string
	skipVerify      bool
}

func parseHealthFlags(args []string) *healthFlags {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.Usage = func() {}

	f := &healthFlags{}
	fs.BoolVar(&f.health, "health", false, "")
	fs.StringVar(&f.healthURL, "health.url", "", "")
	fs.StringVar(&f.configFile, "config.file", "", "")
	fs.BoolVar(&f.configExpandEnv, "config.expand-env", false, "")
	fs.StringVar(&f.tlsCert, "health.tls.cert", "", "")
	fs.StringVar(&f.tlsKey, "health.tls.key", "", "")
	fs.StringVar(&f.tlsCA, "health.tls.ca", "", "")
	fs.BoolVar(&f.skipVerify, "health.tls.skip-verify", false, "")

	for len(args) > 0 {
		err := fs.Parse(args)
		if err == nil {
			break
		}
		remaining := fs.Args()
		if len(remaining) == 0 {
			break
		}
		if !strings.HasPrefix(remaining[0], "-") {
			args = remaining[1:]
		} else {
			args = remaining
		}
	}
	return f
}

// CheckHealth checks if args contain the -health flag
func CheckHealth(args []string) bool {
	f := parseHealthFlags(args)
	return f.health
}

// RunHealthCheck performs a health check against the /ready endpoint.
// Returns exit code 0 if healthy, 1 if unhealthy.
func RunHealthCheck(args []string) int {
	f := parseHealthFlags(args)
	serverCfg, err := loadServerHealthConfig(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		return 1
	}

	url := getHealthURL(f, serverCfg)

	tlsConfig, err := getTLSConfig(f, serverCfg)
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

// loadServerHealthConfig parses server TLS/address fields from the config file,
// using dynamic unmarshaller to handle environment variables and defaults.
func loadServerHealthConfig(f *healthFlags) (*loki.Config, error) {
	if f.configFile == "" {
		return nil, nil
	}

	// We construct a clean set of arguments containing only standard config flags
	// that we've already parsed successfully using the flag package.
	// This avoids passing health-check specific flags to Loki's dynamic config loader.
	var loaderArgs []string
	if f.configFile != "" {
		loaderArgs = append(loaderArgs, "-config.file="+f.configFile)
	}
	if f.configExpandEnv {
		loaderArgs = append(loaderArgs, "-config.expand-env=true")
	}

	// We use a fresh FlagSet to parse defaults and config file without contaminating the global flag.CommandLine.
	fs := flag.NewFlagSet("health-config-loader", flag.ContinueOnError)
	fs.Usage = func() {}

	var wrapper loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&wrapper, loaderArgs, fs); err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return &wrapper.Config, nil
}

// getHealthURL extracts the URL from args or derives it from the server config.
func getHealthURL(f *healthFlags, serverCfg *loki.Config) string {
	if f.healthURL != "" {
		return f.healthURL
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
		return fmt.Sprintf("%s://%s/ready", scheme, net.JoinHostPort(addr, strconv.Itoa(port)))
	}

	if f.tlsCert != "" || f.tlsCA != "" {
		return "https://localhost:3100/ready"
	}

	return defaultHealthURL
}

// getTLSConfig builds a *tls.Config for the health check client.
// Explicit flags take priority; falls back to server config when none are set.
func getTLSConfig(f *healthFlags, serverCfg *loki.Config) (*tls.Config, error) {
	cert := f.tlsCert
	key := f.tlsKey
	ca := f.tlsCA
	skip := f.skipVerify

	if serverCfg != nil && cert == "" && key == "" && ca == "" && !skip {
		ca = serverCfg.Server.HTTPTLSConfig.ClientCAs
		if serverCfg.Server.HTTPTLSConfig.TLSCertPath != "" && ca == "" {
			return nil, fmt.Errorf("server TLS is enabled but client CA is not configured; use -health.tls.skip-verify to skip verification")
		}
	}

	if skip && ca != "" {
		return nil, fmt.Errorf("cannot use both -health.tls.skip-verify and -health.tls.ca")
	}

	if cert == "" && key == "" && ca == "" && !skip {
		return nil, nil
	}

	cfg := &tls.Config{
		InsecureSkipVerify: skip, //nolint:gosec
		MinVersion:         tls.VersionTLS13,
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
