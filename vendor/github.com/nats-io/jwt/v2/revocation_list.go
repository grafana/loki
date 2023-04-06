/*
 * Copyright 2020 The NATS Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jwt

import (
	"time"
)

const All = "*"

// RevocationList is used to store a mapping of public keys to unix timestamps
type RevocationList map[string]int64
type RevocationEntry struct {
	PublicKey string
	TimeStamp int64
}

// Revoke enters a revocation by publickey and timestamp into this export
// If there is already a revocation for this public key that is newer, it is kept.
func (r RevocationList) Revoke(pubKey string, timestamp time.Time) {
	newTS := timestamp.Unix()
	// cannot move a revocation into the future - only into the past
	if ts, ok := r[pubKey]; ok && ts > newTS {
		return
	}
	r[pubKey] = newTS
}

// MaybeCompact will compact the revocation list if jwt.All is found. Any
// revocation that is covered by a jwt.All revocation will be deleted, thus
// reducing the size of the JWT. Returns a slice of entries that were removed
// during the process.
func (r RevocationList) MaybeCompact() []RevocationEntry {
	var deleted []RevocationEntry
	ats, ok := r[All]
	if ok {
		for k, ts := range r {
			if k != All && ats >= ts {
				deleted = append(deleted, RevocationEntry{
					PublicKey: k,
					TimeStamp: ts,
				})
				delete(r, k)
			}
		}
	}
	return deleted
}

// ClearRevocation removes any revocation for the public key
func (r RevocationList) ClearRevocation(pubKey string) {
	delete(r, pubKey)
}

// IsRevoked checks if the public key is in the revoked list with a timestamp later than
// the one passed in. Generally this method is called with an issue time but other time's can
// be used for testing.
func (r RevocationList) IsRevoked(pubKey string, timestamp time.Time) bool {
	if r.allRevoked(timestamp) {
		return true
	}
	ts, ok := r[pubKey]
	return ok && ts >= timestamp.Unix()
}

// allRevoked returns true if All is set and the timestamp is later or same as the
// one passed. This is called by IsRevoked.
func (r RevocationList) allRevoked(timestamp time.Time) bool {
	ts, ok := r[All]
	return ok && ts >= timestamp.Unix()
}
