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

// MapEntry is one key/value pair of a Thrift map whose key type cannot be a
// Go map key: list, set and map keys, and typedefs resolving to one of those.
//
// The generator represents such a field as []MapEntry[K, V]. Entries are
// written to the wire in slice order. As with Thrift sets, which this library
// also represents as slices, writing a slice that contains two equal keys
// fails with an INVALID_DATA protocol exception; duplicates are not checked
// on read.
type MapEntry[K, V any] struct {
	Key   K
	Value V
}
