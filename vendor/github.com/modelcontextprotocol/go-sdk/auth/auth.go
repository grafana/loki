// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// TokenInfo holds information from a bearer token.
type TokenInfo struct {
	Scopes     []string
	Expiration time.Time
	// UserID is an optional identifier for the authenticated user.
	// If set by a TokenVerifier, it can be used by transports to prevent
	// session hijacking by ensuring that all requests for a given session
	// come from the same user.
	UserID string
	// TODO: add standard JWT fields
	Extra map[string]any
}

// The error that a TokenVerifier should return if the token cannot be verified.
var ErrInvalidToken = errors.New("invalid token")

// The error that a TokenVerifier should return for OAuth-specific protocol errors.
var ErrOAuth = errors.New("oauth error")

// A TokenVerifier checks the validity of a bearer token, and extracts information
// from it. If verification fails, it should return an error that unwraps to ErrInvalidToken.
// The HTTP request is provided in case verifying the token involves checking it.
type TokenVerifier func(ctx context.Context, token string, req *http.Request) (*TokenInfo, error)

// RequireBearerTokenOptions are options for [RequireBearerToken].
type RequireBearerTokenOptions struct {
	// The URL for the resource server metadata OAuth flow, to be returned as part
	// of the WWW-Authenticate header.
	ResourceMetadataURL string
	// The required scopes.
	Scopes []string
}

type tokenInfoKey struct{}

// TokenInfoFromContext returns the [TokenInfo] stored in ctx, or nil if none.
func TokenInfoFromContext(ctx context.Context) *TokenInfo {
	ti := ctx.Value(tokenInfoKey{})
	if ti == nil {
		return nil
	}
	return ti.(*TokenInfo)
}

// RequireBearerToken returns a piece of middleware that verifies a bearer token using the verifier.
// If verification succeeds, the [TokenInfo] is added to the request's context and the request proceeds.
// If verification fails, the request fails with a 401 Unauthenticated, and the WWW-Authenticate header
// is populated to enable [protected resource metadata].
//
// [protected resource metadata]: https://datatracker.ietf.org/doc/rfc9728
func RequireBearerToken(verifier TokenVerifier, opts *RequireBearerTokenOptions) func(http.Handler) http.Handler {
	// Based on typescript-sdk/src/server/auth/middleware/bearerAuth.ts.

	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenInfo, errmsg, code := verify(r, verifier, opts)
			if code != 0 {
				if code == http.StatusUnauthorized || code == http.StatusForbidden {
					if opts != nil && opts.ResourceMetadataURL != "" {
						w.Header().Add("WWW-Authenticate", "Bearer resource_metadata="+opts.ResourceMetadataURL)
					}
				}
				http.Error(w, errmsg, code)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), tokenInfoKey{}, tokenInfo))
			handler.ServeHTTP(w, r)
		})
	}
}

func verify(req *http.Request, verifier TokenVerifier, opts *RequireBearerTokenOptions) (_ *TokenInfo, errmsg string, code int) {
	// Extract bearer token.
	authHeader := req.Header.Get("Authorization")
	fields := strings.Fields(authHeader)
	if len(fields) != 2 || strings.ToLower(fields[0]) != "bearer" {
		return nil, "no bearer token", http.StatusUnauthorized
	}

	// Verify the token and get information from it.
	tokenInfo, err := verifier(req.Context(), fields[1], req)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			return nil, err.Error(), http.StatusUnauthorized
		}
		if errors.Is(err, ErrOAuth) {
			return nil, err.Error(), http.StatusBadRequest
		}
		return nil, err.Error(), http.StatusInternalServerError
	}

	// Check scopes. All must be present.
	if opts != nil {
		// Note: quadratic, but N is small.
		for _, s := range opts.Scopes {
			if !slices.Contains(tokenInfo.Scopes, s) {
				return nil, "insufficient scope", http.StatusForbidden
			}
		}
	}

	// Check expiration.
	if tokenInfo.Expiration.IsZero() {
		return nil, "token missing expiration", http.StatusUnauthorized
	}
	if tokenInfo.Expiration.Before(time.Now()) {
		return nil, "token expired", http.StatusUnauthorized
	}
	return tokenInfo, "", 0
}

// ProtectedResourceMetadataHandler returns an http.Handler that serves OAuth 2.0
// protected resource metadata (RFC 9728) with CORS support.
//
// This handler allows cross-origin requests from any origin (Access-Control-Allow-Origin: *)
// because OAuth metadata is public information intended for client discovery (RFC 9728 §3.1).
// The metadata contains only non-sensitive configuration data about authorization servers
// and supported scopes.
//
// No validation of metadata fields is performed; ensure metadata accuracy at configuration time.
//
// For more sophisticated CORS policies or to restrict origins, wrap this handler with a
// CORS middleware like github.com/rs/cors or github.com/jub0bs/cors.
func ProtectedResourceMetadataHandler(metadata *oauthex.ProtectedResourceMetadata) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers for cross-origin client discovery.
		// OAuth metadata is public information, so allowing any origin is safe.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle CORS preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Only GET allowed for metadata retrieval
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			http.Error(w, "Failed to encode metadata", http.StatusInternalServerError)
			return
		}
	})
}
