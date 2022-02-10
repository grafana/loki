// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package confighttp // import "go.opentelemetry.io/collector/config/confighttp"

import (
	"context"
	"net"
	"net/http"

	"go.opentelemetry.io/collector/client"
)

var _ http.Handler = (*clientInfoHandler)(nil)

// clientInfoHandler is an http.Handler that enhances the incoming request context with client.Info.
type clientInfoHandler struct {
	next http.Handler

	// include client metadata or not
	includeMetadata bool
}

// ServeHTTP intercepts incoming HTTP requests, replacing the request's context with one that contains
// a client.Info containing the client's IP address.
func (h *clientInfoHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req = req.WithContext(contextWithClient(req, h.includeMetadata))
	h.next.ServeHTTP(w, req)
}

// contextWithClient attempts to add the client IP address to the client.Info from the context. When no
// client.Info exists in the context, one is created.
func contextWithClient(req *http.Request, includeMetadata bool) context.Context {
	cl := client.FromContext(req.Context())

	ip := parseIP(req.RemoteAddr)
	if ip != nil {
		cl.Addr = ip
	}

	if includeMetadata {
		md := req.Header.Clone()
		if len(md.Get(client.MetadataHostName)) == 0 && req.Host != "" {
			md.Add(client.MetadataHostName, req.Host)
		}

		cl.Metadata = client.NewMetadata(md)
	}

	ctx := client.NewContext(req.Context(), cl)
	return ctx
}

// parseIP parses the given string for an IP address. The input string might contain the port,
// but must not contain a protocol or path. Suitable for getting the IP part of a client connection.
func parseIP(source string) *net.IPAddr {
	ipstr, _, err := net.SplitHostPort(source)
	if err == nil {
		source = ipstr
	}
	ip := net.ParseIP(source)
	if ip != nil {
		return &net.IPAddr{
			IP: ip,
		}
	}
	return nil
}
