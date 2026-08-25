// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"

	"github.com/googleapis/gax-go/v2/callctx"
)

const featureTrackerHeaderName = "x-goog-storage-go-features"

// addFeatureAttributes adds the specified feature codes to the context.
// Features are stored as a bitmask in the callctx headers and will be
// injected into the outgoing request headers by the transport.
func addFeatureAttributes(ctx context.Context, features ...trackedFeature) context.Context {
	if len(features) == 0 {
		return ctx
	}

	current := featureAttributes(ctx)
	updated := current
	for _, f := range features {
		updated |= (1 << f)
	}

	if updated == current {
		return ctx
	}

	return callctx.SetHeaders(ctx, featureTrackerHeaderName, encodeUint32(uint32(updated)))
}

// featureAttributes extracts and merges all feature attributes present in the context.
// It returns a bitmask represented as a uint8.
func featureAttributes(ctx context.Context) uint32 {
	ctxHeaders := callctx.HeadersFromContext(ctx)
	// If multiple values are present in the context (e.g. from nested calls),
	// merge them into a single bitmask.
	return mergeFeatureAttributes(ctxHeaders[featureTrackerHeaderName])
}

func mergeFeatureAttributes(vals []string) uint32 {
	features := uint32(0)
	for _, val := range vals {
		if decoded, err := decodeUint32(val); err == nil {
			features |= decoded
		}
	}
	return features
}
