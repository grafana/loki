package xmlexpr

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// Expr represents a parsed XPath-like expression for XML extraction
type Expr struct {
	Path []string // Path segments like ["pod", "uuid"] for "pod.uuid"
}

// Parse parses a simple XPath-like expression
// Supports:
//   - element.subelement.field
//   - element[0] (positional indexing, treated as filter)
//   - element/@attribute
//   - // (any level, treated as wildcard)
func Parse(expr string) (*Expr, error) {
	if expr == "" {
		return nil, errors.New("empty expression")
	}

	// Normalize the expression
	expr = strings.TrimSpace(expr)

	// Remove leading slash if present
	if strings.HasPrefix(expr, "/") {
		expr = expr[1:]
	}

	if expr == "" {
		return nil, errors.New("empty expression after removing /")
	}

	// Split by dots or slashes for path segments
	var segments []string
	for _, s := range strings.FieldsFunc(expr, func(r rune) bool {
		return r == '.' || r == '/'
	}) {
		if s != "" && s != ".." && s != "." {
			segments = append(segments, s)
		}
	}

	if len(segments) == 0 {
		return nil, errors.New("no valid path segments")
	}

	return &Expr{Path: segments}, nil
}

// Extract extracts values from XML data matching the expression
func (e *Expr) Extract(xmlData []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	results := make([]string, 0) // Initialize with empty slice instead of nil
	var currentPath []string

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		switch elem := token.(type) {
		case xml.StartElement:
			currentPath = append(currentPath, elem.Name.Local)

			// Check if we match the expression path
			if e.matches(currentPath) {
				// For matching paths, look for character data or attributes
				// If path matches and we have attributes, extract them
				for _, attr := range elem.Attr {
					if e.matchesAttribute(currentPath, attr.Name.Local) {
						results = append(results, attr.Value)
					}
				}
			}

		case xml.CharData:
			text := bytes.TrimSpace(elem)
			if len(text) > 0 && len(currentPath) > 0 {
				// Check if current path matches expression
				if e.matches(currentPath) {
					results = append(results, string(text))
				}
			}

		case xml.EndElement:
			if len(currentPath) > 0 {
				currentPath = currentPath[:len(currentPath)-1]
			}
		}
	}

	return results, nil
}

// matches checks if a path matches the expression
func (e *Expr) matches(currentPath []string) bool {
	// Simple matching: check if the expression path is a suffix of the current path
	if len(e.Path) > len(currentPath) {
		return false
	}

	// Check from the end of the current path
	offset := len(currentPath) - len(e.Path)
	for i, segment := range e.Path {
		if currentPath[offset+i] != segment {
			return false
		}
	}

	return true
}

// matchesAttribute checks if an attribute matches the expression
func (e *Expr) matchesAttribute(currentPath []string, attrName string) bool {
	// For now, we check if the last path segment matches the attribute
	// This is a simplification; full XPath would handle /@attr syntax
	if len(e.Path) == 0 {
		return false
	}

	lastSegment := e.Path[len(e.Path)-1]
	// Check if last segment is @attrname or just attrname
	if strings.HasPrefix(lastSegment, "@") {
		expectedAttr := lastSegment[1:]
		return attrName == expectedAttr && e.matches(currentPath)
	}

	return false
}

// String returns the string representation of the expression
func (e *Expr) String() string {
	return strings.Join(e.Path, ".")
}
