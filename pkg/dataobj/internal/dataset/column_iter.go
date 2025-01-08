package dataset

import "github.com/grafana/loki/v3/pkg/dataobj/internal/result"

func iterMemColumn(col *MemColumn) result.Seq[Value] {
	return result.Iter(func(yield func(Value) bool) error {
		for _, page := range col.Pages {
			for result := range iterMemPage(page, col.Info.Type, col.Info.Compression) {
				val, err := result.Value()
				if err != nil {
					return err
				} else if !yield(val) {
					return nil
				}
			}
		}

		return nil
	})
}
