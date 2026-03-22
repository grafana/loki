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

import "sync"

var entriesPool = sync.Pool{
	New: func() interface{} {
		return make(Entries, 0, 10)
	},
}

// Entries is a typed list of Entry that can be reused if Dispose
// is called.
type Entries []Entry

// Dispose will free the resources consumed by this list and
// allow the list to be reused.
func (entries *Entries) Dispose() {
	for i := 0; i < len(*entries); i++ {
		(*entries)[i] = nil
	}

	*entries = (*entries)[:0]
	entriesPool.Put(*entries)
}

// NewEntries will return a reused list of entries.
func NewEntries() Entries {
	return entriesPool.Get().(Entries)
}
