// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package datamodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

const (
	JSONFileType = "json"
	YAMLFileType = "yaml"
)

// SpecInfo represents a 'ready-to-process' OpenAPI Document. The RootNode is the most important property
// used by the library, this contains the top of the document tree that every single low model is based off.
type SpecInfo struct {
	SpecType            string                  `json:"type"`
	NumLines            int                     `json:"numLines"`
	Version             string                  `json:"version"`
	VersionNumeric      float32                 `json:"versionNumeric"`
	SpecFormat          string                  `json:"format"`
	SpecFileType        string                  `json:"fileType"`
	SpecBytes           *[]byte                 `json:"bytes"` // the original byte array
	RootNode            *yaml.Node              `json:"-"`     // reference to the root node of the spec.
	SpecJSONBytes       *[]byte                 `json:"-"`     // original bytes converted to JSON
	SpecJSON            *map[string]interface{} `json:"-"`     // standard JSON map of original bytes
	Error               error                   `json:"-"`     // something go wrong?
	APISchema           string                  `json:"-"`     // API Schema for supplied spec type (2 or 3)
	Generated           time.Time               `json:"-"`
	OriginalIndentation int                     `json:"-"` // the original whitespace
	Self                string                  `json:"-"` // the $self field for OpenAPI 3.2+ documents (base URI)
}

// Release nils fields that pin the YAML node tree and large byte arrays in memory.
func (s *SpecInfo) Release() {
	if s == nil {
		return
	}
	s.RootNode = nil
	s.SpecBytes = nil
	s.SpecJSONBytes = nil
	s.SpecJSON = nil
}

func ExtractSpecInfoWithConfig(spec []byte, config *DocumentConfiguration) (*SpecInfo, error) {
	if config == nil {
		return extractSpecInfoInternal(spec, false, false)
	}
	return extractSpecInfoInternal(spec, config.BypassDocumentCheck, config.SkipJSONConversion)
}

// ExtractSpecInfoWithDocumentCheckSync accepts an OpenAPI/Swagger specification that has been read into a byte array
// and will return a SpecInfo pointer, which contains details on the version and an un-marshaled
// deprecated: use ExtractSpecInfoWithDocumentCheck instead, this function will be removed in a later version.
func ExtractSpecInfoWithDocumentCheckSync(spec []byte, bypass bool) (*SpecInfo, error) {
	i, err := ExtractSpecInfoWithDocumentCheck(spec, bypass)
	if err != nil {
		return nil, err
	}
	return i, nil
}

// ExtractSpecInfoWithDocumentCheck accepts an OpenAPI/Swagger specification that has been read into a byte array
// and will return a SpecInfo pointer, which contains details on the version and an un-marshaled
// ensures the document is an OpenAPI document.
func ExtractSpecInfoWithDocumentCheck(spec []byte, bypass bool) (*SpecInfo, error) {
	return extractSpecInfoInternal(spec, bypass, false)
}

func extractSpecInfoInternal(spec []byte, bypass bool, skipJSON bool) (*SpecInfo, error) {
	var parsedSpec yaml.Node

	specInfo := &SpecInfo{}

	// set original bytes
	specInfo.SpecBytes = &spec

	trimmed := bytes.TrimSpace(spec)
	if len(trimmed) == 0 {
		return specInfo, errors.New("there is nothing in the spec, it's empty - so there is nothing to be done")
	}

	if trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}' {
		specInfo.SpecFileType = JSONFileType
	} else {
		specInfo.SpecFileType = YAMLFileType
	}

	specInfo.NumLines = bytes.Count(spec, []byte{'\n'}) + 1

	// Pre-process JSON escapes that YAML parsers do not accept even though
	// they are valid JSON, while preserving the existing YAML-node parse path.
	parseBytes := spec
	if specInfo.SpecFileType == JSONFileType {
		parseBytes = normalizeJSONForYAMLParser(spec)
	}

	err := yaml.Unmarshal(parseBytes, &parsedSpec)
	if err != nil {
		if !bypass {
			return nil, fmt.Errorf("unable to parse specification: %s", err.Error())
		}

		// read the file into a simulated document node.
		// we can't parse it, so create a fake document node with a single string content
		parsedSpec = yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{
				{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: string(spec),
				},
			},
		}
	}

	specInfo.RootNode = &parsedSpec

	_, openAPI3 := utils.FindKeyNode(utils.OpenApi3, parsedSpec.Content)
	_, openAPI2 := utils.FindKeyNode(utils.OpenApi2, parsedSpec.Content)
	_, asyncAPI := utils.FindKeyNode(utils.AsyncApi, parsedSpec.Content)

	parseJSON := func(bytes []byte, spec *SpecInfo, parsedNode *yaml.Node) error {
		var jsonSpec map[string]interface{}
		if spec.SpecFileType == YAMLFileType {
			// Decode YAML to map - this is critical to catch structural errors like duplicate keys
			if err := parsedNode.Decode(&jsonSpec); err != nil {
				return fmt.Errorf("failed to decode YAML to JSON: %w", err)
			}
			// Marshal to JSON - if this fails due to unsupported types (e.g. map[interface{}]interface{}),
			// we tolerate it as it doesn't indicate spec invalidity, just YAML/JSON incompatibility
			b, err := json.Marshal(&jsonSpec)
			if err == nil {
				spec.SpecJSONBytes = &b
			}
			spec.SpecJSON = &jsonSpec
		} else {
			if err := json.Unmarshal(bytes, &jsonSpec); err != nil {
				return fmt.Errorf("failed to unmarshal JSON: %w", err)
			}
			spec.SpecJSONBytes = &bytes
			spec.SpecJSON = &jsonSpec
		}
		return nil
	}

	// if !bypass {
	// check for specific keys
	parsed := false
	if openAPI3 != nil {
		version, majorVersion, versionError := parseVersionTypeData(openAPI3.Value)
		if versionError != nil {
			if !bypass {
				return nil, versionError
			}
		}

		specInfo.SpecType = utils.OpenApi3
		specInfo.Version = version
		specInfo.SpecFormat = OAS3

		// Extract the prefix version
		prefixVersion := specInfo.Version
		if len(specInfo.Version) >= 3 {
			prefixVersion = specInfo.Version[:3]
		}
		switch prefixVersion {
		case "3.1":
			specInfo.VersionNumeric = 3.1
			specInfo.APISchema = OpenAPI31SchemaData
			specInfo.SpecFormat = OAS31
			// extract $self field for OpenAPI 3.1+ (might be used as forward-compatible feature)
			_, selfNode := utils.FindKeyNode("$self", parsedSpec.Content)
			if selfNode != nil && selfNode.Value != "" {
				specInfo.Self = selfNode.Value
			}
		case "3.2":
			specInfo.VersionNumeric = 3.2
			specInfo.APISchema = OpenAPI32SchemaData
			specInfo.SpecFormat = OAS32
			// extract $self field for OpenAPI 3.2+
			_, selfNode := utils.FindKeyNode("$self", parsedSpec.Content)
			if selfNode != nil && selfNode.Value != "" {
				specInfo.Self = selfNode.Value
			}
		default:
			specInfo.VersionNumeric = 3.0
			specInfo.APISchema = OpenAPI3SchemaData
		}

		// parse JSON (skipped when SkipJSONConversion is set; also skips structural
		// validation like duplicate key detection — an explicit turbo trade-off since
		// the rules consuming these errors are stripped in turbo mode)
		if !skipJSON {
			if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
				return nil, err
			}
		}
		parsed = true

		// double check for the right version, people mix this up.
		if majorVersion < 3 {
			if !bypass {
				specInfo.Error = errors.New("spec is defined as an openapi spec, but is using a swagger (2.0), or unknown version")
				return specInfo, specInfo.Error
			}
		}
	}

	if openAPI2 != nil {
		version, majorVersion, versionError := parseVersionTypeData(openAPI2.Value)
		if versionError != nil {
			if !bypass {
				return nil, versionError
			}
		}

		specInfo.SpecType = utils.OpenApi2
		specInfo.Version = version
		specInfo.SpecFormat = OAS2
		specInfo.VersionNumeric = 2.0
		specInfo.APISchema = OpenAPI2SchemaData

		// parse JSON
		if !skipJSON {
			if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
				return nil, err
			}
		}
		parsed = true

		// I am not certain this edge-case is very frequent, but let's make sure we handle it anyway.
		if majorVersion > 2 {
			if !bypass {
				specInfo.Error = errors.New("spec is defined as a swagger (openapi 2.0) spec, but is an openapi 3 or unknown version")
				return specInfo, specInfo.Error
			}
		}
	}
	if asyncAPI != nil {
		version, majorVersion, versionErr := parseVersionTypeData(asyncAPI.Value)
		if versionErr != nil {
			if !bypass {
				return nil, versionErr
			}
		}

		specInfo.SpecType = utils.AsyncApi
		specInfo.Version = version
		// TODO: format for AsyncAPI.

		// parse JSON
		if !skipJSON {
			if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
				return nil, err
			}
		}
		parsed = true

		// so far there is only 2 as a major release of AsyncAPI
		if majorVersion > 2 {
			if !bypass {
				specInfo.Error = errors.New("spec is defined as asyncapi, but has a major version that is invalid")
				return specInfo, specInfo.Error
			}
		}
	}

	if specInfo.SpecType == "" {
		// parse JSON
		if !bypass {
			if !skipJSON {
				if err := parseJSON(spec, specInfo, &parsedSpec); err != nil {
					return nil, err
				}
			}
			specInfo.Error = errors.New("spec type not supported by libopenapi, sorry")
			return specInfo, specInfo.Error
		}
	}
	//} else {
	//	// parse JSON
	//	parseJSON(spec, specInfo, &parsedSpec)
	//}

	if !parsed && !skipJSON {
		if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
			return nil, err
		}
	}

	// detect the original whitespace indentation
	specInfo.OriginalIndentation = utils.DetermineWhitespaceLengthBytes(spec)

	return specInfo, nil
}

// ExtractSpecInfo accepts an OpenAPI/Swagger specification that has been read into a byte array
// and will return a SpecInfo pointer, which contains details on the version and an un-marshaled
// *yaml.Node root node tree. The root node tree is what's used by the library when building out models.
//
// If the spec cannot be parsed correctly then an error will be returned, otherwise the error is nil.
func ExtractSpecInfo(spec []byte) (*SpecInfo, error) {
	return ExtractSpecInfoWithDocumentCheck(spec, false)
}

// extract version number from specification
func parseVersionTypeData(d interface{}) (string, int, error) {
	r := []rune(strings.TrimSpace(fmt.Sprintf("%v", d)))
	if len(r) <= 0 {
		return "", 0, fmt.Errorf("unable to extract version from: %v", d)
	}
	return string(r), int(r[0]) - '0', nil
}

// normalizeJSONForYAMLParser rewrites the small set of JSON escapes accepted by
// RFC 8259 but rejected by go.yaml.in/yaml/v4. It returns the original slice
// without allocation unless a rewrite is required.
func normalizeJSONForYAMLParser(jsonBytes []byte) []byte {
	if bytes.IndexByte(jsonBytes, '\\') < 0 {
		return jsonBytes
	}

	var result []byte
	var runeBytes [utf8.UTFMax]byte
	last := 0
	scan := 0

	for scan < len(jsonBytes) {
		rel := bytes.IndexByte(jsonBytes[scan:], '\\')
		if rel < 0 {
			break
		}

		escape := scan + rel
		replacement, consumed, ok := jsonEscapeReplacement(jsonBytes, escape, &runeBytes)
		if !ok {
			scan = nextJSONEscapeScanOffset(jsonBytes, escape)
			continue
		}

		if result == nil {
			result = make([]byte, 0, len(jsonBytes))
		}
		result = append(result, jsonBytes[last:escape]...)
		result = append(result, replacement...)
		scan = escape + consumed
		last = scan
	}

	if result == nil {
		return jsonBytes
	}

	result = append(result, jsonBytes[last:]...)
	return result
}

func jsonEscapeReplacement(jsonBytes []byte, escape int, runeBytes *[utf8.UTFMax]byte) ([]byte, int, bool) {
	if escape+1 >= len(jsonBytes) {
		return nil, 0, false
	}

	switch jsonBytes[escape+1] {
	case '/':
		runeBytes[0] = '/'
		return runeBytes[:1], 2, true
	case 'u':
		if escape+12 > len(jsonBytes) {
			return nil, 0, false
		}

		high, ok := decodeJSONUnicodeEscape(jsonBytes[escape+2 : escape+6])
		if !ok || !isHighSurrogate(high) {
			return nil, 0, false
		}

		lowEscape := escape + 6
		if jsonBytes[lowEscape] != '\\' || jsonBytes[lowEscape+1] != 'u' {
			return nil, 0, false
		}

		low, ok := decodeJSONUnicodeEscape(jsonBytes[lowEscape+2 : lowEscape+6])
		if !ok || !isLowSurrogate(low) {
			return nil, 0, false
		}

		r := utf16.DecodeRune(rune(high), rune(low))
		n := utf8.EncodeRune(runeBytes[:], r)
		return runeBytes[:n], 12, true
	default:
		return nil, 0, false
	}
}

func nextJSONEscapeScanOffset(jsonBytes []byte, escape int) int {
	if escape+1 >= len(jsonBytes) {
		return escape + 1
	}
	return escape + 2
}

func decodeJSONUnicodeEscape(hexBytes []byte) (uint16, bool) {
	if len(hexBytes) != 4 {
		return 0, false
	}

	var value uint16
	for _, b := range hexBytes {
		hex, ok := jsonHexValue(b)
		if !ok {
			return 0, false
		}
		value = value<<4 | uint16(hex)
	}
	return value, true
}

func jsonHexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func isHighSurrogate(value uint16) bool {
	return value >= 0xD800 && value <= 0xDBFF
}

func isLowSurrogate(value uint16) bool {
	return value >= 0xDC00 && value <= 0xDFFF
}
