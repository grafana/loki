package index

import (
	"fmt"
	"strings"
)

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
