package assertions

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

// CheckColumnDuplicates checks for duplicate full column names in the record.
func CheckColumnDuplicates(record arrow.RecordBatch) {
	if !Enabled {
		return
	}

	if record == nil {
		return
	}

	seen := make(map[string]struct{})
	for _, f := range record.Schema().Fields() {
		if _, ok := seen[f.Name]; ok {
			panic(fmt.Sprintf("duplicate column name: %s", f.Name))
		}
		seen[f.Name] = struct{}{}
	}
}

// CheckLabelValuesDuplicates checks duplicate short column names in the record and that only one value is present.
// Short column names are used as labels in the LogQL result. Duplicate short names will collapse into a single label,
// therefore only one value is allowed. It is valid to have multiple columns with the same short name, but different
// full names. This happens after Compat.
func CheckLabelValuesDuplicates(record arrow.RecordBatch) {
	if !Enabled {
		return
	}

	if record == nil {
		return
	}

	cols := make(map[string][]int)
	for i, f := range record.Schema().Fields() {
		ident, err := semconv.ParseFQN(f.Name)
		if err != nil {
			continue
		}
		cols[ident.ShortName()] = append(cols[ident.ShortName()], i)
	}

	for s, idxs := range cols {
		if len(idxs) > 1 {
			for i := range record.NumRows() {
				values := 0
				for _, j := range idxs {
					if !record.Column(j).IsNull(int(i)) && record.Column(j).IsValid(int(i)) && record.Column(j).ValueStr(int(i)) != "" {
						values++
					}
				}
				if values > 1 {
					panic(fmt.Sprintf("duplicate label values: %s=%s", s, record.Column(idxs[0]).ValueStr(int(i))))
				}
			}
		}
	}
}
