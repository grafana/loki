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
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter implements a sliding-window rate limiter using Redis.
type RedisLimiter struct {
	rdb        *redis.Client
	prefix     string
	limit      int           // N
	window     time.Duration // e.g., 1 * time.Minute
	lua        *redis.Script
	keyBuilder func(*http.Request) string
}

// NewRedisLimiter creates a global sliding-window limiter.
func NewRedisLimiter(rdb *redis.Client, prefix string, limit int, window time.Duration, keyBuilder func(*http.Request) string) *RedisLimiter {
	return &RedisLimiter{
		rdb:        rdb,
		prefix:     prefix,
		limit:      limit,
		window:     window,
		lua:        redis.NewScript(slidingLua),
		keyBuilder: keyBuilder,
	}
}

func (rl *RedisLimiter) key(k string) string { return rl.prefix + ":" + k }

// Allow returns (allowed, remaining, resetSeconds, error) atomically.
func (rl *RedisLimiter) Allow(ctx context.Context, r *http.Request) (bool, int64, int64, error) {
	k := rl.keyBuilder(r)
	// ARGV: window_ms, limit
	res, err := rl.lua.Run(ctx, rl.rdb, []string{rl.key(k)}, rl.window.Milliseconds(), rl.limit).Result()
	if err != nil {
		return false, 0, 0, err
	}
	arr := res.([]interface{})
	allowed := arr[0].(int64) == 1
	remaining := arr[1].(int64)
	reset := arr[2].(int64)
	return allowed, remaining, reset, nil
}

// GetLimit returns the configured request limit
func (rl *RedisLimiter) GetLimit() int {
	return rl.limit
}

// slidingLua uses Redis TIME for clock, ZSET for timestamps.
// KEYS[1] = rate key
// ARGV[1] = window_ms
// ARGV[2] = limit
var slidingLua = `
local key = KEYS[1]
local window_ms = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])

-- server time (ms)
local t = redis.call('TIME')
local now_ms = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)

local min_ts = now_ms - window_ms

-- prune old
redis.call('ZREMRANGEBYSCORE', key, 0, min_ts)

local count = redis.call('ZCARD', key)
local allowed = 0
local remaining = 0
local reset = 0

if count < limit then
  -- unique-ish member: ts-rand
  local member = tostring(now_ms) .. "-" .. tostring(math.random(1, 999999))
  redis.call('ZADD', key, now_ms, member)
  count = count + 1
  allowed = 1
  remaining = limit - count
else
  allowed = 0
  remaining = 0
end

-- compute reset from oldest element in window
local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
if oldest ~= nil and #oldest >= 2 then
  local oldestScore = tonumber(oldest[2])
  reset = math.max(0, math.ceil((oldestScore + window_ms - now_ms) / 1000))
else
  reset = math.ceil(window_ms / 1000)
end

-- keep TTL slightly > window to avoid premature eviction & reduce churn
redis.call('PEXPIRE', key, window_ms + 1000)

return {allowed, remaining, reset}
`
