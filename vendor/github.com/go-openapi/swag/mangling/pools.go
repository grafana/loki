// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mangling

import (
	"bytes"
	"sync"
)

const maxAllocMatches = 8

type (
	// memory pools of temporary objects.
	//
	// These are used to recycle temporarily allocated objects
	// and relieve the GC from undue pressure.

	matchesPool struct {
		*sync.Pool
	}

	buffersPool struct {
		*sync.Pool
	}

	lexemsPool struct {
		*sync.Pool
	}

	stringsPool struct {
		*sync.Pool
	}
)

var (
	// poolOfMatches holds temporary slices for recycling during the initialism match process
	poolOfMatches = matchesPool{
		Pool: &sync.Pool{
			New: func() any {
				s := make(initialismMatches, 0, maxAllocMatches)

				return &s
			},
		},
	}

	poolOfBuffers = buffersPool{
		Pool: &sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
	}

	poolOfLexems = lexemsPool{
		Pool: &sync.Pool{
			New: func() any {
				s := make([]nameLexem, 0, maxAllocMatches)

				return &s
			},
		},
	}

	poolOfStrings = stringsPool{
		Pool: &sync.Pool{
			New: func() any {
				s := make([]string, 0, maxAllocMatches)

				return &s
			},
		},
	}
)

func (p matchesPool) BorrowMatches() *initialismMatches {
	s := p.Get().(*initialismMatches)
	*s = (*s)[:0] // reset slice, keep allocated capacity

	return s
}

func (p buffersPool) BorrowBuffer(size int) *bytes.Buffer {
	s := p.Get().(*bytes.Buffer)
	s.Reset()

	if s.Cap() < size {
		s.Grow(size)
	}

	return s
}

func (p lexemsPool) BorrowLexems() *[]nameLexem {
	s := p.Get().(*[]nameLexem)
	*s = (*s)[:0] // reset slice, keep allocated capacity

	return s
}

func (p stringsPool) BorrowStrings() *[]string {
	s := p.Get().(*[]string)
	*s = (*s)[:0] // reset slice, keep allocated capacity

	return s
}

func (p matchesPool) RedeemMatches(s *initialismMatches) {
	p.Put(s)
}

func (p buffersPool) RedeemBuffer(s *bytes.Buffer) {
	p.Put(s)
}

func (p lexemsPool) RedeemLexems(s *[]nameLexem) {
	p.Put(s)
}

func (p stringsPool) RedeemStrings(s *[]string) {
	p.Put(s)
}
