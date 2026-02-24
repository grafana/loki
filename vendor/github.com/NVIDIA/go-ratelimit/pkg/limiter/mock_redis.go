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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisMockClient implements a mock Redis client for testing
type RedisMockClient struct {
	mu       sync.RWMutex
	data     map[string]interface{}
	zsets    map[string][]zsetEntry
	ttls     map[string]time.Time
	scripts  map[string]*redis.Script
	failNext bool // For simulating failures
}

type zsetEntry struct {
	member string
	score  float64
}

// NewRedisMockClient creates a new mock Redis client
func NewRedisMockClient() *RedisMockClient {
	return &RedisMockClient{
		data:    make(map[string]interface{}),
		zsets:   make(map[string][]zsetEntry),
		ttls:    make(map[string]time.Time),
		scripts: make(map[string]*redis.Script),
	}
}

// Get simulates Redis GET command
func (m *RedisMockClient) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	if val, exists := m.data[key]; exists {
		if strVal, ok := val.(string); ok {
			cmd.SetVal(strVal)
		} else {
			cmd.SetErr(redis.Nil)
		}
	} else {
		cmd.SetErr(redis.Nil)
	}

	return cmd
}

// Set simulates Redis SET command
func (m *RedisMockClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	m.data[key] = value
	if expiration > 0 {
		m.ttls[key] = time.Now().Add(expiration)
	}

	cmd.SetVal("OK")
	return cmd
}

// Del simulates Redis DEL command
func (m *RedisMockClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	deleted := int64(0)
	for _, key := range keys {
		if _, exists := m.data[key]; exists {
			delete(m.data, key)
			delete(m.ttls, key)
			deleted++
		}
		if _, exists := m.zsets[key]; exists {
			delete(m.zsets, key)
			deleted++
		}
	}

	cmd.SetVal(deleted)
	return cmd
}

// ZAdd simulates Redis ZADD command
func (m *RedisMockClient) ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	if m.zsets[key] == nil {
		m.zsets[key] = []zsetEntry{}
	}

	added := int64(0)
	for _, member := range members {
		found := false
		for i, entry := range m.zsets[key] {
			if entry.member == fmt.Sprint(member.Member) {
				m.zsets[key][i].score = member.Score
				found = true
				break
			}
		}
		if !found {
			m.zsets[key] = append(m.zsets[key], zsetEntry{
				member: fmt.Sprint(member.Member),
				score:  member.Score,
			})
			added++
		}
	}

	cmd.SetVal(added)
	return cmd
}

// ZCard simulates Redis ZCARD command
func (m *RedisMockClient) ZCard(ctx context.Context, key string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	if zset, exists := m.zsets[key]; exists {
		cmd.SetVal(int64(len(zset)))
	} else {
		cmd.SetVal(0)
	}

	return cmd
}

// ZRange simulates Redis ZRANGE command
func (m *RedisMockClient) ZRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd {
	cmd := redis.NewStringSliceCmd(ctx)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	if zset, exists := m.zsets[key]; exists {
		var result []string
		length := int64(len(zset))

		// Handle negative indices
		if start < 0 {
			start = length + start
		}
		if stop < 0 {
			stop = length + stop
		}

		// Clamp to valid range
		if start < 0 {
			start = 0
		}
		if stop >= length {
			stop = length - 1
		}

		for i := start; i <= stop && i < length; i++ {
			result = append(result, zset[i].member)
		}
		cmd.SetVal(result)
	} else {
		cmd.SetVal([]string{})
	}

	return cmd
}

// ZRemRangeByScore simulates Redis ZREMRANGEBYSCORE command
func (m *RedisMockClient) ZRemRangeByScore(ctx context.Context, key string, min, max string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	if zset, exists := m.zsets[key]; exists {
		minScore, _ := strconv.ParseFloat(min, 64)
		maxScore, _ := strconv.ParseFloat(max, 64)

		var newZset []zsetEntry
		removed := int64(0)

		for _, entry := range zset {
			if entry.score < minScore || entry.score > maxScore {
				newZset = append(newZset, entry)
			} else {
				removed++
			}
		}

		m.zsets[key] = newZset
		cmd.SetVal(removed)
	} else {
		cmd.SetVal(0)
	}

	return cmd
}

// PExpire simulates Redis PEXPIRE command
func (m *RedisMockClient) PExpire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	if _, exists := m.data[key]; exists {
		m.ttls[key] = time.Now().Add(expiration)
		cmd.SetVal(true)
	} else if _, exists := m.zsets[key]; exists {
		m.ttls[key] = time.Now().Add(expiration)
		cmd.SetVal(true)
	} else {
		cmd.SetVal(false)
	}

	return cmd
}

// Time simulates Redis TIME command
func (m *RedisMockClient) Time(ctx context.Context) *redis.TimeCmd {
	cmd := redis.NewTimeCmd(ctx)

	if m.failNext {
		cmd.SetErr(fmt.Errorf("mock redis error"))
		return cmd
	}

	cmd.SetVal(time.Now())
	return cmd
}

// SimulateFailure sets the client to fail on the next operation
func (m *RedisMockClient) SimulateFailure(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = fail
}

// MockLuaScript simulates Lua script execution for testing
type MockLuaScript struct {
	mu          sync.RWMutex
	allowCount  int
	currentCall int
	remaining   int64
	reset       int64
}

// NewMockLuaScript creates a new mock Lua script handler
func NewMockLuaScript(allowCount int) *MockLuaScript {
	return &MockLuaScript{
		allowCount: allowCount,
		remaining:  int64(allowCount),
		reset:      60,
	}
}

// Run simulates the Lua script execution
func (m *MockLuaScript) Run(ctx context.Context, c redis.Scripter, keys []string, args ...interface{}) *redis.Cmd {
	cmd := redis.NewCmd(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate sliding window logic
	m.currentCall++

	if m.currentCall <= m.allowCount {
		m.remaining = int64(m.allowCount - m.currentCall)
		// Return [allowed, remaining, reset]
		cmd.SetVal([]interface{}{int64(1), m.remaining, m.reset})
	} else {
		// Rate limit exceeded
		cmd.SetVal([]interface{}{int64(0), int64(0), m.reset})
	}

	return cmd
}

// Reset resets the mock counter
func (m *MockLuaScript) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentCall = 0
	m.remaining = int64(m.allowCount)
}

// ConfigMockManager provides a mock implementation of config manager
type ConfigMockManager struct {
	mu      sync.RWMutex
	configs map[string]json.RawMessage
}

// NewConfigMockManager creates a new mock config manager
func NewConfigMockManager() *ConfigMockManager {
	return &ConfigMockManager{
		configs: make(map[string]json.RawMessage),
	}
}

// GetConfig retrieves a config for testing
func (m *ConfigMockManager) GetConfig(clientID string) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if cfg, exists := m.configs[clientID]; exists {
		return cfg, nil
	}

	// Return default config
	defaultCfg, _ := json.Marshal(map[string]interface{}{
		"request_limit": 10,
		"byte_limit":    1048576, // 1MB
		"window_secs":   60,
	})
	return defaultCfg, nil
}

// SetConfig sets a config for testing
func (m *ConfigMockManager) SetConfig(clientID string, cfg json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.configs[clientID] = cfg
	return nil
}

// Helper function to extract client ID from composite keys
func extractTestClientID(key string) string {
	if strings.HasPrefix(key, "apikey:") {
		parts := strings.Split(key, "|")
		if len(parts) > 0 {
			return strings.TrimPrefix(parts[0], "apikey:")
		}
	}
	return key
}
