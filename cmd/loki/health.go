package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"
)

const (
	healthFlag       = "health"
	defaultHealthURL = "http://localhost:3100/ready"
	healthTimeout    = 5 * time.Second
)

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

// RunHealthCheck performs a health check against the /ready endpoint
// Returns exit code 0 if healthy, 1 if unhealthy
func RunHealthCheck(args []string) int {
	url := getHealthURL(args)
	tlsConfig, err := getTLSConfig(args)
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

// getHealthURL extracts the URL from args or returns default
// Looks for -health.url=<url> or -health.url <url>
func getHealthURL(args []string) string {
	urlPattern := regexp.MustCompile(`^-+health\.url[=:]?(.*)$`)
	healthArgPattern := regexp.MustCompile(`^-`)

	for i, a := range args {
		if matches := urlPattern.FindStringSubmatch(a); matches != nil {
			if matches[1] != "" {
				return matches[1]
			}
			// Check next argument for the URL value
			if i+1 < len(args) && !healthArgPattern.MatchString(args[i+1]) {
				return args[i+1]
			}
		}
	}

	if getArgValue(args, "health.tls.cert") != "" || getArgValue(args, "health.tls.ca") != "" || hasFlag(args, "health.tls.skip-verify") {
		return "https://localhost:3100/ready"
	}
	return defaultHealthURL
}

// getTLSConfig reads -health.tls.cert, -health.tls.key, -health.tls.ca, -health.tls.skip-verify from args
func getTLSConfig(args []string) (*tls.Config, error) {
	cert := getArgValue(args, "health.tls.cert")
	key := getArgValue(args, "health.tls.key")
	ca := getArgValue(args, "health.tls.ca")
	skip := hasFlag(args, "health.tls.skip-verify")

	if cert == "" && key == "" && ca == "" && !skip {
		return nil, nil
	}

	cfg := &tls.Config{
		InsecureSkipVerify: skip, //nolint:gosec // user explicitly opted in via -health.tls.skip-verify flag
	}

	if ca != "" {
		caCert, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("no certificates found in %s", ca)
		}
		cfg.RootCAs = pool
	}

	if cert != "" && key != "" {
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{pair}
	} else if cert != "" || key != "" {
		return nil, fmt.Errorf("both health.tls.cert and health.tls.key must be set")
	}

	return cfg, nil
}

// getArgValue extracts a named flag's value from args
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
