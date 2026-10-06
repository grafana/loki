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

// UnorderedEqual reports whether a and b hold the same elements, in any order,
// comparing them with eq.
//
// Generated code calls this for the fields that stand in for an unordered
// Thrift collection: a set, and a map whose key type forces the entry-slice
// representation. Lists are ordered and are compared position by position
// instead.
//
// The elements are compared position by position until the first pair that
// differs, so two values that agree on order, which includes any value that
// has just been deserialized, cost one pass and allocate nothing. That pass
// consumes the prefix it matched; only the elements after it are matched
// pairwise, which is quadratic.
//
// Matching consumes an element of b at most once, so a and b compare equal
// only if they hold the same elements with the same multiplicities. That
// matters for a value assembled in memory: the wire format rejects a set or
// map that repeats an element, but nothing stops a caller from building one.
func UnorderedEqual[T any](a, b []T, eq func(x, y T) bool) bool {
	if len(a) != len(b) {
		return false
	}

	n := 0
	for ; n < len(a); n++ {
		if !eq(a[n], b[n]) {
			break
		}
	}
	if n == len(a) {
		return true
	}

	// a[:n] and b[:n] are paired off already. Since eq is an equality, a pair
	// it accepted can be part of any matching of the whole, so the elements
	// before n need not be revisited.
	a, b = a[n:], b[n:]
	matched := make([]bool, len(b))
	for i := range a {
		found := false
		for j := range b {
			if matched[j] || !eq(a[i], b[j]) {
				continue
			}
			matched[j] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}
