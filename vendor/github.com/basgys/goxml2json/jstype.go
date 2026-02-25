package xml2json

import (
	"strconv"
	"strings"
)

// https://cswr.github.io/JsonSchema/spec/basic_types/
// JSType is a JavaScript extracted from a string
type JSType int

const (
	Bool JSType = iota
	Int
	Float
	String
	Null
)

// Str2JSType extract a JavaScript type from a string
func Str2JSType(s string) JSType {
	var (
		output JSType
	)
	s = strings.TrimSpace(s) // santize the given string
	switch {
	case isBool(s):
		output = Bool
	case isFloat(s):
		output = Float
	case isInt(s):
		output = Int
	case isNull(s):
		output = Null
	default:
		output = String // if all alternatives have been eliminated, the input is a string
	}
	return output
}

func isBool(s string) bool {
	return s == "true" || s == "false"
}

func isFloat(s string) bool {
	var output = false
	if strings.Contains(s, ".") {
		_, err := strconv.ParseFloat(s, 64)
		if err == nil { // the string successfully converts to a decimal
			output = true
		}
	}
	return output
}

func isInt(s string) bool {
	var output = false
	if len(s) >= 1 {
		_, err := strconv.Atoi(s)
		if err == nil { // the string successfully converts to an int
			if s != "0" && s[0] == '0' {
				// if the first rune is '0' and there is more than 1 rune, then the input is most likely a float or intended to be
				// a string value -- such as in the case of a guid, or an international phone number
			} else {
				output = true
			}
		}
	}
	return output
}

func isNull(s string) bool {
	return s == "null"
}
