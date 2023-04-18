// Copyright 2022 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rand

import "math/bits"

type sfc64 struct {
	a uint64
	b uint64
	c uint64
	w uint64
}

func (s *sfc64) init(a uint64, b uint64, c uint64) {
	s.a = a
	s.b = b
	s.c = c
	s.w = 1
	for i := 0; i < 12; i++ {
		s.next64()
	}
}

func (s *sfc64) init0() {
	s.a = rand64()
	s.b = rand64()
	s.c = rand64()
	s.w = 1
}

func (s *sfc64) init1(u uint64) {
	s.a = u
	s.b = u
	s.c = u
	s.w = 1
	for i := 0; i < 12; i++ {
		s.next64()
	}
}

func (s *sfc64) init3(a uint64, b uint64, c uint64) {
	s.a = a
	s.b = b
	s.c = c
	s.w = 1
	for i := 0; i < 18; i++ {
		s.next64()
	}
}

func (s *sfc64) next64() (out uint64) { // named return value lowers inlining cost
	out = s.a + s.b + s.w
	s.w++
	s.a, s.b, s.c = s.b^(s.b>>11), s.c+(s.c<<3), bits.RotateLeft64(s.c, 24)+out // single assignment lowers inlining cost
	return
}
