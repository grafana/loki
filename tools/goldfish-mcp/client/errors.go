package client

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// IsConnectionError checks if the error is a connection-related error
// (network unreachable, connection refused, timeout, etc.)
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for url.Error (typically wraps connection errors)
	if urlErr, ok := err.(*url.Error); ok {
		// Check if it's a timeout
		if urlErr.Timeout() {
			return true
		}
		// Check the underlying error
		err = urlErr.Err
	}

	// Check for net.OpError (most network errors)
	if _, ok := err.(*net.OpError); ok {
		// Connection refused, network unreachable, etc.
		return true
	}

	// Check for syscall errors
	if sysErr, ok := err.(syscall.Errno); ok {
		// ECONNREFUSED, ENETUNREACH, ETIMEDOUT, etc.
		switch sysErr {
		case syscall.ECONNREFUSED,
			syscall.ENETUNREACH,
			syscall.ETIMEDOUT,
			syscall.ECONNRESET,
			syscall.EPIPE:
			return true
		}
	}

	// Check error message as fallback
	errMsg := strings.ToLower(err.Error())
	connectionErrorStrings := []string{
		"connection refused",
		"no such host",
		"network is unreachable",
		"connection reset",
		"broken pipe",
		"timeout",
		"deadline exceeded",
	}

	for _, substr := range connectionErrorStrings {
		if strings.Contains(errMsg, substr) {
			return true
		}
	}

	return false
}

// FormatConnectionError formats a connection error with helpful setup instructions
func FormatConnectionError(apiURL, endpoint string, err error) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Error: Cannot connect to Goldfish API at %s%s\n\n", apiURL, endpoint))
	sb.WriteString("Please ensure the port-forward is active:\n")
	sb.WriteString("  kubectl port-forward --context <context> --namespace <namespace> svc/query-frontend 3100:3100\n\n")
	sb.WriteString(fmt.Sprintf("Connection error: %v\n", err))

	return sb.String()
}

// FormatAPIError formats an HTTP error response with helpful troubleshooting info
func FormatAPIError(statusCode int, errorMsg, _ string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Error: Goldfish API returned error (HTTP %d)\n\n", statusCode))

	if errorMsg != "" {
		sb.WriteString(fmt.Sprintf("Response: %s\n\n", errorMsg))
	}

	// Add specific troubleshooting for common errors
	switch statusCode {
	case 404:
		sb.WriteString("Check that:\n")
		sb.WriteString("  1. Goldfish is enabled in Loki configuration\n")
		sb.WriteString("  2. You're connecting to the correct service (query-frontend)\n")
		sb.WriteString("  3. The endpoint path is correct\n")
	case 500, 502, 503, 504:
		sb.WriteString("The Goldfish service may be experiencing issues.\n")
		sb.WriteString("Check the Loki query-frontend logs for details.\n")
	}

	return sb.String()
}

// FormatResultNotFoundError formats an error for when query results are not available
func FormatResultNotFoundError(correlationID string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Error: Query results not available for correlation ID %s\n\n", correlationID))
	sb.WriteString("Reason: Results were not persisted to object storage.\n\n")
	sb.WriteString("This can happen when:\n")
	sb.WriteString("  1. Result persistence is disabled in Goldfish config\n")
	sb.WriteString("  2. The query was sampled before persistence was enabled\n")
	sb.WriteString("  3. Results have expired and been deleted\n\n")
	sb.WriteString("You can still see query metadata and statistics using the list_queries tool.\n")

	return sb.String()
}
