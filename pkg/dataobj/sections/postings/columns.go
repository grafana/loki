package postings

import (
	"fmt"
)

// findColumnsByType returns the columns from cols whose Type matches one of
// the requested types, in the order requested. The first column found for
// each requested type is returned. If a requested type is not present in
// cols, findColumnsByType returns an error.
//
// findColumnsByType mirrors the metastore.findPointersColumnsByTypes helper
// for the postings section.
func findColumnsByType(cols []*Column, types ...ColumnType) ([]*Column, error) {
	result := make([]*Column, 0, len(types))
	for _, want := range types {
		var found *Column
		for _, c := range cols {
			if c.Type == want {
				found = c
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("finding postings column %s: not found", want)
		}
		result = append(result, found)
	}
	return result, nil
}
