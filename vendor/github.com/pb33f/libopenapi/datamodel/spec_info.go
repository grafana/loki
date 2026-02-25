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

func ExtractSpecInfoWithConfig(spec []byte, config *DocumentConfiguration) (*SpecInfo, error) {
	return ExtractSpecInfoWithDocumentCheck(spec, config.BypassDocumentCheck)
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
	var parsedSpec yaml.Node

	specInfo := &SpecInfo{}

	// set original bytes
	specInfo.SpecBytes = &spec

	stringSpec := string(spec)
	runes := []rune(strings.TrimSpace(stringSpec))
	if len(runes) <= 0 {
		return specInfo, errors.New("there is nothing in the spec, it's empty - so there is nothing to be done")
	}

	if runes[0] == '{' && runes[len(runes)-1] == '}' {
		specInfo.SpecFileType = JSONFileType
	} else {
		specInfo.SpecFileType = YAMLFileType
	}

	specInfo.NumLines = strings.Count(stringSpec, "\n") + 1

	// Pre-process JSON to handle \/ escape sequences that YAML parser doesn't recognize.
	// JSON (RFC 8259) allows \/ as an optional escape for forward slash, but YAML does not.
	// See: https://github.com/pb33f/libopenapi/issues/479
	parseBytes := spec
	if specInfo.SpecFileType == JSONFileType {
		parseBytes = unescapeJSONSlashes(spec)
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
		if utils.IsYAML(string(bytes)) {
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

		// parse JSON
		if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
			return nil, err
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
		if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
			return nil, err
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
		if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
			return nil, err
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
			if err := parseJSON(spec, specInfo, &parsedSpec); err != nil {
				return nil, err
			}
			specInfo.Error = errors.New("spec type not supported by libopenapi, sorry")
			return specInfo, specInfo.Error
		}
	}
	//} else {
	//	// parse JSON
	//	parseJSON(spec, specInfo, &parsedSpec)
	//}

	if !parsed {
		if err := parseJSON(spec, specInfo, &parsedSpec); err != nil && !bypass {
			return nil, err
		}
	}

	// detect the original whitespace indentation
	specInfo.OriginalIndentation = utils.DetermineWhitespaceLength(string(spec))

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

// unescapeJSONSlashes replaces the optional \/ escape sequence in JSON with /
// JSON (RFC 8259) allows \/ as an optional escape for forward slash, but YAML
// parsers (including go.yaml.in/yaml/v4) do not recognize it.
// This handles escaped backslashes correctly: \\/ becomes \/ not //
// Returns the original slice if no transformation is needed (zero allocation).
func unescapeJSONSlashes(jsonBytes []byte) []byte {
	// fast path: check if transformation is needed
	if !bytes.Contains(jsonBytes, []byte(`\/`)) {
		return jsonBytes
	}

	result := make([]byte, 0, len(jsonBytes))
	i := 0
	for i < len(jsonBytes) {
		if jsonBytes[i] == '\\' && i+1 < len(jsonBytes) {
			switch jsonBytes[i+1] {
			case '/':
				// \/ -> / (json optional escape for solidus)
				result = append(result, '/')
				i += 2
			case '\\':
				// preserve escaped backslash to prevent \\/ becoming //
				result = append(result, '\\', '\\')
				i += 2
			default:
				// preserve other escape sequences (\n, \t, \", etc.)
				result = append(result, jsonBytes[i])
				i++
			}
		} else {
			result = append(result, jsonBytes[i])
			i++
		}
	}
	return result
}
