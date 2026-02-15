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
	"encoding/json"
	"net/http"

	"github.com/NVIDIA/go-ratelimit/pkg/config"
	"github.com/NVIDIA/go-ratelimit/pkg/limiter"
)

// ConfigHandler provides HTTP endpoints for managing client rate limit configurations
type ConfigHandler struct {
	limiter  *limiter.DynamicLimiter
	adminKey string
}

// NewConfigHandler creates a new configuration handler
func NewConfigHandler(limiter *limiter.DynamicLimiter) *ConfigHandler {
	return &ConfigHandler{
		limiter:  limiter,
		adminKey: "admin-secret-key", // Default admin key
	}
}

// SetAdminKey sets the admin key used for authentication
func (h *ConfigHandler) SetAdminKey(key string) {
	h.adminKey = key
}

// RegisterRoutes registers the configuration API routes
func (h *ConfigHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/config/", h.handleConfigRequest)
}

// handleConfigRequest handles all configuration-related requests
func (h *ConfigHandler) handleConfigRequest(w http.ResponseWriter, r *http.Request) {
	// Check admin authentication
	adminKey := r.Header.Get("X-Admin-Key")
	if adminKey != h.adminKey {
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
		h.getConfig(w, r, clientID)
	case http.MethodPut:
		h.setConfig(w, r, clientID)
	case http.MethodDelete:
		h.resetConfig(w, r, clientID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getConfig returns the current configuration for a client
func (h *ConfigHandler) getConfig(w http.ResponseWriter, r *http.Request, clientID string) {
	cfg, err := h.limiter.GetClientConfig(r.Context(), clientID)
	if err != nil {
		http.Error(w, "Error retrieving config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// setConfig updates the configuration for a client
func (h *ConfigHandler) setConfig(w http.ResponseWriter, r *http.Request, clientID string) {
	var cfg config.ClientConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.limiter.SetClientConfig(r.Context(), clientID, cfg); err != nil {
		http.Error(w, "Error setting config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// resetConfig removes the custom configuration for a client
func (h *ConfigHandler) resetConfig(w http.ResponseWriter, r *http.Request, clientID string) {
	if err := h.limiter.ResetClientConfig(r.Context(), clientID); err != nil {
		http.Error(w, "Error resetting config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
