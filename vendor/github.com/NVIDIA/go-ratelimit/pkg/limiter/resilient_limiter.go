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
package limiter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/NVIDIA/go-ratelimit/pkg/config"
)

// Kraft Proposal : ResilientLimiter combines Redis and in-memory limiters for fault tolerance
type ResilientLimiter struct {
	redisLimiter  *DynamicLimiter
	memoryLimiter *MemoryLimiter
	keyBuilder    func(*http.Request) string
	defaultConfig config.ClientConfig

	mu                 sync.RWMutex
	redisAvailable     bool
	lastRedisCheck     time.Time
	redisCheckInterval time.Duration
}

// Kraft Proposal : NewResilientLimiter creates a limiter that falls back to memory if Redis is unavailable
func NewResilientLimiter(
	rdb redis.Cmdable,
	prefix string,
	defaultRequestLimit int,
	defaultByteLimit int64,
	defaultWindow time.Duration,
	keyBuilder func(*http.Request) string,
) *ResilientLimiter {
	// Create the Redis-based limiter
	redisLimiter := NewDynamicLimiter(
		rdb,
		prefix,
		defaultRequestLimit,
		defaultByteLimit,
		defaultWindow,
		keyBuilder,
	)

	// Create the memory-based limiter
	memoryLimiter := NewMemoryLimiter(
		prefix,
		defaultRequestLimit,
		defaultByteLimit,
		defaultWindow,
		keyBuilder,
	)

	// Create default config
	defaultCfg := config.ClientConfig{
		RequestLimit: defaultRequestLimit,
		ByteLimit:    defaultByteLimit,
		WindowSecs:   int(defaultWindow.Seconds()),
	}

	return &ResilientLimiter{
		redisLimiter:       redisLimiter,
		memoryLimiter:      memoryLimiter,
		keyBuilder:         keyBuilder,
		defaultConfig:      defaultCfg,
		redisAvailable:     true, 
		lastRedisCheck:     time.Now(),
		redisCheckInterval: 5 * time.Second, 
	}
}

// checkRedisAvailability tests if Redis is available
func (rl *ResilientLimiter) checkRedisAvailability(ctx context.Context) bool {
	if time.Since(rl.lastRedisCheck) < rl.redisCheckInterval {
		rl.mu.RLock()
		available := rl.redisAvailable
		rl.mu.RUnlock()
		return available
	}

	pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	//check redis and see if its down or not
	_, err := rl.redisLimiter.rdb.Ping(pingCtx).Result()
	available := err == nil

	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.redisAvailable = available
	rl.lastRedisCheck = time.Now()

	return available
}


func (rl *ResilientLimiter) GetConfigManager() *config.ConfigManager {
	return rl.redisLimiter.GetConfigManager()
}

func (rl *ResilientLimiter) AllowRequest(ctx context.Context, r *http.Request) (bool, int64, int64, error) {
	if rl.checkRedisAvailability(ctx) {
		allowed, remaining, reset, err := rl.redisLimiter.AllowRequest(ctx, r)
		if err == nil {
			return allowed, remaining, reset, nil
		}

		fmt.Println("Redis error in AllowRequest, falling back to memory:", err)
	}

	return rl.memoryLimiter.AllowRequest(ctx, r)
}

func (rl *ResilientLimiter) RecordBytes(ctx context.Context, r *http.Request, bytes int64) (bool, int64, int64, error) {
	if rl.checkRedisAvailability(ctx) {
		allowed, remaining, reset, err := rl.redisLimiter.RecordBytes(ctx, r, bytes)
		if err == nil {
			return allowed, remaining, reset, nil
		}

		fmt.Println("Redis error in RecordBytes, falling back to memory:", err)
	}

	return rl.memoryLimiter.RecordBytes(ctx, r, bytes)
}

func (rl *ResilientLimiter) GetClientConfig(ctx context.Context, clientID string) (config.ClientConfig, error) {
	if rl.checkRedisAvailability(ctx) {
		cfg, err := rl.redisLimiter.GetClientConfig(ctx, clientID)
		if err == nil {
			rl.memoryLimiter.SetClientConfig(ctx, clientID, cfg)
			return cfg, nil
		}

		fmt.Println("Redis error in GetClientConfig, falling back to memory:", err)
	}

	return rl.memoryLimiter.GetClientConfig(ctx, clientID)
}

func (rl *ResilientLimiter) SetClientConfig(ctx context.Context, clientID string, cfg config.ClientConfig) error {
	if err := rl.memoryLimiter.SetClientConfig(ctx, clientID, cfg); err != nil {
		return err
	}

	if rl.checkRedisAvailability(ctx) {
		return rl.redisLimiter.SetClientConfig(ctx, clientID, cfg)
	}

	return nil
}

// ResetClientConfig removes custom config for a client, reverting to defaults
func (rl *ResilientLimiter) ResetClientConfig(ctx context.Context, clientID string) error {
	if err := rl.memoryLimiter.ResetClientConfig(ctx, clientID); err != nil {
		return err
	}

	if rl.checkRedisAvailability(ctx) {
		return rl.redisLimiter.ResetClientConfig(ctx, clientID)
	}

	return nil
}

// UpdateDefaultConfig updates the default rate limit configuration
func (rl *ResilientLimiter) UpdateDefaultConfig(ctx context.Context, requestLimit int, byteLimit int64, windowSecs int) error {
	newDefault := config.ClientConfig{
		RequestLimit: requestLimit,
		ByteLimit:    byteLimit,
		WindowSecs:   windowSecs,
	}

	rl.mu.Lock()
	rl.defaultConfig = newDefault
	rl.mu.Unlock()

	if rl.checkRedisAvailability(ctx) {
		if cfgManager := rl.redisLimiter.GetConfigManager(); cfgManager != nil {
			cfgManager.SetDefaultConfig(newDefault)
		}
	}

	return nil
}

// GetDefaultConfig returns the current default configuration
func (rl *ResilientLimiter) GetDefaultConfig() config.ClientConfig {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.defaultConfig
}

// GetCurrentUsage returns the current byte usage for a client without recording new bytes
func (rl *ResilientLimiter) GetCurrentUsage(ctx context.Context, r *http.Request) (int64, error) {
	if rl.checkRedisAvailability(ctx) {
		usage, err := rl.redisLimiter.GetCurrentUsage(ctx, r)
		if err == nil {
			return usage, nil
		}

		fmt.Println("Redis error in GetCurrentUsage, falling back to memory:", err)
	}

	return rl.memoryLimiter.GetCurrentUsage(ctx, r)
}

// GetLastKey returns the key that would be used for this request
func (rl *ResilientLimiter) GetLastKey(r *http.Request) string {
	return rl.keyBuilder(r)
}