// Configuration for goldfish-mcp server
package main

import (
	"flag"
	"fmt"
	"net/url"
	"time"
)

// Config holds configuration for the goldfish-mcp server
type Config struct {
	APIURL  string
	Timeout time.Duration
}

// RegisterFlags registers command-line flags for configuration.
// Long flags only (--api-url, --timeout), no short flags.
func (c *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(
		&c.APIURL,
		"api-url",
		"http://localhost:3100",
		"Goldfish API base URL (typically via kubectl port-forward)",
	)
	fs.DurationVar(
		&c.Timeout,
		"timeout",
		30*time.Second,
		"HTTP request timeout",
	)
}

// Validate checks that the configuration is valid.
// Returns an error if the configuration is invalid.
func (c *Config) Validate() error {
	// Validate APIURL is not empty
	if c.APIURL == "" {
		return fmt.Errorf("API URL cannot be empty")
	}

	// Validate APIURL is a valid URL
	parsedURL, err := url.Parse(c.APIURL)
	if err != nil {
		return fmt.Errorf("invalid API URL: %w", err)
	}

	// Ensure URL has a scheme
	if parsedURL.Scheme == "" {
		return fmt.Errorf("API URL must include scheme (http:// or https://)")
	}

	// Validate Timeout is at least 1 second
	if c.Timeout < 1*time.Second {
		return fmt.Errorf("timeout must be at least 1s, got %s", c.Timeout)
	}

	return nil
}
