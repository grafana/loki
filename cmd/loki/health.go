package main

import (
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

	client := &http.Client{
		Timeout: healthTimeout,
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
	return defaultHealthURL
}
