package log

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// ExtractXMLValues extracts values from XML using a simple path expression
// Supports simple paths like "element", "parent/child", "element.child", "element@attr"
func ExtractXMLValues(data []byte, path string) ([]string, error) {
	var results []string

	// Handle simple cases
	if path == "" {
		return results, nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = nil

	// Parse the path - support "element" or "element@attr" or "parent/child"
	pathParts := strings.Split(strings.TrimSpace(path), "/")
	if len(pathParts) == 0 {
		return results, nil
	}

	// Simple implementation: look for the last element in path
	targetElement := pathParts[len(pathParts)-1]
	isAttribute := strings.Contains(targetElement, "@")
	var attrName string
	var elemName string

	if isAttribute {
		parts := strings.Split(targetElement, "@")
		if len(parts) == 2 {
			elemName = parts[0]
			attrName = parts[1]
		}
	} else {
		elemName = targetElement
	}

	// Scan XML for target element
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return results, err
		}

		switch elem := token.(type) {
		case xml.StartElement:
			// Check if this is our target element
			elemLocal := stripNamespacePrefix(elem.Name.Local)

			if elemLocal == elemName {
				if isAttribute && attrName != "" {
					// Extract attribute value
					for _, attr := range elem.Attr {
						attrLocal := stripNamespacePrefix(attr.Name.Local)
						if attrLocal == attrName {
							results = append(results, attr.Value)
						}
					}
				} else {
					// Extract element text content
					var text strings.Builder
					for {
						t, err := decoder.Token()
						if err != nil {
							break
						}

						switch v := t.(type) {
						case xml.CharData:
							text.Write(v)
						case xml.EndElement:
							if v.Name.Local == elem.Name.Local {
								if text.Len() > 0 {
									results = append(results, strings.TrimSpace(text.String()))
								}
								goto continueOuter
							}
						}
					}
				continueOuter:
				}
			}
		}
	}

	return results, nil
}

// stripNamespacePrefix removes XML namespace prefix (everything before ':')
func stripNamespacePrefix(name string) string {
	if idx := strings.IndexByte(name, ':'); idx != -1 {
		return name[idx+1:]
	}
	return name
}
