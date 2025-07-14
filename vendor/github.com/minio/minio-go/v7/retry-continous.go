/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2017 MinIO, Inc.
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

package minio

import (
	"iter"
	"math"
	"time"
)

// newRetryTimerContinous creates a timer with exponentially increasing delays forever.
func (c *Client) newRetryTimerContinous(baseSleep, maxSleep time.Duration, jitter float64) iter.Seq[int] {
	// normalize jitter to the range [0, 1.0]
	if jitter < NoJitter {
		jitter = NoJitter
	}
	if jitter > MaxJitter {
		jitter = MaxJitter
	}

	// computes the exponential backoff duration according to
	// https://www.awsarchitectureblog.com/2015/03/backoff.html
	exponentialBackoffWait := func(attempt int) time.Duration {
		// 1<<uint(attempt) below could overflow, so limit the value of attempt
		maxAttempt := 30
		if attempt > maxAttempt {
			attempt = maxAttempt
		}
		// sleep = random_between(0, min(maxSleep, base * 2 ** attempt))
		sleep := baseSleep * time.Duration(1<<uint(attempt))
		if sleep > maxSleep {
			sleep = maxSleep
		}
		if math.Abs(jitter-NoJitter) > 1e-9 {
			sleep -= time.Duration(c.random.Float64() * float64(sleep) * jitter)
		}
		return sleep
	}

	return func(yield func(int) bool) {
		var nextBackoff int
		for {
			if !yield(nextBackoff) {
				return
			}
			nextBackoff++
			time.Sleep(exponentialBackoffWait(nextBackoff))
		}
	}
}
