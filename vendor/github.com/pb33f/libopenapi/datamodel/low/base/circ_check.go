// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package base

// CheckSchemaProxyForCircularRefs checks if the provided SchemaProxy has any circular references, extracted from
// The rolodex attached to the index.
func CheckSchemaProxyForCircularRefs(s *SchemaProxy) bool {
	if s.GetIndex() == nil || s.GetIndex().GetRolodex() == nil {
		return false // no index or rolodex, so no circular references
	}
	rolo := s.GetIndex().GetRolodex()
	allCircs := rolo.GetRootIndex().GetCircularReferences()
	safeCircularRefs := rolo.GetSafeCircularReferences()
	ignoredCircularRefs := rolo.GetIgnoredCircularReferences()
	combinedCircularRefs := append(safeCircularRefs, ignoredCircularRefs...)
	combinedCircularRefs = append(combinedCircularRefs, allCircs...)
	dup := make(map[string]struct{})
	for _, ref := range combinedCircularRefs {
		// hash the root node of the schema reference
		if ref.LoopPoint.FullDefinition == s.GetReference() || ref.LoopPoint.Definition == s.GetReference() {
			return true
		}
		// check journey, if we have any duplicated
		for _, ji := range ref.Journey {
			if _, exists := dup[ji.FullDefinition]; exists {
				return true // this has already been checked, it's a loop.
			}
			dup[ji.FullDefinition] = struct{}{}
		}
	}
	return false
}
