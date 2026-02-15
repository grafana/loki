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
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ClientConfig holds rate limit configuration for a specific client
type ClientConfig struct {
	RequestLimit int   `json:"request_limit"`
	ByteLimit    int64 `json:"byte_limit"`
	WindowSecs   int   `json:"window_secs"`
}

// ConfigManager manages per-client rate limit configurations
type ConfigManager struct {
	rdb        redis.Cmdable
	prefix     string
	defaultCfg ClientConfig
	cache      map[string]ClientConfig
	cacheMu    sync.RWMutex
	cacheTTL   time.Duration
	lastUpdate map[string]time.Time
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(rdb redis.Cmdable, prefix string, defaultCfg ClientConfig) *ConfigManager {
	return &ConfigManager{
		rdb:        rdb,
		prefix:     prefix,
		defaultCfg: defaultCfg,
		cache:      make(map[string]ClientConfig),
		lastUpdate: make(map[string]time.Time),
		cacheTTL:   time.Minute, // Cache configs for 1 minute
	}
}

// configKey returns the Redis key for a client's config
func (cm *ConfigManager) configKey(clientID string) string {
	return fmt.Sprintf("%s:config:%s", cm.prefix, clientID)
}

// GetConfig returns the configuration for a specific client
// It first checks the local cache, then Redis, falling back to defaults if needed
func (cm *ConfigManager) GetConfig(ctx context.Context, clientID string) (ClientConfig, error) {
	// Check cache first
	cm.cacheMu.RLock()
	cfg, found := cm.cache[clientID]
	lastUpdate := cm.lastUpdate[clientID]
	cm.cacheMu.RUnlock()

	// If found in cache and not expired, return it
	if found && time.Since(lastUpdate) < cm.cacheTTL {
		return cfg, nil
	}

	// Try to get from Redis
	configJSON, err := cm.rdb.Get(ctx, cm.configKey(clientID)).Result()
	if err == redis.Nil {
		// No custom config, use default
		return cm.defaultCfg, nil
	} else if err != nil {
		// Redis error, return default but don't cache it
		return cm.defaultCfg, err
	}

	// Parse the JSON config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return cm.defaultCfg, err
	}

	// Update cache
	cm.cacheMu.Lock()
	cm.cache[clientID] = cfg
	cm.lastUpdate[clientID] = time.Now()
	cm.cacheMu.Unlock()

	return cfg, nil
}

// SetConfig sets the configuration for a specific client
func (cm *ConfigManager) SetConfig(ctx context.Context, clientID string, cfg ClientConfig) error {
	// Validate config
	if cfg.RequestLimit <= 0 {
		return fmt.Errorf("request limit must be positive")
	}
	if cfg.ByteLimit <= 0 {
		return fmt.Errorf("byte limit must be positive")
	}
	if cfg.WindowSecs <= 0 {
		return fmt.Errorf("window seconds must be positive")
	}

	// Serialize to JSON
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	// Save to Redis
	if err := cm.rdb.Set(ctx, cm.configKey(clientID), configJSON, 0).Err(); err != nil {
		return err
	}

	// Update cache
	cm.cacheMu.Lock()
	cm.cache[clientID] = cfg
	cm.lastUpdate[clientID] = time.Now()
	cm.cacheMu.Unlock()

	return nil
}

// DeleteConfig removes the custom configuration for a client
func (cm *ConfigManager) DeleteConfig(ctx context.Context, clientID string) error {
	// Remove from Redis
	if err := cm.rdb.Del(ctx, cm.configKey(clientID)).Err(); err != nil {
		return err
	}

	// Remove from cache
	cm.cacheMu.Lock()
	delete(cm.cache, clientID)
	delete(cm.lastUpdate, clientID)
	cm.cacheMu.Unlock()

	return nil
}

// ExtractClientID extracts a client identifier from a request
// This should match the logic used in your key builders
func ExtractClientID(clientID string) string {
	if clientID == "" {
		return ""
	}
	return clientID
}

// SetDefaultConfig updates the default configuration
func (cm *ConfigManager) SetDefaultConfig(defaultCfg ClientConfig) {
	cm.cacheMu.Lock()
	defer cm.cacheMu.Unlock()
	cm.defaultCfg = defaultCfg
}
