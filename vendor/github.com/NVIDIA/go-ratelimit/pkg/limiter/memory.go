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
	"net/http"
	"sync"
	"time"

	"github.com/NVIDIA/go-ratelimit/pkg/config"
)

// MemoryLimiter implements a sliding-window rate limiter using in-memory storage.
type MemoryLimiter struct {
	prefix       string
	requestLimit int           // N
	byteLimit    int64         // Byte limit
	window       time.Duration // e.g., 1 * time.Minute
	keyBuilder   func(*http.Request) string

	mu              sync.RWMutex
	requestWindows  map[string][]time.Time
	byteWindows     map[string][]ByteRecord
	clientConfigs   map[string]config.ClientConfig
	defaultConfig   config.ClientConfig
	lastCleanup     time.Time
	cleanupInterval time.Duration
}

// ByteRecord represents a byte usage record with a timestamp
type ByteRecord struct {
	Bytes     int64
	Timestamp time.Time
}

// NewMemoryLimiter creates an in-memory sliding-window limiter.
func NewMemoryLimiter(
	prefix string,
	requestLimit int,
	byteLimit int64,
	window time.Duration,
	keyBuilder func(*http.Request) string,
) *MemoryLimiter {
	defaultConfig := config.ClientConfig{
		RequestLimit: requestLimit,
		ByteLimit:    byteLimit,
		WindowSecs:   int(window.Seconds()),
	}

	return &MemoryLimiter{
		prefix:          prefix,
		requestLimit:    requestLimit,
		byteLimit:       byteLimit,
		window:          window,
		keyBuilder:      keyBuilder,
		requestWindows:  make(map[string][]time.Time),
		byteWindows:     make(map[string][]ByteRecord),
		clientConfigs:   make(map[string]config.ClientConfig),
		defaultConfig:   defaultConfig,
		lastCleanup:     time.Now(),
		cleanupInterval: time.Minute, // Cleanup old entries every minute
	}
}

// cleanup removes expired entries from the sliding windows
func (ml *MemoryLimiter) cleanup() {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-ml.window)

	// Only run cleanup at most once per cleanupInterval
	if now.Sub(ml.lastCleanup) < ml.cleanupInterval {
		return
	}
	ml.lastCleanup = now

	// Clean request windows
	for key, times := range ml.requestWindows {
		var validTimes []time.Time
		for _, t := range times {
			if t.After(cutoff) {
				validTimes = append(validTimes, t)
			}
		}
		if len(validTimes) > 0 {
			ml.requestWindows[key] = validTimes
		} else {
			delete(ml.requestWindows, key)
		}
	}

	// Clean byte windows
	for key, records := range ml.byteWindows {
		var validRecords []ByteRecord
		for _, record := range records {
			if record.Timestamp.After(cutoff) {
				validRecords = append(validRecords, record)
			}
		}
		if len(validRecords) > 0 {
			ml.byteWindows[key] = validRecords
		} else {
			delete(ml.byteWindows, key)
		}
	}
}

// extractClientID extracts the client identifier from the key
func (ml *MemoryLimiter) extractClientID(key string) string {
	return extractClientID(key) // Reuse the existing function
}

// GetClientConfig returns the current config for a client
func (ml *MemoryLimiter) GetClientConfig(_ context.Context, clientID string) (config.ClientConfig, error) {
	ml.mu.RLock()
	defer ml.mu.RUnlock()

	if cfg, ok := ml.clientConfigs[clientID]; ok {
		return cfg, nil
	}
	return ml.defaultConfig, nil
}

// SetClientConfig sets a custom config for a client
func (ml *MemoryLimiter) SetClientConfig(_ context.Context, clientID string, cfg config.ClientConfig) error {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	ml.clientConfigs[clientID] = cfg
	return nil
}

// ResetClientConfig removes custom config for a client, reverting to defaults
func (ml *MemoryLimiter) ResetClientConfig(_ context.Context, clientID string) error {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	delete(ml.clientConfigs, clientID)
	return nil
}

// AllowRequest checks if a new request is allowed based on client-specific config
func (ml *MemoryLimiter) AllowRequest(ctx context.Context, r *http.Request) (bool, int64, int64, error) {
	// Run cleanup to remove expired entries
	ml.cleanup()

	// Get the key for this request
	k := ml.keyBuilder(r)

	// Extract client ID for config lookup
	clientID := ml.extractClientID(k)

	// Get client-specific config
	cfg, _ := ml.GetClientConfig(ctx, clientID)

	ml.mu.Lock()
	defer ml.mu.Unlock()

	// Get the current time
	now := time.Now()
	cutoff := now.Add(-time.Duration(cfg.WindowSecs) * time.Second)

	// Get current requests in the window
	times, exists := ml.requestWindows[k]
	if !exists {
		times = []time.Time{}
		ml.requestWindows[k] = times
	}

	// Count valid requests in the window
	var validTimes []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			validTimes = append(validTimes, t)
		}
	}

	// If request limiting is disabled, allow the request
	if cfg.RequestLimit < 0 {
		// Add this request to the window
		ml.requestWindows[k] = append(validTimes, now)
		return true, 999999, int64(cfg.WindowSecs), nil
	}

	// Check if we're under the limit
	if len(validTimes) < cfg.RequestLimit {
		// Add this request to the window
		ml.requestWindows[k] = append(validTimes, now)
		remaining := int64(cfg.RequestLimit - len(validTimes) - 1)
		return true, remaining, int64(cfg.WindowSecs), nil
	}

	// We're at the limit, calculate reset time
	var resetSecs int64 = int64(cfg.WindowSecs)
	if len(validTimes) > 0 {
		oldestTime := validTimes[0]
		for _, t := range validTimes {
			if t.Before(oldestTime) {
				oldestTime = t
			}
		}
		resetDuration := oldestTime.Add(time.Duration(cfg.WindowSecs) * time.Second).Sub(now)
		resetSecs = int64(resetDuration.Seconds())
		if resetSecs < 0 {
			resetSecs = 0
		}
	}

	return false, 0, resetSecs, nil
}

// RecordBytes records the bytes used for a request with client-specific config
func (ml *MemoryLimiter) RecordBytes(ctx context.Context, r *http.Request, bytes int64) (bool, int64, int64, error) {
	// Run cleanup to remove expired entries
	ml.cleanup()

	// Get the key for this request
	k := ml.keyBuilder(r)

	// Extract client ID for config lookup
	clientID := ml.extractClientID(k)

	// Get client-specific config
	cfg, _ := ml.GetClientConfig(ctx, clientID)

	ml.mu.Lock()
	defer ml.mu.Unlock()

	// Get the current time
	now := time.Now()
	cutoff := now.Add(-time.Duration(cfg.WindowSecs) * time.Second)

	// Get current byte records in the window
	records, exists := ml.byteWindows[k]
	if !exists {
		records = []ByteRecord{}
		ml.byteWindows[k] = records
	}

	// Count valid bytes in the window
	var validRecords []ByteRecord
	var totalBytes int64
	for _, record := range records {
		if record.Timestamp.After(cutoff) {
			validRecords = append(validRecords, record)
			totalBytes += record.Bytes
		}
	}

	// Check if adding these bytes would exceed the limit
	if totalBytes+bytes <= cfg.ByteLimit {
		// Add these bytes to the window
		ml.byteWindows[k] = append(validRecords, ByteRecord{
			Bytes:     bytes,
			Timestamp: now,
		})
		remaining := cfg.ByteLimit - (totalBytes + bytes)
		return true, remaining, int64(cfg.WindowSecs), nil
	}

	// We're at the limit, calculate reset time
	var resetSecs int64 = int64(cfg.WindowSecs)
	if len(validRecords) > 0 {
		oldestRecord := validRecords[0]
		for _, record := range validRecords {
			if record.Timestamp.Before(oldestRecord.Timestamp) {
				oldestRecord = record
			}
		}
		resetDuration := oldestRecord.Timestamp.Add(time.Duration(cfg.WindowSecs) * time.Second).Sub(now)
		resetSecs = int64(resetDuration.Seconds())
		if resetSecs < 0 {
			resetSecs = 0
		}
	}

	return false, 0, resetSecs, nil
}

// GetCurrentUsage returns the current byte usage for a client without recording new bytes
func (ml *MemoryLimiter) GetCurrentUsage(ctx context.Context, r *http.Request) (int64, error) {
	// Run cleanup to remove expired entries
	ml.cleanup()

	// Get the key for this request
	k := ml.keyBuilder(r)

	// Extract client ID for config lookup
	clientID := ml.extractClientID(k)

	// Get client-specific config
	cfg, _ := ml.GetClientConfig(ctx, clientID)

	ml.mu.RLock()
	defer ml.mu.RUnlock()

	// Get the current time
	now := time.Now()
	cutoff := now.Add(-time.Duration(cfg.WindowSecs) * time.Second)

	// Get current byte records in the window
	records, exists := ml.byteWindows[k]
	if !exists {
		return 0, nil
	}

	// Count valid bytes in the window
	var totalBytes int64
	for _, record := range records {
		if record.Timestamp.After(cutoff) {
			totalBytes += record.Bytes
		}
	}

	return totalBytes, nil
}

// GetLastKey returns the key that would be used for this request
func (ml *MemoryLimiter) GetLastKey(r *http.Request) string {
	return ml.keyBuilder(r)
}