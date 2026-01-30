package executor

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type xmlParser struct {
	stripNamespaces bool
	prefixBuffer    [][]byte
}

// buildXMLColumns builds Arrow columnar data from XML log lines
func buildXMLColumns(
	input *array.String,
	requestedKeys []string,
	stripNamespaces bool,
) ([]string, []arrow.Array) {
	parser := newXMLParser(stripNamespaces)

	// Build requested key lookup for efficiency
	requestedKeyLookup := make(map[string]struct{})
	for _, k := range requestedKeys {
		requestedKeyLookup[k] = struct{}{}
	}

	results := make([]map[string]string, input.Len())
	allKeys := make(map[string]struct{})

	// Parse each line
	for i := 0; i < input.Len(); i++ {
		if input.IsNull(i) {
			results[i] = nil
			continue
		}

		line := input.Value(i)
		parsed, _ := parser.parseXMLLine([]byte(line), requestedKeyLookup, stripNamespaces)
		results[i] = parsed

		// Track all keys found
		for k := range parsed {
			allKeys[k] = struct{}{}
		}
	}

	// Build ordered list of all keys
	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}

	// Build arrays for each key
	arrays := make([]arrow.Array, len(keys))
	for i, k := range keys {
		builder := array.NewStringBuilder(memory.DefaultAllocator)
		defer builder.Release()

		for _, result := range results {
			if result == nil {
				builder.AppendNull()
			} else if v, ok := result[k]; ok {
				builder.Append(v)
			} else {
				builder.AppendNull()
			}
		}

		arrays[i] = builder.NewStringArray()
	}

	return keys, arrays
}

// newXMLParser creates a new XML parser for columnar processing
func newXMLParser(stripNamespaces bool) *xmlParser {
	return &xmlParser{
		stripNamespaces: stripNamespaces,
		prefixBuffer:    make([][]byte, 0, 16),
	}
}

// parseXMLLine parses a single XML line and returns a map of extracted fields
func (p *xmlParser) parseXMLLine(
	line []byte,
	requestedKeys map[string]struct{},
	stripNamespaces bool,
) (map[string]string, error) {
	result := make(map[string]string)

	// Check if this looks like XML
	if len(bytes.TrimSpace(line)) == 0 || bytes.TrimSpace(line)[0] != '<' {
		return result, nil
	}

	// Parse XML using decoder
	decoder := xml.NewDecoder(bytes.NewReader(line))
	decoder.CharsetReader = nil

	p.prefixBuffer = p.prefixBuffer[:0]

	err := p.parseXMLElement(decoder, requestedKeys, stripNamespaces, result)
	if err != nil && err != io.EOF {
		// Return what we've parsed so far on error
		return result, nil
	}

	return result, nil
}

// parseXMLElement recursively parses XML elements
func (p *xmlParser) parseXMLElement(
	decoder *xml.Decoder,
	requestedKeys map[string]struct{},
	stripNamespaces bool,
	result map[string]string,
) error {
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch elem := token.(type) {
		case xml.StartElement:
			name := p.stripNamespace(elem.Name.Local, stripNamespaces)

			// Process attributes
			for _, attr := range elem.Attr {
				attrName := p.stripNamespace(attr.Name.Local, stripNamespaces)
				key := name + "_" + attrName
				if p.shouldExtract(key, requestedKeys) {
					result[key] = attr.Value
				}
			}

			// Add element to prefix for tracking nesting
			p.prefixBuffer = append(p.prefixBuffer, []byte(name))

		case xml.CharData:
			// Handle text content
			text := bytes.TrimSpace(elem)
			if len(text) > 0 && len(p.prefixBuffer) > 0 {
				key := p.buildPrefixKey()
				if p.shouldExtract(key, requestedKeys) {
					result[key] = string(text)
				}
			}

		case xml.EndElement:
			// Pop the element from prefix
			if len(p.prefixBuffer) > 0 {
				p.prefixBuffer = p.prefixBuffer[:len(p.prefixBuffer)-1]
			}
		}
	}
}

// stripNamespace removes XML namespace prefix if needed
func (p *xmlParser) stripNamespace(name string, stripNamespaces bool) string {
	if !stripNamespaces {
		return name
	}
	if idx := strings.Index(name, ":"); idx != -1 {
		return name[idx+1:]
	}
	return name
}

// buildPrefixKey builds the flattened key from the current prefix buffer
func (p *xmlParser) buildPrefixKey() string {
	if len(p.prefixBuffer) == 0 {
		return ""
	}

	parts := make([]string, 0, len(p.prefixBuffer))
	for _, part := range p.prefixBuffer {
		parts = append(parts, string(part))
	}
	return strings.Join(parts, "_")
}

// shouldExtract determines if a key should be extracted
func (p *xmlParser) shouldExtract(key string, requestedKeys map[string]struct{}) bool {
	if len(requestedKeys) == 0 {
		return true
	}
	_, ok := requestedKeys[key]
	return ok
}
