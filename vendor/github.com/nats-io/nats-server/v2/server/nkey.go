// Copyright 2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"crypto/rand"
	"encoding/base64"
)

// Raw length of the nonce challenge
const (
	nonceRawLen = 11
	nonceLen    = 15 // base64.RawURLEncoding.EncodedLen(nonceRawLen)
)

// NonceRequired tells us if we should send a nonce.
func (s *Server) NonceRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nonceRequired()
}

// nonceRequired tells us if we should send a nonce.
// Lock should be held on entry.
func (s *Server) nonceRequired() bool {
	return s.opts.AlwaysEnableNonce || len(s.nkeys) > 0 || s.trustedKeys != nil
}

// Generate a nonce for INFO challenge.
// Assumes server lock is held
func (s *Server) generateNonce(n []byte) {
	var raw [nonceRawLen]byte
	data := raw[:]
	rand.Read(data)
	base64.RawURLEncoding.Encode(n, data)
}
