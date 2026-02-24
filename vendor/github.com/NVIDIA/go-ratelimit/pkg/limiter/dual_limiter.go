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

// DualLimiter implements a sliding-window rate limiter that tracks both request count and bytes
type DualLimiter struct {
	rdb          *redis.Client
	prefix       string
	requestLimit int           // Request count limit
	byteLimit    int64         // Byte limit
	window       time.Duration // e.g., 1 * time.Minute
	lua          *redis.Script
	keyBuilder   func(*http.Request) string
}

// NewDualLimiter creates a sliding-window limiter that tracks both requests and bytes
func NewDualLimiter(
	rdb *redis.Client,
	prefix string,
	requestLimit int,
	byteLimit int64,
	window time.Duration,
	keyBuilder func(*http.Request) string,
) *DualLimiter {
	return &DualLimiter{
		rdb:          rdb,
		prefix:       prefix,
		requestLimit: requestLimit,
		byteLimit:    byteLimit,
		window:       window,
		lua:          redis.NewScript(dualSlidingLua),
		keyBuilder:   keyBuilder,
	}
}

func (dl *DualLimiter) requestKey(k string) string { return dl.prefix + ":req:" + k }
func (dl *DualLimiter) byteKey(k string) string    { return dl.prefix + ":byte:" + k }

// GetRequestLimit returns the configured request limit
func (dl *DualLimiter) GetRequestLimit() int {
	return dl.requestLimit
}

// GetByteLimit returns the configured byte limit
func (dl *DualLimiter) GetByteLimit() int64 {
	return dl.byteLimit
}

// AllowRequest checks if a new request is allowed based on request count limit
func (dl *DualLimiter) AllowRequest(ctx context.Context, r *http.Request) (bool, int64, int64, error) {
	k := dl.keyBuilder(r)
	// ARGV: window_ms, limit, bytes (0 for request check)
	res, err := dl.lua.Run(ctx, dl.rdb,
		[]string{dl.requestKey(k), dl.byteKey(k)},
		dl.window.Milliseconds(), dl.requestLimit, dl.byteLimit, 0).Result()
	if err != nil {
		return false, 0, 0, err
	}
	arr := res.([]interface{})
	allowed := arr[0].(int64) == 1
	remaining := arr[1].(int64)
	reset := arr[2].(int64)
	return allowed, remaining, reset, nil
}

// RecordBytes records the bytes used for a request and checks if byte limit is exceeded
func (dl *DualLimiter) RecordBytes(ctx context.Context, r *http.Request, bytes int64) (bool, int64, int64, error) {
	k := dl.keyBuilder(r)
	// ARGV: window_ms, limit, bytes (actual bytes for byte check)
	res, err := dl.lua.Run(ctx, dl.rdb,
		[]string{dl.requestKey(k), dl.byteKey(k)},
		dl.window.Milliseconds(), dl.requestLimit, dl.byteLimit, bytes).Result()
	if err != nil {
		return false, 0, 0, err
	}
	arr := res.([]interface{})
	allowed := arr[0].(int64) == 1
	remaining := arr[1].(int64)
	reset := arr[2].(int64)
	return allowed, remaining, reset, nil
}

// dualSlidingLua uses Redis TIME for clock, ZSETs for timestamps and bytes.
// KEYS[1] = request rate key
// KEYS[2] = byte rate key
// ARGV[1] = window_ms
// ARGV[2] = request limit
// ARGV[3] = byte limit
// ARGV[4] = bytes (0 for request check, actual bytes for byte recording)
var dualSlidingLua = `
local req_key = KEYS[1]
local byte_key = KEYS[2]
local window_ms = tonumber(ARGV[1])
local req_limit = tonumber(ARGV[2])
local byte_limit = tonumber(ARGV[3])
local bytes = tonumber(ARGV[4])

-- server time (ms)
local t = redis.call('TIME')
local now_ms = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)

local min_ts = now_ms - window_ms

-- prune old entries from both keys
redis.call('ZREMRANGEBYSCORE', req_key, 0, min_ts)
redis.call('ZREMRANGEBYSCORE', byte_key, 0, min_ts)

local req_count = redis.call('ZCARD', req_key)
local byte_count = 0

-- Get total bytes in window
local byte_entries = redis.call('ZRANGE', byte_key, 0, -1)
for i = 1, #byte_entries do
    -- Extract the byte count from the member string (format: "bytes-timestamp-random")
    local bytes_str = string.match(byte_entries[i], "^(%d+)-")
    if bytes_str then
        byte_count = byte_count + tonumber(bytes_str)
    end
end

local allowed = 0
local remaining = 0
local reset = 0

-- If bytes is 0, we're just checking request count
if bytes == 0 then
    -- If req_limit is negative, request limiting is disabled
    if req_limit < 0 or req_count < req_limit then
        allowed = 1
        -- For disabled limits, return a high number for remaining
        remaining = req_limit < 0 and 999999 or (req_limit - req_count)
    else
        allowed = 0
        remaining = 0
    end
else
    -- We're recording bytes
    if byte_count + bytes <= byte_limit then
        -- Add the bytes to the byte counter
        local byte_member = tostring(bytes) .. "-" .. tostring(now_ms) .. "-" .. tostring(math.random(1, 999999))
        redis.call('ZADD', byte_key, now_ms, byte_member)
        allowed = 1
        remaining = byte_limit - (byte_count + bytes)
    else
        allowed = 0
        remaining = 0
    end
end

-- If we're checking request count and allowed, add the request (unless disabled)
if bytes == 0 and allowed == 1 and req_limit >= 0 then
    -- unique-ish member: ts-rand
    local req_member = tostring(now_ms) .. "-" .. tostring(math.random(1, 999999))
    redis.call('ZADD', req_key, now_ms, req_member)
end

-- compute reset from oldest element in request window
local oldest = redis.call('ZRANGE', req_key, 0, 0, 'WITHSCORES')
if oldest ~= nil and #oldest >= 2 then
    local oldestScore = tonumber(oldest[2])
    reset = math.max(0, math.ceil((oldestScore + window_ms - now_ms) / 1000))
else
    reset = math.ceil(window_ms / 1000)
end

-- keep TTL slightly > window to avoid premature eviction & reduce churn
redis.call('PEXPIRE', req_key, window_ms + 1000)
redis.call('PEXPIRE', byte_key, window_ms + 1000)

return {allowed, remaining, reset}
`
