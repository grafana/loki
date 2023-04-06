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

// We wrap to hold onto optional items for /connz.
type closedClient struct {
	ConnInfo
	subs []SubDetail
	user string
	acc  string
}

// Fixed sized ringbuffer for closed connections.
type closedRingBuffer struct {
	total uint64
	conns []*closedClient
}

// Create a new ring buffer with at most max items.
func newClosedRingBuffer(max int) *closedRingBuffer {
	rb := &closedRingBuffer{}
	rb.conns = make([]*closedClient, max)
	return rb
}

// Adds in a new closed connection. If there is no more room,
// remove the oldest.
func (rb *closedRingBuffer) append(cc *closedClient) {
	rb.conns[rb.next()] = cc
	rb.total++
}

func (rb *closedRingBuffer) next() int {
	return int(rb.total % uint64(cap(rb.conns)))
}

func (rb *closedRingBuffer) len() int {
	if rb.total > uint64(cap(rb.conns)) {
		return cap(rb.conns)
	}
	return int(rb.total)
}

func (rb *closedRingBuffer) totalConns() uint64 {
	return rb.total
}

// This will return a sorted copy of the list which recipient can
// modify. If the contents of the client itself need to be modified,
// meaning swapping in any optional items, a copy should be made. We
// could introduce a new lock and hold that but since we return this
// list inside monitor which allows programatic access, we do not
// know when it would be done.
func (rb *closedRingBuffer) closedClients() []*closedClient {
	dup := make([]*closedClient, rb.len())
	head := rb.next()
	if rb.total <= uint64(cap(rb.conns)) || head == 0 {
		copy(dup, rb.conns[:rb.len()])
	} else {
		fp := rb.conns[head:]
		sp := rb.conns[:head]
		copy(dup, fp)
		copy(dup[len(fp):], sp)
	}
	return dup
}
