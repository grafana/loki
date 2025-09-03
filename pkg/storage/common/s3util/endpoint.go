package s3util

import "strings"

// FormatEndpoint ensures an endpoint has the correct scheme (http:// or https://) based on the insecure flag.
// If the endpoint already has a scheme, it is left unchanged.
// If the endpoint is empty, it returns empty string.
func FormatEndpoint(endpoint string, insecure bool) string {
	if endpoint == "" {
		return ""
	}

	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		if insecure {
			return "http://" + endpoint
		}
		return "https://" + endpoint
	}

	return endpoint
}
