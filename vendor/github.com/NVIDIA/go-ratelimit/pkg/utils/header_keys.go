/*
 * Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package utils

import (
	"fmt"
	"net/http"
	"strings"
)

// HeaderKeyOptions provides configuration for header-based key extraction
type HeaderKeyOptions struct {
	// HeaderName is the name of the header to extract the key from
	HeaderName string

	// Prefix is an optional prefix to add to the extracted value
	// This helps in namespacing keys in Redis
	Prefix string

	// DefaultValue is used when the header is missing or empty
	// If empty, the client's IP address will be used as fallback
	DefaultValue string

	// CaseSensitive determines if header values should be case-sensitive
	// Default is false (case-insensitive)
	CaseSensitive bool

	// AllowEmpty determines if empty header values are allowed
	// If false and header is empty, DefaultValue or IP will be used
	AllowEmpty bool

	// Sanitizer is an optional function to clean/validate the header value
	// This can be used to remove special characters, validate format, etc.
	Sanitizer func(string) string
}

// KeyByHeader returns a key extraction function that uses a specific HTTP header
// This is useful for rate limiting by API key, tenant ID, customer ID, etc.
//
// Example:
//
//	// Rate limit by API key
//	keyFunc := KeyByHeader("X-API-Key")
//
//	// Rate limit by tenant with prefix
//	keyFunc := KeyByHeaderWithOptions(HeaderKeyOptions{
//	    HeaderName: "X-Tenant-ID",
//	    Prefix: "tenant",
//	})
func KeyByHeader(headerName string) func(*http.Request) string {
	return KeyByHeaderWithOptions(HeaderKeyOptions{
		HeaderName: headerName,
	})
}

// KeyByHeaderWithOptions returns a key extraction function with advanced options
func KeyByHeaderWithOptions(opts HeaderKeyOptions) func(*http.Request) string {
	// Validate options
	if opts.HeaderName == "" {
		panic("HeaderName cannot be empty")
	}

	return func(r *http.Request) string {
		// Extract header value
		value := r.Header.Get(opts.HeaderName)

		// Handle case sensitivity
		if !opts.CaseSensitive {
			value = strings.ToLower(value)
		}

		// Apply sanitizer if provided
		if opts.Sanitizer != nil {
			value = opts.Sanitizer(value)
		}

		// Handle empty values
		if value == "" && !opts.AllowEmpty {
			if opts.DefaultValue != "" {
				value = opts.DefaultValue
			} else {
				// Fallback to IP-based key
				return fmt.Sprintf("header-%s-fallback:%s",
					strings.ToLower(opts.HeaderName),
					ClientIP(r))
			}
		}

		// Build the final key
		if opts.Prefix != "" {
			return fmt.Sprintf("%s:%s", opts.Prefix, value)
		}

		return value
	}
}

// KeyByMultipleHeaders returns a key extraction function that combines multiple headers
// This is useful when you need composite keys (e.g., tenant + endpoint)
//
// Example:
//
//	// Rate limit by tenant AND API version
//	keyFunc := KeyByMultipleHeaders([]string{"X-Tenant-ID", "X-API-Version"}, ":")
func KeyByMultipleHeaders(headerNames []string, separator string) func(*http.Request) string {
	if len(headerNames) == 0 {
		panic("At least one header name must be provided")
	}

	if separator == "" {
		separator = ":"
	}

	return func(r *http.Request) string {
		var parts []string
		allEmpty := true

		for _, headerName := range headerNames {
			value := r.Header.Get(headerName)
			if value != "" {
				allEmpty = false
				parts = append(parts, value)
			} else {
				parts = append(parts, "none")
			}
		}

		// If all headers are empty, use IP as fallback
		if allEmpty {
			return fmt.Sprintf("multi-header-fallback:%s", ClientIP(r))
		}

		return strings.Join(parts, separator)
	}
}

// KeyByHeaderWithFallback returns a key extraction function that tries multiple headers in order
// This is useful when clients might send the same information in different headers
//
// Example:
//
//	// Try X-API-Key first, then Authorization, then fall back to IP
//	keyFunc := KeyByHeaderWithFallback([]string{"X-API-Key", "Authorization"})
func KeyByHeaderWithFallback(headerNames []string) func(*http.Request) string {
	if len(headerNames) == 0 {
		panic("At least one header name must be provided")
	}

	return func(r *http.Request) string {
		for _, headerName := range headerNames {
			value := r.Header.Get(headerName)
			if value != "" {
				return fmt.Sprintf("header:%s:%s",
					strings.ToLower(headerName),
					value)
			}
		}

		// No header found, use IP as fallback
		return fmt.Sprintf("header-fallback:%s", ClientIP(r))
	}
}

// Common sanitizers for header values

// AlphanumericSanitizer removes all non-alphanumeric characters
func AlphanumericSanitizer(value string) string {
	var result strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// UUIDSanitizer validates and normalizes UUID format
func UUIDSanitizer(value string) string {
	// Remove hyphens and convert to lowercase
	cleaned := strings.ToLower(strings.ReplaceAll(value, "-", ""))

	// Basic UUID validation (32 hex characters)
	if len(cleaned) != 32 {
		return ""
	}

	for _, r := range cleaned {
		if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
			return ""
		}
	}

	return cleaned
}

// MaxLengthSanitizer returns a sanitizer that truncates values to a maximum length
func MaxLengthSanitizer(maxLength int) func(string) string {
	return func(value string) string {
		if len(value) > maxLength {
			return value[:maxLength]
		}
		return value
	}
}
