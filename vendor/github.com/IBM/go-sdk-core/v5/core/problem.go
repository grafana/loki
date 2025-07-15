package core

// (C) Copyright IBM Corp. 2024.
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

// Problem is an interface that describes the common
// behavior of custom IBM problem message types.
type Problem interface {

	// GetConsoleMessage returns a message suited to the practitioner
	// or end user. It should tell the user what went wrong, and why,
	// without unnecessary implementation details.
	GetConsoleMessage() string

	// GetDebugMessage returns a message suited to the developer, in
	// order to assist in debugging. It should give enough information
	// for the developer to identify the root cause of the issue.
	GetDebugMessage() string

	// GetID returns an identifier or code for a given problem. It is computed
	// from the attributes of the problem, so that the same problem scenario
	// will always have the same ID, even when encountered by different users.
	GetID() string

	// Error returns the message associated with a given problem and guarantees
	// every instance of Problem also implements the native "error" interface.
	Error() string
}

// OrderableProblem provides an interface for retrieving ordered
// representations of problems in order to print YAML messages
// with a controlled ordering of the fields.
type OrderableProblem interface {
	GetConsoleOrderedMaps() *OrderedMaps
	GetDebugOrderedMaps() *OrderedMaps
}
