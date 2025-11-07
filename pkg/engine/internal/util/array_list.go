package util //nolint:revive

import (
	"github.com/apache/arrow-go/v18/arrow/array"
)

func ArrayListValue(arr *array.List, i int) any {
	if arr.Len() == 0 {
		return []string{}
	}

	start, end := arr.ValueOffsets(i)
	listValues := arr.ListValues()
	switch listValues := listValues.(type) {
	case *array.String:
		result := make([]string, end-start)
		for i := start; i < end; i++ {
			result[i-start] = listValues.Value(int(i))
		}
		return result
	}

	return nil

}
