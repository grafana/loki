/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements. See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership. The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License. You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package thrift

import (
	"context"
	"errors"
)

type recursionDepthKey struct{}

type recursionDepthTracker struct {
	depth int
	limit int
}

// CheckRecursionDepth increments the per-context struct nesting depth and
// returns an error when it exceeds DEFAULT_RECURSION_DEPTH.  The returned
// context must be passed to DecrementRecursionDepth when the struct is done
// being read or written.
func CheckRecursionDepth(ctx context.Context) (context.Context, error) {
	tracker, _ := ctx.Value(recursionDepthKey{}).(*recursionDepthTracker)
	if tracker == nil {
		tracker = &recursionDepthTracker{limit: DEFAULT_RECURSION_DEPTH}
		ctx = context.WithValue(ctx, recursionDepthKey{}, tracker)
	}
	tracker.depth++
	if tracker.depth > tracker.limit {
		tracker.depth--
		return ctx, NewTProtocolExceptionWithType(DEPTH_LIMIT, errors.New("maximum recursion depth exceeded"))
	}
	return ctx, nil
}

// DecrementRecursionDepth decrements the per-context struct nesting depth.
// It must be called after CheckRecursionDepth returns nil, typically via defer.
func DecrementRecursionDepth(ctx context.Context) {
	if tracker, ok := ctx.Value(recursionDepthKey{}).(*recursionDepthTracker); ok && tracker != nil {
		tracker.depth--
	}
}
