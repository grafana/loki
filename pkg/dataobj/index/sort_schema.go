package index

import (
	"fmt"
	"strings"
)

// defaultSortSchema is the fully-qualified sort schema used for stats and
// postings sections when a logs section carries no schema sort of its own.
// It mirrors validation.DefaultSortSchema.
var defaultSortSchema = []string{"label:service_name"}

// sectionSortSchema returns the fully-qualified sort schema to record for a logs
// section, falling back to the default when the section carries none.
func sectionSortSchema(fqns []string) []string {
	if len(fqns) == 0 {
		return defaultSortSchema
	}
	return fqns
}

// schemaLabelNames converts fully-qualified sort keys ("label:<name>") into
// their bare Prometheus label names. Only "label:<name>" keys are supported; any
// other form is an error.
func schemaLabelNames(fqns []string) ([]string, error) {
	names := make([]string, 0, len(fqns))
	for _, fqn := range fqns {
		typ, name, ok := strings.Cut(fqn, ":")
		if !ok || typ != "label" || name == "" {
			return nil, fmt.Errorf("unsupported sort key %q — expected \"label:<name>\"", fqn)
		}
		names = append(names, name)
	}
	return names, nil
}
