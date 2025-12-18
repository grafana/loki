package parquet

import "reflect"

var noTags = parquetTags{}

// parquetTags represents the superset of all the parquet struct tags that can be used
// to configure a field.
type parquetTags struct {
	parquet        string
	parquetKey     string
	parquetValue   string
	parquetElement string
}

// fromStructTag parses the parquet struct tags from a reflect.StructTag and returns
// a parquetTags struct.
func fromStructTag(tag reflect.StructTag) parquetTags {
	parquetTags := parquetTags{}
	if val := tag.Get("parquet"); val != "" {
		parquetTags.parquet = val
	}
	if val := tag.Get("parquet-key"); val != "" {
		parquetTags.parquetKey = val
	}
	if val := tag.Get("parquet-value"); val != "" {
		parquetTags.parquetValue = val
	}
	if val := tag.Get("parquet-element"); val != "" {
		parquetTags.parquetElement = val
	}
	return parquetTags
}

// getMapKeyNodeTags returns the parquet tags for configuring the keys of a map.
func (p parquetTags) getMapKeyNodeTags() parquetTags {
	return parquetTags{
		parquet: p.parquetKey,
	}
}

// getMapValueNodeTags returns the parquet tags for configuring the values of a map.
func (p parquetTags) getMapValueNodeTags() parquetTags {
	return parquetTags{
		parquet: p.parquetValue,
	}
}

// getListElementNodeTags returns the parquet tags for configuring the elements of a list.
func (p parquetTags) getListElementNodeTags() parquetTags {
	return parquetTags{
		parquet: p.parquetElement,
	}
}
