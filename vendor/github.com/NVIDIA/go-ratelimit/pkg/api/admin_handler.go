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
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NVIDIA/go-ratelimit/pkg/config"
	"github.com/NVIDIA/go-ratelimit/pkg/limiter"
)

// AdminConfig holds configuration for the admin API
type AdminConfig struct {
	// Enable authentication for admin endpoints
	EnableAuth bool
	// Admin key for authentication (if enabled)
	AdminKey string
	// Redis connection details for status endpoint
	RedisAddress string
	// Default limits for status endpoint
	DefaultRequestLimit int
	DefaultByteLimit    int64
	DefaultWindow       time.Duration
	// Rate limiting mode
	RateLimitByTenant bool
	FallbackToIP      bool
}

// AdminHandler provides enhanced HTTP endpoints for managing rate limits
// This is designed to be compatible with Loki's admin API requirements
type AdminHandler struct {
	limiter *limiter.DynamicLimiter
	config  AdminConfig
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(limiter *limiter.DynamicLimiter, cfg AdminConfig) *AdminHandler {
	return &AdminHandler{
		limiter: limiter,
		config:  cfg,
	}
}

// RegisterRoutes registers all admin API routes
func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux) {
	// Status endpoint
	mux.HandleFunc("/admin/ratelimit/status", h.handleStatus)

	// Tenant/Client management endpoints
	mux.HandleFunc("/admin/ratelimit/tenant/", h.handleTenant)
	mux.HandleFunc("/admin/ratelimit/client/", h.handleClient)

	// Batch operations
	mux.HandleFunc("/admin/ratelimit/batch", h.handleBatch)

	// Legacy compatibility endpoints (compatible with existing config_handler.go)
	mux.HandleFunc("/admin/config/", h.handleLegacyConfig)
}

// authenticate checks if the request is authorized
func (h *AdminHandler) authenticate(r *http.Request) bool {
	if !h.config.EnableAuth {
		return true
	}

	// Check various authentication headers
	adminKey := r.Header.Get("X-Admin-Key")
	if adminKey == "" {
		adminKey = r.Header.Get("Authorization")
		adminKey = strings.TrimPrefix(adminKey, "Bearer ")
	}

	return adminKey == h.config.AdminKey
}

// handleStatus handles GET requests for rate limiting status
func (h *AdminHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	status := map[string]interface{}{
		"rate_limiting_enabled": true,
		"redis_address":         h.config.RedisAddress,
		"default_request_limit": h.config.DefaultRequestLimit,
		"default_byte_limit":    h.config.DefaultByteLimit,
		"default_window":        h.config.DefaultWindow.String(),
		"rate_limit_by_tenant":  h.config.RateLimitByTenant,
		"fallback_to_ip":        h.config.FallbackToIP,
		"service_status":        "initialized",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleTenant handles tenant-specific rate limit operations
func (h *AdminHandler) handleTenant(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract tenant ID from URL path
	// URL format: /admin/ratelimit/tenant/{tenantID}
	path := r.URL.Path
	if !strings.HasPrefix(path, "/admin/ratelimit/tenant/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	tenantID := strings.TrimPrefix(path, "/admin/ratelimit/tenant/")
	if tenantID == "" {
		http.Error(w, "Missing tenant ID", http.StatusBadRequest)
		return
	}

	// Use tenant-prefixed client ID for consistency with Loki
	clientID := fmt.Sprintf("tenant-%s", tenantID)

	switch r.Method {
	case http.MethodGet:
		h.getClientConfig(w, r, clientID, tenantID)
	case http.MethodPut, http.MethodPost:
		h.setTenantConfig(w, r, clientID, tenantID)
	case http.MethodDelete:
		h.resetTenantConfig(w, r, clientID, tenantID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClient handles client-specific rate limit operations (generic clients, not tenants)
func (h *AdminHandler) handleClient(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract client ID from URL path
	// URL format: /admin/ratelimit/client/{clientID}
	path := r.URL.Path
	if !strings.HasPrefix(path, "/admin/ratelimit/client/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	clientID := strings.TrimPrefix(path, "/admin/ratelimit/client/")
	if clientID == "" {
		http.Error(w, "Missing client ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getClientConfig(w, r, clientID, "")
	case http.MethodPut, http.MethodPost:
		h.setClientConfigGeneric(w, r, clientID)
	case http.MethodDelete:
		h.resetClientConfigGeneric(w, r, clientID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getClientConfig retrieves configuration for a client/tenant
func (h *AdminHandler) getClientConfig(w http.ResponseWriter, r *http.Request, clientID string, tenantID string) {
	cfg, err := h.limiter.GetClientConfig(r.Context(), clientID)
	if err != nil {
		// Return defaults if no custom config exists
		cfg = config.ClientConfig{
			RequestLimit: h.config.DefaultRequestLimit,
			ByteLimit:    h.config.DefaultByteLimit,
			WindowSecs:   int(h.config.DefaultWindow.Seconds()),
		}
	}

	response := map[string]interface{}{
		"client_id":     clientID,
		"request_limit": cfg.RequestLimit,
		"byte_limit":    cfg.ByteLimit,
		"window_secs":   cfg.WindowSecs,
	}

	if tenantID != "" {
		response["tenant_id"] = tenantID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// setTenantConfig updates configuration for a tenant
func (h *AdminHandler) setTenantConfig(w http.ResponseWriter, r *http.Request, clientID string, tenantID string) {
	var limits struct {
		RequestLimit int   `json:"request_limit"`
		ByteLimit    int64 `json:"byte_limit"`
		WindowSecs   int   `json:"window_secs,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&limits); err != nil {
		http.Error(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Use default window if not specified
	if limits.WindowSecs == 0 {
		limits.WindowSecs = int(h.config.DefaultWindow.Seconds())
	}

	cfg := config.ClientConfig{
		RequestLimit: limits.RequestLimit,
		ByteLimit:    limits.ByteLimit,
		WindowSecs:   limits.WindowSecs,
	}

	if err := h.limiter.SetClientConfig(r.Context(), clientID, cfg); err != nil {
		http.Error(w, "Error setting config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":        "success",
		"tenant":        tenantID,
		"client_id":     clientID,
		"request_limit": limits.RequestLimit,
		"byte_limit":    limits.ByteLimit,
		"window_secs":   limits.WindowSecs,
		"message":       "Rate limits updated successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// resetTenantConfig resets a tenant to default configuration
func (h *AdminHandler) resetTenantConfig(w http.ResponseWriter, r *http.Request, clientID string, tenantID string) {
	if err := h.limiter.ResetClientConfig(r.Context(), clientID); err != nil {
		http.Error(w, "Error resetting config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":    "success",
		"tenant":    tenantID,
		"client_id": clientID,
		"message":   "Limits reset to defaults",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// setClientConfigGeneric updates configuration for a generic client
func (h *AdminHandler) setClientConfigGeneric(w http.ResponseWriter, r *http.Request, clientID string) {
	var cfg config.ClientConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.limiter.SetClientConfig(r.Context(), clientID, cfg); err != nil {
		http.Error(w, "Error setting config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":        "success",
		"client_id":     clientID,
		"request_limit": cfg.RequestLimit,
		"byte_limit":    cfg.ByteLimit,
		"window_secs":   cfg.WindowSecs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// resetClientConfigGeneric resets a generic client to default configuration
func (h *AdminHandler) resetClientConfigGeneric(w http.ResponseWriter, r *http.Request, clientID string) {
	if err := h.limiter.ResetClientConfig(r.Context(), clientID); err != nil {
		http.Error(w, "Error resetting config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":    "success",
		"client_id": clientID,
		"message":   "Configuration reset to defaults",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// BatchOperation represents a batch operation request
type BatchOperation struct {
	Operation string              `json:"operation"` // "set" or "reset"
	Clients   []BatchClientConfig `json:"clients,omitempty"`
	ClientIDs []string            `json:"client_ids,omitempty"` // For reset operations
}

// BatchClientConfig represents configuration for a client in batch operations
type BatchClientConfig struct {
	ClientID     string `json:"client_id"`
	TenantID     string `json:"tenant_id,omitempty"`
	RequestLimit int    `json:"request_limit"`
	ByteLimit    int64  `json:"byte_limit"`
	WindowSecs   int    `json:"window_secs,omitempty"`
}

// handleBatch handles batch operations for multiple clients/tenants
func (h *AdminHandler) handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var batch BatchOperation
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	results := make([]map[string]interface{}, 0)
	errors := make([]map[string]interface{}, 0)

	switch batch.Operation {
	case "set":
		for _, client := range batch.Clients {
			clientID := client.ClientID
			if client.TenantID != "" {
				clientID = fmt.Sprintf("tenant-%s", client.TenantID)
			}

			windowSecs := client.WindowSecs
			if windowSecs == 0 {
				windowSecs = int(h.config.DefaultWindow.Seconds())
			}

			cfg := config.ClientConfig{
				RequestLimit: client.RequestLimit,
				ByteLimit:    client.ByteLimit,
				WindowSecs:   windowSecs,
			}

			if err := h.limiter.SetClientConfig(ctx, clientID, cfg); err != nil {
				errors = append(errors, map[string]interface{}{
					"client_id": clientID,
					"error":     err.Error(),
				})
			} else {
				result := map[string]interface{}{
					"client_id":     clientID,
					"status":        "success",
					"request_limit": client.RequestLimit,
					"byte_limit":    client.ByteLimit,
					"window_secs":   windowSecs,
				}
				if client.TenantID != "" {
					result["tenant_id"] = client.TenantID
				}
				results = append(results, result)
			}
		}

	case "reset":
		for _, clientID := range batch.ClientIDs {
			if err := h.limiter.ResetClientConfig(ctx, clientID); err != nil {
				errors = append(errors, map[string]interface{}{
					"client_id": clientID,
					"error":     err.Error(),
				})
			} else {
				results = append(results, map[string]interface{}{
					"client_id": clientID,
					"status":    "success",
					"message":   "Reset to defaults",
				})
			}
		}

	default:
		http.Error(w, "Invalid operation. Must be 'set' or 'reset'", http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"status":  "completed",
		"results": results,
	}

	if len(errors) > 0 {
		response["errors"] = errors
		response["status"] = "partial_success"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleLegacyConfig maintains backward compatibility with existing config_handler.go endpoints
func (h *AdminHandler) handleLegacyConfig(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract client ID from URL path
	// URL format: /admin/config/{clientID}
	path := r.URL.Path
	if len(path) <= len("/admin/config/") {
		http.Error(w, "Missing client ID", http.StatusBadRequest)
		return
	}
	clientID := path[len("/admin/config/"):]

	switch r.Method {
	case http.MethodGet:
		h.getClientConfig(w, r, clientID, "")
	case http.MethodPut:
		h.setClientConfigGeneric(w, r, clientID)
	case http.MethodDelete:
		h.resetClientConfigGeneric(w, r, clientID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ExportMetrics returns current rate limiting metrics (for monitoring)
func (h *AdminHandler) ExportMetrics(ctx context.Context) map[string]interface{} {
	// This could be extended to include actual metrics from the limiter
	// For now, return basic status
	return map[string]interface{}{
		"status":                "active",
		"rate_limiting_enabled": true,
		"default_limits": map[string]interface{}{
			"request_limit": h.config.DefaultRequestLimit,
			"byte_limit":    h.config.DefaultByteLimit,
			"window_secs":   h.config.DefaultWindow.Seconds(),
		},
	}
}
