// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build tinygo

package maphash

import (
	"hash"
	"hash/fnv"
	"math/rand"
	"time"
)

type MapHash struct {
	h hash.Hash64
}

func (h *MapHash) Write(p []byte) (n int, err error) {
	if h.h == nil {
		h.h = fnv.New64a()
	}
	return h.h.Write(p)
}

func (h *MapHash) WriteByte(c byte) error {
	if h.h == nil {
		h.h = fnv.New64a()
	}
	_, err := h.h.Write([]byte{c})
	return err
}

func (h *MapHash) WriteString(s string) (n int, err error) {
	if h.h == nil {
		h.h = fnv.New64a()
	}
	return h.h.Write([]byte(s))
}

func (h *MapHash) Reset() {
	if h.h != nil {
		h.h.Reset()
	}
}

func (h *MapHash) Sum64() uint64 {
	if h.h == nil {
		h.h = fnv.New64a()
	}
	return h.h.Sum64()
}

func (h *MapHash) SetSeed(seed Seed) {
	// fnv doesn't have a seed. So we ignore this.
	// But we need to define the method to match the interface.
}

type Seed struct {
	s uint64
}

func MakeSeed() Seed {
	rand.Seed(time.Now().UnixNano())
	return Seed{s: rand.Uint64()}
}
