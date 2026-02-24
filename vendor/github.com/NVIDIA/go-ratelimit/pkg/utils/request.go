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
	"net"
	"net/http"
	"strings"
)

// KeyByIP returns a key based only on client IP
func KeyByIP(r *http.Request) string {
	return "ip:" + ClientIP(r)
}

// KeyByClientID returns a key based on client ID
// The client ID should be extracted by the API implementation after authentication
func KeyByClientID(r *http.Request, clientID string) string {
	if clientID == "" {
		// Fallback to IP if no client ID provided
		return "ip:" + ClientIP(r)
	}
	return "client:" + clientID
}

// KeyByClientIDAndRoute returns a key based on client ID and route
// The client ID should be extracted by the API implementation after authentication
func KeyByClientIDAndRoute(r *http.Request, clientID string) string {
	seg := RouteBucket(r.URL.Path)
	if clientID == "" {
		// Fallback to IP if no client ID provided
		return "ip:" + ClientIP(r) + "|route:" + seg
	}
	return "client:" + clientID + "|route:" + seg
}

// RouteBucket normalizes a path to a small set of buckets
func RouteBucket(path string) string {
	// normalize to a small set of buckets like /v1/users, /v1/orders, etc.
	p := strings.Trim(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "/"
	}
	if len(parts) >= 2 {
		return "/" + parts[0] + "/" + parts[1]
	}
	return "/" + parts[0]
}

// ClientIP extracts the client IP address from an HTTP request
func ClientIP(r *http.Request) string {
	// Honor X-Forwarded-For (trust only if set by your proxy/load balancer)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
