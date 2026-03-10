package parquet

import (
	"reflect"
	"strings"
)

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

// protoFieldNameFromTag extracts the field name from a protobuf struct tag.
// The protobuf tag format is: protobuf:"type,number,opt,name=field_name,json=jsonName,proto3"
// Returns empty string if no name is found.
func protoFieldNameFromTag(tag reflect.StructTag) string {
	protoTag := tag.Get("protobuf")
	if protoTag == "" {
		return ""
	}
	for part := range strings.SplitSeq(protoTag, ",") {
		if name, value, ok := strings.Cut(part, "="); ok && name == "name" {
			return value
		}
	}
	return ""
}
