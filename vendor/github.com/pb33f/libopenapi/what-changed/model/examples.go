// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	v2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	"go.yaml.in/yaml/v4"
)

// ExamplesChanges represents changes made between Swagger Examples objects (Not OpenAPI 3).
type ExamplesChanges struct {
	*PropertyChanges
}

// GetAllChanges returns a slice of all changes made between Examples objects
func (a *ExamplesChanges) GetAllChanges() []*Change {
	if a == nil {
		return nil
	}
	return a.Changes
}

// TotalChanges represents the total number of changes made between Example instances.
func (a *ExamplesChanges) TotalChanges() int {
	if a == nil {
		return 0
	}
	return a.PropertyChanges.TotalChanges()
}

// TotalBreakingChanges will always return 0. Examples cannot break a contract.
func (a *ExamplesChanges) TotalBreakingChanges() int {
	return 0 // not supported.
}

// CompareExamplesV2 compares two Swagger Examples objects, returning a pointer to
// ExamplesChanges if anything was found.
func CompareExamplesV2(l, r *v2.Examples) *ExamplesChanges {
	lHashes := make(map[string]string)
	rHashes := make(map[string]string)
	lValues := make(map[string]low.ValueReference[*yaml.Node])
	rValues := make(map[string]low.ValueReference[*yaml.Node])

	for k, v := range l.Values.FromOldest() {
		lHashes[k.Value] = low.GenerateHashString(v.Value)
		lValues[k.Value] = v
	}

	for k, v := range r.Values.FromOldest() {
		rHashes[k.Value] = low.GenerateHashString(v.Value)
		rValues[k.Value] = v
	}
	var changes []*Change

	// check left example hashes
	for k := range lHashes {
		rhash := rHashes[k]
		if rhash == "" {
			CreateChange(&changes, ObjectRemoved, k,
				lValues[k].GetValueNode(), nil, false,
				lValues[k].GetValue(), nil)
			continue
		}
		if lHashes[k] == rHashes[k] {
			continue
		}
		CreateChange(&changes, Modified, k,
			lValues[k].GetValueNode(), rValues[k].GetValueNode(), false,
			lValues[k].GetValue(), lValues[k].GetValue())

	}

	// check right example hashes
	for k := range rHashes {
		lhash := lHashes[k]
		if lhash == "" {
			CreateChange(&changes, ObjectAdded, k,
				nil, lValues[k].GetValueNode(), false,
				nil, lValues[k].GetValue())
			continue
		}
	}

	ex := new(ExamplesChanges)
	ex.PropertyChanges = NewPropertyChanges(changes)
	if ex.TotalChanges() <= 0 {
		return nil
	}
	return ex
}
