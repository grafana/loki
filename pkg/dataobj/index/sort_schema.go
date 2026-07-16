package index

import "strings"

// defaultSortSchemaKeys defines the label keys used for stats and postings
// sections when a logs section carries no schema sort of its own.
var defaultSortSchemaKeys = []string{"service_name"}

// schemaSortKeys converts a logs section's fully-qualified schema labels (e.g.
// "label:service_name") into label keys
func schemaSortKeys(fqns []string) []string {
	if len(fqns) == 0 {
		return defaultSortSchemaKeys
	}
	keys := make([]string, 0, len(fqns))
	for _, fqn := range fqns {
		// Only "label:<name>" keys are supported. Anything else means we cannot
		// reproduce the sort key here, so fall back to the default.
		typ, name, ok := strings.Cut(fqn, ":")
		if !ok || typ != "label" || name == "" {
			return defaultSortSchemaKeys
		}
		keys = append(keys, name)
	}
	return keys
}
