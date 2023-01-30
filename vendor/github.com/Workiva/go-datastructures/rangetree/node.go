/*
Copyright 2014 Workiva, LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rangetree

type nodes []*node

type node struct {
	value        int64
	entry        Entry
	orderedNodes orderedNodes
}

func newNode(value int64, entry Entry, needNextDimension bool) *node {
	n := &node{}
	n.value = value
	if needNextDimension {
		n.orderedNodes = make(orderedNodes, 0, 10)
	} else {
		n.entry = entry
	}

	return n
}
