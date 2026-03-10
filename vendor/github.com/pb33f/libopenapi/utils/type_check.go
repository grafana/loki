// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package utils

import "fmt"

// AreValuesCorrectlyTyped will look through an array of unknown values and check they match
// against the supplied type as a string. The return value is empty if everything is OK, or it
// contains failures in the form of a value as a key and a message as to why it's not valid
func AreValuesCorrectlyTyped(valType string, values interface{}) map[string]string {
	var arr []interface{}
	if _, ok := values.([]interface{}); !ok {
		return nil
	}
	arr = values.([]interface{})

	results := make(map[string]string)
	for _, v := range arr {
		switch v := v.(type) {
		case string:
			if valType != "string" {
				results[v] = fmt.Sprintf("enum value '%v' is a "+
					"string, but it's defined as a '%v'", v, valType)
			}
		case int64:
			if valType != "integer" && valType != "number" {
				results[fmt.Sprintf("%v", v)] = fmt.Sprintf("enum value '%v' is a "+
					"integer, but it's defined as a '%v'", v, valType)
			}
		case int:
			if valType != "integer" && valType != "number" {
				results[fmt.Sprintf("%v", v)] = fmt.Sprintf("enum value '%v' is a "+
					"integer, but it's defined as a '%v'", v, valType)
			}
		case float64:
			if valType != "number" {
				results[fmt.Sprintf("%v", v)] = fmt.Sprintf("enum value '%v' is a "+
					"number, but it's defined as a '%v'", v, valType)
			}
		case bool:
			if valType != "boolean" {
				results[fmt.Sprintf("%v", v)] = fmt.Sprintf("enum value '%v' is a "+
					"boolean, but it's defined as a '%v'", v, valType)
			}
		}
	}
	return results
}
