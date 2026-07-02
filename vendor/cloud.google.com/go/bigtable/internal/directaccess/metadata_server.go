// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package directaccess

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
)

const metadataBaseURL = "http://metadata.google.internal/computeMetadata/v1/"
const metadataIPURL = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ip"
const metadataIPv6URL = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ipv6s"

// CheckMetadataServerReachability performs a basic connectivity check to the GCE metadata server.
func CheckMetadataServerReachability() error {
	btopt.Debugf(nil, "directaccess: Dialing metadata server at %s", metadataBaseURL)
	req, err := http.NewRequest("GET", metadataBaseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create metadata request: %w", err)
	}
	req.Header.Add("Metadata-Flavor", "Google")

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to GCE Metadata Server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reachable but returned status code: %d", resp.StatusCode)
	}
	btopt.Debugf(nil, "directaccess: Metadata server is reachable (200 OK)")
	return nil
}

// FetchIPFromMetadataServer fetches the assigned IP address from the metadata server.
func FetchIPFromMetadataServer(addrFamilyStr string) (*net.IP, error) {
	var metadataServerURL string
	switch addrFamilyStr {
	case "IPv4":
		metadataServerURL = metadataIPURL
	case "IPv6":
		metadataServerURL = metadataIPv6URL
	default:
		return nil, fmt.Errorf("invalid address family %v", addrFamilyStr)
	}

	btopt.Debugf(nil, "directaccess: Fetching %s address from %s", addrFamilyStr, metadataServerURL)
	req, err := http.NewRequest("GET", metadataServerURL, nil)
	if err != nil {
		btopt.Debugf(nil, "directaccess: Failed to execute metadata HTTP request for %s: %v", addrFamilyStr, err)
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 200 {
		address := net.ParseIP(strings.TrimSpace(string(body)))
		if address == nil {
			btopt.Debugf(nil, "directaccess: Failed to parse metadata response as valid IP: %s", string(body))
			return nil, fmt.Errorf("failed to parse IP: %s", string(body))
		}
		btopt.Debugf(nil, "directaccess: Successfully fetched %s address: %s", addrFamilyStr, address.String())
		return &address, nil
	}
	btopt.Debugf(nil, "directaccess: Metadata server returned %d for %s fetch", resp.StatusCode, addrFamilyStr)
	return nil, fmt.Errorf("received status code %d", resp.StatusCode)
}
