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
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

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

	return callctx.SetHeaders(ctx, featureTrackerHeaderName, encodeFeatureMask(updated))
}

// featureAttributes extracts and merges all feature attributes present in the context.
// It returns a bitmask represented as a uint32.
func featureAttributes(ctx context.Context) uint32 {
	ctxHeaders := callctx.HeadersFromContext(ctx)
	// If multiple values are present in the context (e.g. from nested calls),
	// merge them into a single bitmask.
	return mergeFeatureAttributes(ctxHeaders[featureTrackerHeaderName])
}

func mergeFeatureAttributes(vals []string) uint32 {
	features := uint32(0)
	for _, val := range vals {
		if decoded, err := decodeFeatureMask(val); err == nil {
			features |= decoded
		}
	}
	return features
}

// encodeFeatureMask encodes a feature bitmask as 4 big-endian bytes using the
// URL-safe base64 alphabet (RFC 4648 section 5).
//
// The server decodes the feature header with a strict web-safe decoder that
// rejects the '+' and '/' characters of the standard alphabet, so this must
// not use encodeUint32 (which is standard base64, as required for CRC32C).
func encodeFeatureMask(mask uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], mask)
	return base64.URLEncoding.EncodeToString(b[:])
}

// decodeFeatureMask decodes a 4-byte big-endian feature bitmask. It accepts
// both the URL-safe and the standard base64 alphabets, with or without
// padding, so that values produced by older versions of this package are
// still merged correctly.
func decodeFeatureMask(s string) (uint32, error) {
	trimmed := strings.TrimSuffix(s, "==")
	if len(trimmed) != 6 {
		return 0, fmt.Errorf("storage: %q does not encode a 32-bit feature mask", s)
	}
	d, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		d, err = base64.RawStdEncoding.DecodeString(trimmed)
		if err != nil {
			return 0, err
		}
	}
	return binary.BigEndian.Uint32(d), nil
}
